//go:build windows

package webview2

// Runtime discovery: the folder rules and the selection precedence, tested
// against a fake disk instead of an install. Headless by construction - these
// create no window, start no browser process and require no WebView2 install.
// The tests that do look at this machine live in loader_machine_windows_test.go.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestArchFolder(t *testing.T) {
	cases := []struct {
		goarch string
		want   string
	}{
		{"amd64", "x64"},
		{"386", "x86"},
		{"arm64", "arm64"},
	}
	for _, testCase := range cases {
		got, err := archFolder(testCase.goarch)
		if err != nil {
			t.Errorf("archFolder(%q): %v", testCase.goarch, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("archFolder(%q) = %q, want %q", testCase.goarch, got, testCase.want)
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
