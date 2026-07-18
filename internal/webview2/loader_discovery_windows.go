//go:build windows

package webview2

// Runtime discovery: where the WebView2 runtime lives on this machine.
// Split from loader_windows.go, which keeps the creation entry points.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// The WebView2 Evergreen runtime registers itself with Edge Update under this
// application ID. The key is the documented way to discover the installed
// runtime; probing Program Files by hand would miss per-user installs and
// installs relocated by policy.
const edgeWebViewClientID = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

// runtimeType values for CreateWebViewEnvironmentWithOptionsInternal.
const (
	runtimeTypeEvergreen = 0
	runtimeTypeFixed     = 1
)

// BrowserExecutableFolderEnv pins the runtime to a specific folder. It is the
// documented override for fixed-version distributions and for developers who
// need to reproduce a bug against an exact browser build.
const BrowserExecutableFolderEnv = "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"

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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
