//go:build windows

package webview2

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The tests here are headless by construction: they create no window, start no
// browser process and require no WebView2 install. The two that do look at the
// machine skip when there is nothing installed to look at.

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{"equal", "150.0.4078.65", "150.0.4078.65", 0},
		{"patch newer", "150.0.4078.65", "150.0.4078.48", 1},
		{"patch older", "150.0.4078.48", "150.0.4078.65", -1},
		{"major beats every lower component", "150.0.0.0", "149.9.9999.99", 1},
		{"numeric, not lexicographic", "9.0.0.0", "10.0.0.0", -1},
		{"missing components count as zero", "150.0.4078", "150.0.4078.0", 0},
		{"missing components lose to a set one", "150.0.4078", "150.0.4078.1", -1},
		{"channel suffix is not part of the order", "94.0.992.31 dev", "94.0.992.31", 0},
		{"empty is oldest", "", "0.0.0.1", -1},
		{"both empty", "", "", 0},
		{"garbage component sorts as zero", "150.x.4078.65", "150.0.4078.65", 0},
		{"whitespace tolerated", " 150.0.4078.65 ", "150.0.4078.65", 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := CompareVersions(testCase.a, testCase.b); got != testCase.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", testCase.a, testCase.b, got, testCase.want)
			}
			// Antisymmetry: a wrong sign here would make runtime selection depend
			// on the order the registry happened to be read in.
			if got, want := CompareVersions(testCase.b, testCase.a), -testCase.want; got != want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", testCase.b, testCase.a, got, want)
			}
		})
	}
}

func TestIsInstalledVersion(t *testing.T) {
	cases := map[string]bool{
		"150.0.4078.65": true,
		"1":             true,
		"0.0.0.1":       true,
		// EdgeUpdate leaves this behind after an uninstall: the client is still
		// registered, but nothing is on disk.
		"0.0.0.0": false,
		"0":       false,
		"":        false,
		"   ":     false,
	}
	for version, want := range cases {
		if got := isInstalledVersion(version); got != want {
			t.Errorf("isInstalledVersion(%q) = %t, want %t", version, got, want)
		}
	}
}

func TestArchFolder(t *testing.T) {
	cases := map[string]string{"amd64": "x64", "386": "x86", "arm64": "arm64"}
	for goarch, want := range cases {
		got, err := archFolder(goarch)
		if err != nil {
			t.Fatalf("archFolder(%q): %v", goarch, err)
		}
		if got != want {
			t.Errorf("archFolder(%q) = %q, want %q", goarch, got, want)
		}
	}
	if _, err := archFolder("riscv64"); err == nil {
		t.Fatal("archFolder(riscv64) must fail: loading an x64 DLL into a riscv64 process is a crash, not a fallback")
	}
}

func TestClientPaths(t *testing.T) {
	paths := clientPaths(`C:\rt\150.0.4078.65`, "x64")
	if len(paths) == 0 {
		t.Fatal("clientPaths returned nothing")
	}
	want := filepath.Join(`C:\rt\150.0.4078.65`, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
	if paths[0] != want {
		t.Errorf("first candidate = %q, want the documented EBWebView layout %q", paths[0], want)
	}
	if got := clientPaths("", "x64"); got != nil {
		t.Errorf("clientPaths with no folder = %v, want nil", got)
	}
}

func TestRuntimeFolders(t *testing.T) {
	const root = `C:\Program Files (x86)\Microsoft\EdgeWebView\Application`

	folders := runtimeFolders(root, "150.0.4078.65", root)
	if len(folders) == 0 || folders[0] != filepath.Join(root, "150.0.4078.65") {
		t.Fatalf("folders = %v, want the versioned folder first", folders)
	}
	// The registry may already name the versioned folder; both readings have to
	// be offered, and neither may be offered twice.
	for i, folder := range folders {
		for j := i + 1; j < len(folders); j++ {
			if strings.EqualFold(folder, folders[j]) {
				t.Fatalf("duplicate candidate %q in %v", folder, folders)
			}
		}
	}

	// No location value: fall back to the default install root.
	folders = runtimeFolders("", "150.0.4078.65", root)
	if len(folders) != 1 || folders[0] != filepath.Join(root, "150.0.4078.65") {
		t.Fatalf("folders = %v, want only the default root", folders)
	}

	// A relative location is dropped: probed against the process CWD it could
	// reach LoadLibraryEx as a CWD-relative path. Discovery falls back to the
	// default install root, and every folder offered stays absolute.
	for _, rel := range []string{
		`EdgeWebView\Application`, // bare relative
		`.\runtime`,               // explicitly CWD-relative
		`C:runtime`,               // drive-relative (a per-drive CWD)
		`\runtime`,                // rooted but drive-relative
	} {
		folders = runtimeFolders(rel, "150.0.4078.65", root)
		if len(folders) != 1 || folders[0] != filepath.Join(root, "150.0.4078.65") {
			t.Fatalf("runtimeFolders(%q, ...) = %v, want only the default root", rel, folders)
		}
		for _, folder := range folders {
			if !filepath.IsAbs(folder) {
				t.Fatalf("runtimeFolders(%q, ...) offered a relative folder %q", rel, folder)
			}
		}
	}

	// An absolute UNC location is a legitimate network install and is honoured.
	const unc = `\\BUILD-NAS\tools\webview2`
	folders = runtimeFolders(unc, "150.0.4078.65", root)
	if len(folders) == 0 || folders[0] != filepath.Join(unc, "150.0.4078.65") {
		t.Fatalf("runtimeFolders(UNC) = %v, want the versioned UNC folder first", folders)
	}

	if got := runtimeFolders("", "", ""); got != nil {
		t.Errorf("runtimeFolders with nothing known = %v, want nil", got)
	}
}

// fakeDisk answers "does this exist" from a fixed set, so selection can be
// tested without an install.
func fakeDisk(paths ...string) func(string) bool {
	present := make(map[string]bool, len(paths))
	for _, path := range paths {
		present[strings.ToLower(filepath.Clean(path))] = true
	}
	return func(path string) bool {
		return present[strings.ToLower(filepath.Clean(path))]
	}
}

func clientIn(folder string) string {
	return filepath.Join(folder, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
}

func TestSelectRuntimePicksNewestInstalled(t *testing.T) {
	const older = `C:\rt\149.0.1.1`
	const newer = `C:\rt\150.0.4078.65`

	found, err := selectRuntime([]candidate{
		{source: sourceHKCU, version: "149.0.1.1", folders: []string{older}},
		{source: sourceHKLM32, version: "150.0.4078.65", folders: []string{newer}},
	}, "x64", fakeDisk(clientIn(older), clientIn(newer)))
	if err != nil {
		t.Fatalf("selectRuntime: %v", err)
	}
	if found.Version != "150.0.4078.65" {
		t.Fatalf("version = %q, want the newest install", found.Version)
	}
	if found.ClientDLL != clientIn(newer) {
		t.Fatalf("client = %q, want %q", found.ClientDLL, clientIn(newer))
	}
	if found.Fixed {
		t.Error("an Evergreen install must not be reported as fixed-version")
	}
}

func TestSelectRuntimeSkipsRegistryEntriesThatAreNotOnDisk(t *testing.T) {
	const ghost = `C:\rt\151.0.0.1` // registry says installed, disk disagrees
	const real = `C:\rt\150.0.4078.65`

	found, err := selectRuntime([]candidate{
		{source: sourceHKCU, version: "151.0.0.1", folders: []string{ghost}},
		{source: sourceHKLM32, version: "150.0.4078.65", folders: []string{real}},
	}, "x64", fakeDisk(clientIn(real)))
	if err != nil {
		t.Fatalf("selectRuntime: %v", err)
	}
	if found.Version != "150.0.4078.65" {
		t.Fatalf("version = %q: a newer registry entry with no DLL behind it must be ignored", found.Version)
	}
}

func TestSelectRuntimePinnedFolderWinsEvenWhenOlder(t *testing.T) {
	const pinned = `C:\fixed\120.0.0.1`
	const installed = `C:\rt\150.0.4078.65`

	found, err := selectRuntime([]candidate{
		{source: sourceEnvOverride, folders: []string{pinned}, pinned: true},
		{source: sourceHKLM32, version: "150.0.4078.65", folders: []string{installed}},
	}, "x64", fakeDisk(clientIn(pinned), clientIn(installed)))
	if err != nil {
		t.Fatalf("selectRuntime: %v", err)
	}
	if found.Folder != pinned {
		t.Fatalf("folder = %q, want the pinned folder %q", found.Folder, pinned)
	}
	if !found.Fixed {
		t.Error("a pinned folder is a fixed-version runtime and must be reported as one")
	}
}

func TestSelectRuntimePinnedFolderWithoutRuntimeIsAnError(t *testing.T) {
	const installed = `C:\rt\150.0.4078.65`

	_, err := selectRuntime([]candidate{
		{source: sourceEnvOverride, folders: []string{`C:\fixed\empty`}, pinned: true},
		{source: sourceHKLM32, version: "150.0.4078.65", folders: []string{installed}},
	}, "x64", fakeDisk(clientIn(installed)))
	if err == nil {
		t.Fatal("a pin that points at nothing must fail: silently running a different browser build than the one that was pinned is worse than not running")
	}
	if !strings.Contains(err.Error(), BrowserExecutableFolderEnv) {
		t.Errorf("error = %q, want it to name %s", err, BrowserExecutableFolderEnv)
	}
}

func TestSelectRuntimeWithNothingInstalled(t *testing.T) {
	_, err := selectRuntime(nil, "x64", fakeDisk())
	if err == nil {
		t.Fatal("selectRuntime must fail when no runtime exists")
	}
	if !strings.Contains(err.Error(), BrowserExecutableFolderEnv) {
		t.Errorf("error = %q, want it to tell the user how to point at a runtime", err)
	}
}

// TestResolveTargetVersion locks a fact that only the live runtime could teach:
// ICoreWebView2EnvironmentOptions::get_TargetCompatibleBrowserVersion must never
// answer null. CreateWebViewEnvironmentWithOptionsInternal validates it and
// fails the whole creation with E_INVALIDARG if it is missing, and an
// implausible value ("1.0.0.0") fails with ERROR_FILE_NOT_FOUND. Reporting the
// runtime we actually found is the only answer that always holds.
func TestResolveTargetVersion(t *testing.T) {
	if got := resolveTargetVersion("", "150.0.4078.65"); got != "150.0.4078.65" {
		t.Errorf("resolveTargetVersion = %q, want the discovered runtime version", got)
	}
	if got := resolveTargetVersion("142.0.3595.46", "150.0.4078.65"); got != "142.0.3595.46" {
		t.Errorf("resolveTargetVersion = %q, want the caller's explicit choice to win", got)
	}
	// Nothing is known about the runtime: still not null.
	for _, unknown := range []string{"", "0.0.0.0", "   "} {
		got := resolveTargetVersion("", unknown)
		if got == "" {
			t.Fatalf("resolveTargetVersion(%q, %q) = \"\": a null target version is rejected by the runtime with E_INVALIDARG", "", unknown)
		}
		if got != fallbackTargetVersion {
			t.Errorf("resolveTargetVersion(%q, %q) = %q, want the fallback %q", "", unknown, got, fallbackTargetVersion)
		}
	}
	if !isInstalledVersion(fallbackTargetVersion) {
		t.Fatal("the fallback target version must be a plausible browser version; the runtime rejects one it cannot map to a build")
	}
}

func TestParseFixedFileInfo(t *testing.T) {
	info := make([]byte, 52)
	// signature, strucVersion, fileVersionMS, fileVersionLS
	copy(info, []byte{0xBD, 0x04, 0xEF, 0xFE})
	copy(info[8:], []byte{0x00, 0x00, 0x96, 0x00})  // MS: 150.0
	copy(info[12:], []byte{0x41, 0x00, 0xEE, 0x0F}) // LS: 4078.65

	version, err := parseFixedFileInfo(info)
	if err != nil {
		t.Fatalf("parseFixedFileInfo: %v", err)
	}
	if version != "150.0.4078.65" {
		t.Fatalf("version = %q, want 150.0.4078.65", version)
	}

	if _, err := parseFixedFileInfo(info[:8]); err == nil {
		t.Error("a truncated block must be rejected, not read past its end")
	}
	bad := make([]byte, len(info))
	copy(bad, info)
	bad[0] = 0
	if _, err := parseFixedFileInfo(bad); err == nil {
		t.Error("a bad signature must be rejected: it means we are not looking at VS_FIXEDFILEINFO")
	}
}

func TestHResult(t *testing.T) {
	if err := hres(0); err != nil {
		t.Errorf("S_OK must be a success, got %v", err)
	}
	// S_FALSE is a success. Reading it as a failure is a classic COM bug: some
	// methods use it to mean "nothing to do".
	if err := hres(1); err != nil {
		t.Errorf("S_FALSE must be a success, got %v", err)
	}
	err := hres(eNoInterface)
	if err == nil {
		t.Fatal("E_NOINTERFACE must be an error")
	}
	var code HResultError
	if !errors.As(err, &code) {
		t.Fatalf("error %v does not carry its HRESULT; callers cannot tell an old runtime from a broken one", err)
	}
	if code.HResult() != 0x80004002 {
		t.Errorf("HResult() = 0x%08X, want 0x80004002", code.HResult())
	}
	if !strings.Contains(err.Error(), "80004002") {
		t.Errorf("error %q does not name the code", err)
	}
}

// --- the Go-implemented COM object ------------------------------------------
//
// These exercise the riskiest code in the package - a vtable Go hands to native
// code - without a WebView2 runtime, by calling the object through its own
// vtable exactly as the browser would.

func TestEnvironmentOptionsAnswersQueryInterface(t *testing.T) {
	before := liveServerCount()
	options := newEnvironmentOptions(Options{})
	unknown := (*IUnknown)(unsafe.Pointer(options))

	for _, iid := range []windows.GUID{IIDIUnknown, iidEnvironmentOptions} {
		pointer, err := unknown.QueryInterface(&iid)
		if err != nil {
			t.Fatalf("QueryInterface(%s): %v", iid.String(), err)
		}
		if uintptr(pointer) != options.this {
			t.Errorf("QueryInterface(%s) returned a different object", iid.String())
		}
		unknown.Release() // the reference QueryInterface just took
	}

	// A runtime probes for interfaces it might use. Claiming one we have not
	// implemented would make it call into an empty vtable slot.
	unsupported := windows.GUID{Data1: 0xdeadbeef}
	if _, err := unknown.QueryInterface(&unsupported); err == nil {
		t.Fatal("QueryInterface must refuse an interface the object does not implement")
	}

	options.release()
	if got := liveServerCount(); got != before {
		t.Fatalf("live COM objects = %d, want %d: the handler outlived its last reference", got, before)
	}
}

func TestEnvironmentOptionsReportsItsValuesToTheRuntime(t *testing.T) {
	const args = "--disable-features=ElasticOverscroll --autoplay-policy=no-user-gesture-required"
	options := newEnvironmentOptions(Options{
		AdditionalBrowserArguments:             args,
		AllowSingleSignOnUsingOSPrimaryAccount: true,
	})
	defer options.release()

	// Call through the vtable, the way the browser does.
	var value uintptr
	hr, _, _ := environmentOptionsVtable.GetAdditionalBrowserArguments.Call(
		options.this,
		uintptr(unsafe.Pointer(&value)),
	)
	if err := hres(hr); err != nil {
		t.Fatalf("get_AdditionalBrowserArguments: %v", err)
	}
	if value == 0 {
		t.Fatal("get_AdditionalBrowserArguments returned null for a value that was set")
	}
	if got := utf16At(value); got != args {
		t.Fatalf("get_AdditionalBrowserArguments = %q, want %q", got, args)
	}
	freeCoTaskMem(value)

	// An unset string must be reported as null, not as an empty string: that is
	// what the SDK's own options object does, and the runtime tells them apart.
	value = 1
	hr, _, _ = environmentOptionsVtable.GetLanguage.Call(options.this, uintptr(unsafe.Pointer(&value)))
	if err := hres(hr); err != nil {
		t.Fatalf("get_Language: %v", err)
	}
	if value != 0 {
		t.Fatalf("get_Language = 0x%x, want null for an unset property", value)
	}

	var allowed int32
	hr, _, _ = environmentOptionsVtable.GetAllowSingleSignOnUsingOSPrimaryAccount.Call(
		options.this,
		uintptr(unsafe.Pointer(&allowed)),
	)
	if err := hres(hr); err != nil {
		t.Fatalf("get_AllowSingleSignOnUsingOSPrimaryAccount: %v", err)
	}
	// Win32 BOOL is four bytes; writing a one-byte Go bool would leave three
	// bytes of whatever was on the stack for the runtime to read.
	if allowed != 1 {
		t.Fatalf("get_AllowSingleSignOnUsingOSPrimaryAccount = %d, want 1", allowed)
	}

	// A null out-parameter must be refused, not dereferenced.
	hr, _, _ = environmentOptionsVtable.GetLanguage.Call(options.this, 0)
	if hr != ePointer {
		t.Fatalf("get_Language(null) = 0x%08X, want E_POINTER", uint32(hr))
	}
}

func TestCompletedHandlerIsReleasedNotLeaked(t *testing.T) {
	before := liveServerCount()
	handler := newCompletedHandler(
		uintptr(unsafe.Pointer(&environmentCompletedVtable)),
		iidEnvironmentCompletedHandler,
	)
	if liveServerCount() != before+1 {
		t.Fatal("the handler was not registered")
	}

	// The runtime takes its own reference while the call is outstanding.
	unknown := (*IUnknown)(unsafe.Pointer(handler))
	if got := unknown.AddRef(); got != 2 {
		t.Fatalf("AddRef = %d, want 2", got)
	}
	if got := unknown.Release(); got != 1 {
		t.Fatalf("Release = %d, want 1", got)
	}
	if liveServerCount() != before+1 {
		t.Fatal("the handler was freed while a reference was still outstanding")
	}

	handler.release()
	if got := liveServerCount(); got != before {
		t.Fatalf("live COM objects = %d, want %d: handlers must not accumulate for the life of the process", got, before)
	}
}

// utf16At reads a NUL-terminated UTF-16 string out of memory the caller does
// not own, without converting the address into a Go pointer.
func utf16At(address uintptr) string {
	const limit = 4096
	units := make([]uint16, 0, 64)
	for offset := uintptr(0); offset < limit; offset += 2 {
		var unit uint16
		_, _, _ = procRtlMoveMemory.Call(
			uintptr(unsafe.Pointer(&unit)),
			address+offset,
			unsafe.Sizeof(unit),
		)
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return windows.UTF16ToString(units)
}

func freeCoTaskMem(address uintptr) {
	if address == 0 {
		return
	}
	_, _, _ = ole32.NewProc("CoTaskMemFree").Call(address)
}

// --- this machine ----------------------------------------------------------

// requireWebView2Env, set to "1", turns "no runtime installed" from a skip into
// a failure. The two tests below look at the machine, and skip when it has no
// WebView2 runtime, which keeps the suite runnable for everyone who has none. But
// a skip is invisible in a green run, so a job that is meant to exercise a real
// runtime can pass without ever checking the export this package is built on.
// Setting the variable closes that gap: the skip becomes a failure. The default
// stays a skip, so an ordinary `go test` still runs anywhere. See CONTRIBUTING.md.
const requireWebView2Env = "MULLION_REQUIRE_WEBVIEW2"

// skipT is the part of *testing.T that requireOrSkip uses. It is an interface so
// the skip-vs-fail decision can be tested without actually skipping or failing:
// the real Skipf and Fatalf both call runtime.Goexit, which a test cannot catch.
type skipT interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// requireOrSkip reports that this machine cannot satisfy a runtime-dependent
// test. Under MULLION_REQUIRE_WEBVIEW2=1 it is a failure; otherwise it is a skip.
func requireOrSkip(t skipT, reason string, err error) {
	t.Helper()
	if os.Getenv(requireWebView2Env) == "1" {
		t.Fatalf("%s=1 but %s: %v", requireWebView2Env, reason, err)
		return // *testing.T.Fatalf never returns; a fake one does, and must not fall through to Skipf
	}
	t.Skipf("%s: %v", reason, err)
}

// recordingT is a skipT that remembers which branch it was sent down, instead of
// unwinding the goroutine the way *testing.T would.
type recordingT struct {
	skipped bool
	failed  bool
}

func (*recordingT) Helper()                 {}
func (r *recordingT) Skipf(string, ...any)  { r.skipped = true }
func (r *recordingT) Fatalf(string, ...any) { r.failed = true }

// TestRequireWebView2TurnsSkipIntoFailure locks the escape from the silent-skip
// gap: unset, a missing runtime skips so a contributor without one is not
// blocked; set, the same missing runtime fails so a CI run cannot go green
// without checking the export.
func TestRequireWebView2TurnsSkipIntoFailure(t *testing.T) {
	absent := errors.New("no WebView2 runtime found")

	t.Setenv(requireWebView2Env, "") // default: not required
	relaxed := &recordingT{}
	requireOrSkip(relaxed, "no WebView2 runtime installed", absent)
	if !relaxed.skipped || relaxed.failed {
		t.Fatalf("unset: skipped=%v failed=%v, want a skip and no failure", relaxed.skipped, relaxed.failed)
	}

	t.Setenv(requireWebView2Env, "1") // CI: required
	strict := &recordingT{}
	requireOrSkip(strict, "no WebView2 runtime installed", absent)
	if !strict.failed || strict.skipped {
		t.Fatalf("required: skipped=%v failed=%v, want a failure and no skip", strict.skipped, strict.failed)
	}
}

func TestFindRuntimeOnThisMachine(t *testing.T) {
	folder, version, err := FindRuntime()
	if err != nil {
		requireOrSkip(t, "no WebView2 runtime installed", err)
		return
	}
	if folder == "" || version == "" {
		t.Fatalf("FindRuntime returned folder=%q version=%q; both must be set for an Evergreen install", folder, version)
	}
	client, err := RuntimeClientPath()
	if err != nil {
		t.Fatalf("RuntimeClientPath: %v", err)
	}
	if _, err := os.Stat(client); err != nil {
		t.Fatalf("FindRuntime chose %q, which is not on disk: %v", client, err)
	}
	if !strings.EqualFold(filepath.Base(client), clientDLL) {
		t.Fatalf("client = %q, want %s", client, clientDLL)
	}

	// The registry's version and the binary's own version describe the same
	// install; if they disagree, discovery picked a folder from one install and
	// a version from another.
	binary, err := fileVersion(client)
	if err != nil {
		requireOrSkip(t, "cannot read the version resource of "+clientDLL, err)
		return
	}
	if CompareVersions(binary, version) != 0 {
		t.Fatalf("registry reports %q but %s reports %q", version, clientDLL, binary)
	}
}

func TestRuntimeExportsTheEntryPointWeCallDirectly(t *testing.T) {
	path, err := RuntimeClientPath()
	if err != nil {
		requireOrSkip(t, "no WebView2 runtime installed", err)
		return
	}
	// Loading the DLL starts no browser process; it only proves that the export
	// this package is built on is really there. If Microsoft ever removes it,
	// this is the test that says so.
	loaded, err := loadClient(path)
	if err != nil {
		t.Fatalf("loadClient(%s): %v", clientDLL, err)
	}
	if loaded.createEnviron == 0 {
		t.Fatalf("%s exports no %s", clientDLL, createEnvironmentExport)
	}
}
