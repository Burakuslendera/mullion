//go:build windows

package webview2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The WebView2 Evergreen runtime registers itself with Edge Update under this
// application ID. The key is the documented way to discover the installed
// runtime; probing Program Files by hand would miss per-user installs and
// installs relocated by policy.
const edgeWebViewClientID = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

const (
	// clientDLL is the runtime's own COM server. It exports
	// CreateWebViewEnvironmentWithOptionsInternal, which is the whole reason
	// mullion does not have to ship WebView2Loader.dll: the loader DLL is a
	// convenience wrapper whose real work happens here.
	clientDLL = "EmbeddedBrowserWebView.dll"

	// createEnvironmentExport is documented by Microsoft as:
	//
	//	STDAPI CreateWebViewEnvironmentWithOptionsInternal(
	//	    bool checkRunningInstance,
	//	    int runtimeType,
	//	    PCWSTR userDataFolder,
	//	    IUnknown *environmentOptions,
	//	    ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler *handler)
	//
	// The argument list is a contract with a native ABI: a missing or reordered
	// argument is not an error return, it is a crash inside the browser process.
	createEnvironmentExport = "CreateWebViewEnvironmentWithOptionsInternal"
)

// runtimeType values for CreateWebViewEnvironmentWithOptionsInternal.
const (
	runtimeTypeEvergreen = 0
	runtimeTypeFixed     = 1
)

// BrowserExecutableFolderEnv pins the runtime to a specific folder. It is the
// documented override for fixed-version distributions and for developers who
// need to reproduce a bug against an exact browser build.
const BrowserExecutableFolderEnv = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"

// DefaultTimeout bounds environment and controller creation. WebView2 has to
// start a browser process, which on a cold machine is slow but not unbounded; a
// caller that waits forever would hang the UI thread with no diagnosis.
const DefaultTimeout = 60 * time.Second

var (
	// The IIDs below are the two callback interfaces we implement. They are
	// taken from the WebView2 SDK's WebView2.h (MIDL_INTERFACE declarations),
	// which is the authoritative source: an interface ID is an identity, and a
	// wrong one means the runtime silently refuses to call us back.
	//
	// ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	iidEnvironmentCompletedHandler = windows.GUID{
		Data1: 0x4e8a3389, Data2: 0xc9d8, Data3: 0x4bd2,
		Data4: [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d},
	}
	// ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	iidControllerCompletedHandler = windows.GUID{
		Data1: 0x6c4819f3, Data2: 0xc9b7, Data3: 0x4260,
		Data4: [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c},
	}
	// ICoreWebView2EnvironmentOptions
	iidEnvironmentOptions = windows.GUID{
		Data1: 0x2fde08a8, Data2: 0x1e9a, Data3: 0x4766,
		Data4: [8]byte{0x8c, 0x05, 0x95, 0xa9, 0xce, 0xb9, 0xd1, 0xc5},
	}
)

// --- runtime discovery -----------------------------------------------------

// runtimeSource records how a runtime was found, so a failure to start can name
// the thing that was wrong rather than "WebView2 not found".
type runtimeSource string

const (
	sourceEnvOverride runtimeSource = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"
	sourceHKCU        runtimeSource = "HKCU EdgeUpdate"
	sourceHKLM32      runtimeSource = "HKLM EdgeUpdate (32-bit view)"
	sourceHKLM64      runtimeSource = "HKLM EdgeUpdate (64-bit view)"
)

// candidate is one place a runtime might live, before the disk has been asked.
type candidate struct {
	source  runtimeSource
	version string   // "" when only the folder is known (fixed-version override)
	folders []string // folders to try, most specific first
	pinned  bool     // an explicit user instruction, not a discovery
}

// resolved is a runtime that exists on disk.
type resolved struct {
	Folder    string
	ClientDLL string
	Version   string
	Source    runtimeSource
	Fixed     bool
}

func (r resolved) runtimeType() int {
	if r.Fixed {
		return runtimeTypeFixed
	}
	return runtimeTypeEvergreen
}

// FindRuntime locates the WebView2 runtime this process should use.
//
// Order of precedence, which mirrors the official loader:
//
//  1. WEBVIEW2_BROWSER_EXECUTABLE_FOLDER, if set. This is a pin, not a hint: if
//     the folder has no usable runtime we fail instead of silently falling back
//     to the installed one, because running a different browser build than the
//     one that was pinned is worse than not running at all.
//  2. The per-user Evergreen install (HKCU).
//  3. The machine-wide Evergreen install (HKLM), 32-bit registry view first -
//     EdgeUpdate is a 32-bit process and writes under WOW6432Node - then the
//     64-bit view for hosts that do not have one.
//
// Every candidate is verified against the disk before it is accepted; the
// registry outlives uninstalls and half-finished updates.
func FindRuntime() (folder string, version string, err error) {
	found, err := findRuntime()
	if err != nil {
		return "", "", err
	}
	return found.Folder, found.Version, nil
}

// RuntimeClientPath returns the full path of the runtime DLL that will be
// loaded. It exists for diagnostics: when the browser fails to start, the first
// question is always which binary was actually used.
func RuntimeClientPath() (string, error) {
	found, err := findRuntime()
	if err != nil {
		return "", err
	}
	return found.ClientDLL, nil
}

func findRuntime() (resolved, error) {
	arch, err := archFolder(runtime.GOARCH)
	if err != nil {
		return resolved{}, err
	}
	found, err := selectRuntime(discoverCandidates(), arch, fileExists)
	if err != nil {
		return resolved{}, err
	}
	if !isInstalledVersion(found.Version) {
		// A pinned folder carries no registry entry, so the version has to come
		// from the runtime itself. This is not merely cosmetic: the version is
		// sent back to the runtime as the target compatible browser version, and
		// an absent one is an E_INVALIDARG.
		found.Version = versionOfRuntime(found.ClientDLL, found.Folder)
	}
	return found, nil
}

// versionOfRuntime reads the version of a runtime that no registry entry
// describes: first from the binary's version resource, then from the folder
// name, which both the Evergreen layout and the fixed-version package name
// after the version. Returns "" when neither can say.
func versionOfRuntime(clientPath, folder string) string {
	if version, err := fileVersion(clientPath); err == nil && isInstalledVersion(version) {
		return version
	}
	if base := sanitizeVersion(filepath.Base(folder)); isInstalledVersion(base) {
		return base
	}
	return ""
}

// discoverCandidates asks the environment and the registry. It touches no disk.
func discoverCandidates() []candidate {
	var out []candidate

	if pinned := strings.TrimSpace(os.Getenv(BrowserExecutableFolderEnv)); pinned != "" {
		out = append(out, candidate{
			source:  sourceEnvOverride,
			folders: []string{filepath.Clean(pinned)},
			pinned:  true,
		})
	}

	defaultRoot := defaultInstallRoot()
	type regKey struct {
		root   registry.Key
		path   string
		access uint32
		source runtimeSource
	}
	for _, key := range []regKey{
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + edgeWebViewClientID, 0, sourceHKCU},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + edgeWebViewClientID, registry.WOW64_32KEY, sourceHKLM32},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + edgeWebViewClientID, registry.WOW64_64KEY, sourceHKLM64},
	} {
		version, location, ok := readEdgeUpdateClient(key.root, key.path, key.access)
		if !ok {
			continue
		}
		out = append(out, candidate{
			source:  key.source,
			version: version,
			folders: runtimeFolders(location, version, defaultRoot),
		})
	}
	return out
}

// readEdgeUpdateClient reads the "pv" (product version) and "location" values
// EdgeUpdate publishes for an installed client.
func readEdgeUpdateClient(root registry.Key, path string, access uint32) (version, location string, ok bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|access)
	if err != nil {
		return "", "", false
	}
	defer key.Close()

	version, _, err = key.GetStringValue("pv")
	if err != nil {
		return "", "", false
	}
	// Strip control bytes at the trust boundary. pv is an unprivileged-writable
	// HKCU value that is logged at startup and printed by `mullion doctor`, so a
	// smuggled ESC/OSC/BEL sequence must not survive to reach an operator's
	// terminal through a terminal-rendering Logger. A real version carries none.
	version = sanitizeVersion(version)
	if !isInstalledVersion(version) {
		// EdgeUpdate leaves the client key behind with pv="0.0.0.0" after an
		// uninstall. Treating that as an install sends us hunting for a folder
		// that was deleted.
		return "", "", false
	}
	// "location" is optional; the default install root covers its absence.
	location, _, _ = key.GetStringValue("location")
	return version, strings.TrimSpace(location), true
}

// selectRuntime turns candidates into the one runtime to load.
//
// exists is injected so the whole selection rule - precedence, version
// ordering, the refusal to fall back past a pin - is testable without a
// WebView2 install.
func selectRuntime(candidates []candidate, arch string, exists func(string) bool) (resolved, error) {
	var best resolved
	var haveBest bool

	for _, item := range candidates {
		var match resolved
		var matched bool
		for _, folder := range item.folders {
			for _, path := range clientPaths(folder, arch) {
				if !exists(path) {
					continue
				}
				match = resolved{
					Folder:    folder,
					ClientDLL: path,
					Version:   item.version,
					Source:    item.source,
					Fixed:     item.pinned,
				}
				matched = true
				break
			}
			if matched {
				break
			}
		}

		if item.pinned {
			if !matched {
				return resolved{}, fmt.Errorf(
					"webview2: %s is set but no %s was found under it",
					BrowserExecutableFolderEnv, clientDLL)
			}
			// A pin wins outright, even over a newer installed runtime.
			return match, nil
		}
		if !matched {
			continue
		}
		if !haveBest || CompareVersions(match.Version, best.Version) > 0 {
			best, haveBest = match, true
		}
	}

	if !haveBest {
		return resolved{}, errors.New("webview2: no WebView2 runtime found; install the Evergreen WebView2 Runtime or set " + BrowserExecutableFolderEnv)
	}
	return best, nil
}

// --- pure helpers (unit-tested without a runtime present) -------------------

// archFolder maps a Go architecture to the runtime's per-architecture folder.
// The client DLL is native code: a 386 process cannot load the x64 build, so
// this must follow the process, not the machine.
func archFolder(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x64", nil
	case "386":
		return "x86", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("webview2: no WebView2 runtime exists for GOARCH=%s", goarch)
	}
}

// clientPaths lists where the client DLL may sit inside a runtime folder, most
// likely first. The Evergreen install and the fixed-version package both use
// the EBWebView\<arch> layout; the flatter forms are tolerated so that an
// unusual repackaging degrades into a working lookup rather than a failure.
func clientPaths(folder, arch string) []string {
	if folder == "" || arch == "" {
		return nil
	}
	return []string{
		filepath.Join(folder, "EBWebView", arch, clientDLL),
		filepath.Join(folder, arch, clientDLL),
		filepath.Join(folder, clientDLL),
	}
}

// runtimeFolders expands a registry entry into the folders that may hold the
// runtime. EdgeUpdate's "location" points at the Application directory, with
// one subdirectory per installed version, so the versioned path is tried first.
func runtimeFolders(location, version, defaultRoot string) []string {
	var out []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		for _, existing := range out {
			if strings.EqualFold(existing, path) {
				return
			}
		}
		out = append(out, path)
	}

	location = strings.TrimSpace(location)
	if location != "" {
		if version != "" {
			add(filepath.Join(location, version))
		}
		// Tolerate a location that already includes the version.
		add(location)
	}
	if defaultRoot != "" && version != "" {
		add(filepath.Join(defaultRoot, version))
	}
	return out
}

// defaultInstallRoot is where the machine-wide Evergreen runtime installs when
// the registry does not say otherwise. It is under the 32-bit Program Files
// even on 64-bit Windows, because Edge Update is a 32-bit installer.
func defaultInstallRoot() string {
	for _, key := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if root := os.Getenv(key); root != "" {
			return filepath.Join(root, "Microsoft", "EdgeWebView", "Application")
		}
	}
	return ""
}

// isInstalledVersion rejects the placeholder EdgeUpdate leaves behind when a
// client is registered but not installed.
func isInstalledVersion(version string) bool {
	if strings.TrimSpace(version) == "" {
		return false
	}
	for _, part := range versionParts(version) {
		if part != 0 {
			return true
		}
	}
	return false
}

// sanitizeVersion drops control characters from a version string that came from
// an untrusted source (the EdgeUpdate registry "pv", a folder name). The value is
// logged at startup and printed by `mullion doctor`, so a control byte
// (ESC/BEL/OSC) smuggled into it could inject terminal escape sequences into an
// operator's console through a terminal-rendering Logger. A legitimate version -
// digits, dots, spaces and an optional channel word - contains no control
// character, so stripping them cannot corrupt a real value.
func sanitizeVersion(version string) string {
	version = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, version)
	return strings.TrimSpace(version)
}

// CompareVersions orders two WebView2 version strings, returning -1, 0 or 1.
// It follows CompareBrowserVersions: compare dot-separated numbers left to
// right, with missing components treated as zero, so "150.0.4078" sorts before
// "150.0.4078.65".
//
// Browser version strings may carry a channel suffix ("94.0.992.31 dev"). The
// suffix names a channel, not a rank, so it takes no part in the ordering.
func CompareVersions(a, b string) int {
	left, right := versionParts(a), versionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var lv, rv int
		if i < len(left) {
			lv = left[i]
		}
		if i < len(right) {
			rv = right[i]
		}
		switch {
		case lv < rv:
			return -1
		case lv > rv:
			return 1
		}
	}
	return 0
}

func versionParts(version string) []int {
	version = strings.TrimSpace(version)
	if index := strings.IndexByte(version, ' '); index >= 0 {
		version = version[:index]
	}
	if version == "" {
		return nil
	}
	fields := strings.Split(version, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		// A component that is not a number is not ordered: treating it as zero
		// keeps a malformed version strictly older than a well-formed one
		// rather than crashing the discovery.
		value, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || value < 0 {
			value = 0
		}
		parts = append(parts, value)
	}
	return parts
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// --- file version --------------------------------------------------------

var (
	versionDLL                 = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSize = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = versionDLL.NewProc("VerQueryValueW")
)

// fileVersion reads a PE file's version resource. It is how a pinned
// fixed-version runtime gets a version number, since no registry entry
// describes it.
func fileVersion(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	size, _, callErr := procGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(name)), 0)
	if size == 0 {
		return "", fmt.Errorf("webview2: GetFileVersionInfoSize(%s): %w", filepath.Base(path), callErr)
	}

	buffer := make([]byte, size)
	ok, _, callErr := procGetFileVersionInfo.Call(
		uintptr(unsafe.Pointer(name)),
		0,
		size,
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if ok == 0 {
		return "", fmt.Errorf("webview2: GetFileVersionInfo(%s): %w", filepath.Base(path), callErr)
	}

	root, err := windows.UTF16PtrFromString(`\`)
	if err != nil {
		return "", err
	}
	var block uintptr
	var length uint32
	ok, _, callErr = procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&block)),
		uintptr(unsafe.Pointer(&length)),
	)
	if ok == 0 || block == 0 {
		return "", fmt.Errorf("webview2: VerQueryValue(%s): %w", filepath.Base(path), callErr)
	}

	// VerQueryValue returns a pointer *into* the buffer we own. Rather than
	// convert that address back into a Go pointer, turn it into an offset: the
	// bytes are already in Go memory and can simply be resliced.
	base := uintptr(unsafe.Pointer(&buffer[0]))
	if block < base || block+uintptr(length) > base+uintptr(len(buffer)) {
		return "", errors.New("webview2: version block lies outside the buffer")
	}
	offset := block - base
	return parseFixedFileInfo(buffer[offset : offset+uintptr(length)])
}

// parseFixedFileInfo decodes a VS_FIXEDFILEINFO structure. Split out from the
// Win32 plumbing so the decoding is unit-testable.
func parseFixedFileInfo(info []byte) (string, error) {
	// VS_FIXEDFILEINFO: signature, strucVersion, fileVersionMS, fileVersionLS, ...
	const (
		signature   = 0xFEEF04BD
		minimumSize = 16
	)
	if len(info) < minimumSize {
		return "", errors.New("webview2: version info too short")
	}
	if binary.LittleEndian.Uint32(info[0:4]) != signature {
		return "", errors.New("webview2: version info has a bad signature")
	}
	high := binary.LittleEndian.Uint32(info[8:12])
	low := binary.LittleEndian.Uint32(info[12:16])
	return fmt.Sprintf("%d.%d.%d.%d", high>>16, high&0xFFFF, low>>16, low&0xFFFF), nil
}

// --- loading the client DLL ------------------------------------------------

// loadWithAlteredSearchPath makes the DLL's own directory the first place its
// dependencies are looked for. The runtime DLL resolves siblings out of the
// install folder; without this flag they would be searched for next to our
// executable, which is the wrong folder and, worse, a folder an attacker may be
// able to write to.
const loadWithAlteredSearchPath = 0x00000008

type client struct {
	handle        windows.Handle
	createEnviron ComProc
	path          string
}

// The DLL is loaded once and never freed. Unloading it is not supported: it has
// spawned browser processes, registered COM classes and installed hooks that
// outlive any FreeLibrary we could call.
var (
	clientsMu sync.Mutex
	clients   = make(map[string]*client)
)

func loadClient(path string) (*client, error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if loaded, ok := clients[path]; ok {
		return loaded, nil
	}

	handle, err := windows.LoadLibraryEx(path, 0, loadWithAlteredSearchPath)
	if err != nil {
		return nil, fmt.Errorf("webview2: loading %s: %w", clientDLL, err)
	}
	address, err := windows.GetProcAddress(handle, createEnvironmentExport)
	if err != nil {
		// The runtime is present but does not export the entry point we need.
		// That means a runtime old enough - or repackaged oddly enough - that
		// it cannot be driven without WebView2Loader.dll.
		return nil, fmt.Errorf("webview2: %s does not export %s: %w", clientDLL, createEnvironmentExport, err)
	}
	loaded := &client{handle: handle, createEnviron: ComProc(address), path: path}
	clients[path] = loaded
	return loaded, nil
}

// --- ICoreWebView2EnvironmentOptions (implemented in Go) --------------------

// Options configures environment creation. The zero value is valid and asks for
// the installed runtime with no extra browser arguments.
type Options struct {
	// UserDataFolder is where the browser keeps its profile. Leave empty to let
	// the runtime pick its default (a folder beside the executable), which
	// fails for an executable installed under Program Files.
	UserDataFolder string

	// AdditionalBrowserArguments are Chromium command line switches.
	AdditionalBrowserArguments string

	// Language is a BCP-47 tag for the browser UI. Empty means the system
	// default, which is what the SDK's own options object reports.
	Language string

	// TargetCompatibleBrowserVersion names the browser build the caller was
	// written against. Empty means "the runtime we found", which is what this
	// package wants: the bindings are hand-written and every optional interface
	// is reached through QueryInterface, so the runtime that is installed is by
	// definition the one we are compatible with.
	//
	// It must not end up null. The runtime validates this property and rejects a
	// null with E_INVALIDARG - WebView2Loader.dll always supplies a value, so the
	// official path never discovers this, but we are not going through it.
	// See resolveTargetVersion.
	TargetCompatibleBrowserVersion string

	// AllowSingleSignOnUsingOSPrimaryAccount enables Azure AD SSO. Off by
	// default: it sends the signed-in Windows identity to the web content.
	AllowSingleSignOnUsingOSPrimaryAccount bool

	// Timeout bounds creation. Zero means DefaultTimeout.
	Timeout time.Duration
}

type environmentOptionsVtbl struct {
	IUnknownVtbl
	GetAdditionalBrowserArguments             ComProc
	PutAdditionalBrowserArguments             ComProc
	GetLanguage                               ComProc
	PutLanguage                               ComProc
	GetTargetCompatibleBrowserVersion         ComProc
	PutTargetCompatibleBrowserVersion         ComProc
	GetAllowSingleSignOnUsingOSPrimaryAccount ComProc
	PutAllowSingleSignOnUsingOSPrimaryAccount ComProc
}

type environmentOptions struct {
	server comServer // must stay first: this is the COM object's identity
	this   uintptr
	opts   Options
}

// The vtable is a package-level value built once. Every windows.NewCallback is
// permanent, so one per method for the whole process is the budget.
var environmentOptionsVtable = environmentOptionsVtbl{
	IUnknownVtbl:                              iunknownVtbl,
	GetAdditionalBrowserArguments:             ComProc(windows.NewCallback(optionsGetAdditionalBrowserArguments)),
	PutAdditionalBrowserArguments:             ComProc(windows.NewCallback(optionsPutString)),
	GetLanguage:                               ComProc(windows.NewCallback(optionsGetLanguage)),
	PutLanguage:                               ComProc(windows.NewCallback(optionsPutString)),
	GetTargetCompatibleBrowserVersion:         ComProc(windows.NewCallback(optionsGetTargetCompatibleBrowserVersion)),
	PutTargetCompatibleBrowserVersion:         ComProc(windows.NewCallback(optionsPutString)),
	GetAllowSingleSignOnUsingOSPrimaryAccount: ComProc(windows.NewCallback(optionsGetAllowSingleSignOn)),
	PutAllowSingleSignOnUsingOSPrimaryAccount: ComProc(windows.NewCallback(optionsPutBOOL)),
}

func newEnvironmentOptions(opts Options) *environmentOptions {
	options := &environmentOptions{opts: opts}
	options.this = options.server.register(
		uintptr(unsafe.Pointer(&environmentOptionsVtable)),
		iidEnvironmentOptions,
		options,
	)
	return options
}

func (o *environmentOptions) release() {
	serverRelease(o.this)
}

func optionsFor(this uintptr) *environmentOptions {
	server := serverFor(this)
	if server == nil {
		return nil
	}
	options, _ := server.self.(*environmentOptions)
	return options
}

// optionsGetString answers a string property. A null result with S_OK means
// "unset", which is exactly what the SDK's reference implementation returns for
// a property that was never assigned - so an empty Go string maps to null
// rather than to an empty string, which the runtime would treat as an explicit
// (and meaningless) value.
func optionsGetString(this, out uintptr, pick func(Options) string) uintptr {
	if out == 0 {
		return ePointer
	}
	options := optionsFor(this)
	if options == nil {
		return eFail
	}
	value, err := coTaskMemString(pick(options.opts))
	if err != nil {
		writeAddress(out, 0)
		return eOutOfMemory
	}
	writeAddress(out, value)
	return sOK
}

func optionsGetAdditionalBrowserArguments(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.AdditionalBrowserArguments })
}

func optionsGetLanguage(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.Language })
}

func optionsGetTargetCompatibleBrowserVersion(this, out uintptr) uintptr {
	return optionsGetString(this, out, func(o Options) string { return o.TargetCompatibleBrowserVersion })
}

func optionsGetAllowSingleSignOn(this, out uintptr) uintptr {
	if out == 0 {
		return ePointer
	}
	options := optionsFor(this)
	if options == nil {
		return eFail
	}
	writeBOOL(out, options.opts.AllowSingleSignOnUsingOSPrimaryAccount)
	return sOK
}

// The setters exist to fill their vtable slots. This object never leaves the
// package - it is created from an Options value, handed to the runtime, and
// released - and the runtime only ever reads it. Accepting and ignoring a write
// keeps a hypothetical setter call from failing environment creation, while
// failing it outright (E_NOTIMPL) would turn a harmless call into a dead
// window.
func optionsPutString(this, value uintptr) uintptr { return sOK }
func optionsPutBOOL(this, value uintptr) uintptr   { return sOK }

// --- completion handlers (implemented in Go) --------------------------------

type completionVtbl struct {
	IUnknownVtbl
	Invoke ComProc
}

// completion carries what an Invoke received. The environment and controller
// handlers are the same shape, so they share one type.
type completion struct {
	hr     uintptr
	result *IUnknown
}

type completedHandler struct {
	server comServer // must stay first
	this   uintptr
	done   chan completion
}

var (
	environmentCompletedVtable = completionVtbl{
		IUnknownVtbl: iunknownVtbl,
		Invoke:       ComProc(windows.NewCallback(environmentCompletedInvoke)),
	}
	controllerCompletedVtable = completionVtbl{
		IUnknownVtbl: iunknownVtbl,
		Invoke:       ComProc(windows.NewCallback(controllerCompletedInvoke)),
	}
)

func newCompletedHandler(vtable uintptr, iid windows.GUID) *completedHandler {
	// Buffered, so Invoke never blocks. Invoke runs on the UI thread inside our
	// own message pump: a send that blocked would deadlock the thread that is
	// supposed to receive it.
	handler := &completedHandler{done: make(chan completion, 1)}
	handler.this = handler.server.register(vtable, iid, handler)
	return handler
}

func (h *completedHandler) release() {
	serverRelease(h.this)
}

// invoked is the body shared by both Invoke callbacks.
func invoked(this, errorCode, result uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		return eFail
	}
	handler, ok := server.self.(*completedHandler)
	if !ok {
		return eFail
	}
	object := unknownFromAddress(result)
	if object != nil {
		// The reference handed to a completion handler is borrowed: the runtime
		// releases it as soon as Invoke returns. Keeping it without an AddRef
		// leaves a pointer to a freed object.
		object.AddRef()
	}
	select {
	case handler.done <- completion{hr: errorCode, result: object}:
	default:
		// Invoke fired twice, which the interface forbids. Drop the extra
		// rather than leak the reference we just took.
		object.Release()
	}
	return sOK
}

func environmentCompletedInvoke(this, errorCode, result uintptr) uintptr {
	return invoked(this, errorCode, result)
}

func controllerCompletedInvoke(this, errorCode, result uintptr) uintptr {
	return invoked(this, errorCode, result)
}

// --- environment ------------------------------------------------------------

// Environment is a live ICoreWebView2Environment.
//
// It deliberately holds nothing but the COM pointer: the interface's methods
// are declared elsewhere (interfaces_windows.go), and duplicating them here
// would mean two vtable layouts to keep in step with each other.
type Environment struct {
	unknown *IUnknown
}

// Unknown exposes the raw ICoreWebView2Environment pointer.
func (e *Environment) Unknown() *IUnknown {
	if e == nil {
		return nil
	}
	return e.unknown
}

// Release drops the reference taken when the environment was created.
func (e *Environment) Release() {
	if e == nil || e.unknown == nil {
		return
	}
	e.unknown.Release()
	e.unknown = nil
}

// CreateEnvironment creates a WebView2 environment for the runtime installed on
// this machine.
//
// The call is synchronous even though the underlying API is not: WebView2
// delivers the result to a completion handler on the calling thread's message
// queue, so this pumps messages until the handler fires. That means it must be
// called on a thread with a message queue and an initialised STA apartment -
// the same thread that will own the window.
func CreateEnvironment(userDataFolder string, additionalBrowserArgs string) (*Environment, error) {
	return CreateEnvironmentWithOptions(Options{
		UserDataFolder:             userDataFolder,
		AdditionalBrowserArguments: additionalBrowserArgs,
	})
}

// CreateEnvironmentWithOptions is CreateEnvironment with the full option set.
func CreateEnvironmentWithOptions(opts Options) (*Environment, error) {
	found, err := findRuntime()
	if err != nil {
		return nil, err
	}
	loaded, err := loadClient(found.ClientDLL)
	if err != nil {
		return nil, err
	}

	// The completion handler arrives as a window message, so the thread that
	// dispatches it must be the thread that made the call. Locking is cheap
	// insurance: the caller (the host's UI thread) is already locked, and
	// LockOSThread nests.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	opts.TargetCompatibleBrowserVersion = resolveTargetVersion(opts.TargetCompatibleBrowserVersion, found.Version)

	options := newEnvironmentOptions(opts)
	defer options.release()

	handler := newCompletedHandler(uintptr(unsafe.Pointer(&environmentCompletedVtable)), iidEnvironmentCompletedHandler)
	// Our reference is held until Invoke has run. Releasing it right after the
	// create call would rely on the runtime having taken its own reference; it
	// does, but a lifetime bug there is a use-after-free inside the browser, and
	// holding on costs one object.
	defer handler.release()

	var userDataFolder *uint16
	if opts.UserDataFolder != "" {
		if userDataFolder, err = windows.UTF16PtrFromString(opts.UserDataFolder); err != nil {
			return nil, fmt.Errorf("webview2: user data folder: %w", err)
		}
	}

	hr, _, _ := loaded.createEnviron.Call(
		1, // checkRunningInstance: join an already-running runtime for this user
		// data folder instead of failing. Two mullion windows in one process, or
		// a second process sharing the profile, are normal.
		uintptr(found.runtimeType()),
		uintptr(unsafe.Pointer(userDataFolder)),
		options.this,
		handler.this,
	)
	if err := hres(hr); err != nil {
		return nil, fmt.Errorf("webview2: %s: %w", createEnvironmentExport, err)
	}

	result, err := waitFor(handler.done, timeoutOf(opts), "the WebView2 environment")
	if err != nil {
		return nil, err
	}
	if err := hres(result.hr); err != nil {
		return nil, fmt.Errorf("webview2: environment creation failed: %w", err)
	}
	if result.result == nil {
		return nil, errors.New("webview2: environment creation reported success but returned nothing")
	}
	return &Environment{unknown: result.result}, nil
}

// environmentVtbl mirrors ICoreWebView2Environment only as far as its first
// method. The remaining methods belong to the interface bindings; this exists
// so that creating a controller - the one thing the loader must be able to do
// to prove it works - does not depend on them.
type environmentVtbl struct {
	IUnknownVtbl
	CreateCoreWebView2Controller ComProc
}

// CreateController creates the ICoreWebView2Controller that hosts the browser
// inside parent, and returns it as a raw interface pointer for the interface
// bindings to wrap. The caller owns a reference and must Release it.
//
// Like CreateEnvironment, this pumps messages until the completion handler
// fires, and must run on the window's own thread.
func (e *Environment) CreateController(parent windows.Handle) (*IUnknown, error) {
	return e.CreateControllerWithTimeout(parent, DefaultTimeout)
}

// CreateControllerWithTimeout is CreateController with an explicit bound.
func (e *Environment) CreateControllerWithTimeout(parent windows.Handle, timeout time.Duration) (*IUnknown, error) {
	if e == nil || e.unknown == nil {
		return nil, errors.New("webview2: environment is not open")
	}
	if parent == 0 {
		return nil, errors.New("webview2: controller needs a parent window")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handler := newCompletedHandler(uintptr(unsafe.Pointer(&controllerCompletedVtable)), iidControllerCompletedHandler)
	defer handler.release()

	vtbl := (*environmentVtbl)(unsafe.Pointer(e.unknown.Vtbl))
	hr, _, _ := vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(e.unknown)),
		uintptr(parent),
		handler.this,
	)
	if err := hres(hr); err != nil {
		return nil, fmt.Errorf("webview2: CreateCoreWebView2Controller: %w", err)
	}

	result, err := waitFor(handler.done, timeout, "the WebView2 controller")
	if err != nil {
		return nil, err
	}
	if err := hres(result.hr); err != nil {
		return nil, fmt.Errorf("webview2: controller creation failed: %w", err)
	}
	if result.result == nil {
		return nil, errors.New("webview2: controller creation reported success but returned nothing")
	}
	return result.result, nil
}

func timeoutOf(opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return DefaultTimeout
}

// fallbackTargetVersion is the browser build these bindings were written
// against - the value the WebView2 SDK 1.0.3595.46 compiles into its own
// options object (CORE_WEBVIEW_TARGET_PRODUCT_VERSION).
//
// It is only reached when the runtime's version cannot be determined at all,
// which means the registry, the DLL's version resource and the folder name all
// failed to say. Something must be sent: a null target is rejected outright.
const fallbackTargetVersion = "142.0.3595.46"

// resolveTargetVersion decides what to report as ICoreWebView2EnvironmentOptions
// ::get_TargetCompatibleBrowserVersion.
//
// Three behaviours of the runtime shape this, all established by testing the
// export directly rather than by reading the docs:
//
//   - A null value is rejected with E_INVALIDARG. Something must always be sent.
//   - An implausible value ("1.0.0.0") is rejected with ERROR_FILE_NOT_FOUND:
//     the runtime maps the version onto a browser build and finds none.
//   - A value *newer* than the installed runtime ("999.0.0.0") is accepted. The
//     compatibility floor lives in WebView2Loader.dll, which this package does
//     not use, so declaring a high target buys no protection at all. Feature
//     detection has to be done with QueryInterface, and is.
//
// Reporting the version of the runtime we are about to load is therefore both
// the truthful answer and the only one that cannot fail.
func resolveTargetVersion(requested, runtimeVersion string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if isInstalledVersion(runtimeVersion) {
		return runtimeVersion
	}
	return fallbackTargetVersion
}

// --- waiting on the UI thread ----------------------------------------------

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	procPeekMessage               = user32.NewProc("PeekMessageW")
	procTranslateMessage          = user32.NewProc("TranslateMessage")
	procDispatchMessage           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procMsgWaitForMultipleObjects = user32.NewProc("MsgWaitForMultipleObjectsEx")
)

const (
	wmQuit             = 0x0012
	pmRemove           = 0x0001
	qsAllInput         = 0x04FF
	mwmoInputAvailable = 0x0004

	// How long a single wait blocks before the deadline is re-checked. Long
	// enough not to spin, short enough that a timeout is reported promptly.
	pumpSliceMS = 20
)

// win32Msg mirrors MSG.
type win32Msg struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// pump dispatches window messages while we wait for a COM completion handler.
//
// It cannot simply sleep: WebView2 delivers the callback *through* the message
// queue, so a caller that blocks without dispatching waits for a message it is
// itself preventing from being delivered.
type pump struct {
	quitSeen bool
	quitCode uintptr
}

func (p *pump) step() {
	var message win32Msg
	for {
		got, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmRemove)
		if got == 0 {
			break
		}
		if message.message == wmQuit {
			// WM_QUIT is not dispatchable, and swallowing it would strand an
			// application that asked to exit while the WebView was still
			// starting. Remember it and put it back once the wait is over.
			p.quitSeen = true
			p.quitCode = message.wParam
			continue
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	// Block until something arrives rather than spinning. MWMO_INPUTAVAILABLE
	// makes the wait return at once if a message was posted between the drain
	// above and this call - the race a bare WaitMessage would lose.
	_, _, _ = procMsgWaitForMultipleObjects.Call(0, 0, pumpSliceMS, qsAllInput, mwmoInputAvailable)
}

// finish re-posts a quit that arrived while we were waiting.
func (p *pump) finish() {
	if p.quitSeen {
		_, _, _ = procPostQuitMessage.Call(p.quitCode)
	}
}

// waitFor pumps the message queue until the handler reports, or the deadline
// passes.
func waitFor[T any](done <-chan T, timeout time.Duration, what string) (T, error) {
	var zero T
	var messages pump
	defer messages.finish()

	deadline := time.Now().Add(timeout)
	for {
		select {
		case value := <-done:
			return value, nil
		default:
		}
		if time.Now().After(deadline) {
			// One last look: the handler may have fired inside the final step.
			select {
			case value := <-done:
				return value, nil
			default:
			}
			return zero, fmt.Errorf("webview2: gave up after %s waiting for %s", timeout, what)
		}
		messages.step()
	}
}
