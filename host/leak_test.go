package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot returns the repository root, so these guards scan the whole tree no
// matter which directory `go test` runs them from. Before this package moved out
// of the module root, WalkDir(".") covered the whole repo by accident of
// location; moving it here would have silently shrunk every scan below to this
// one directory. Locating go.mod restores the original scope on purpose.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test working directory")
		}
		dir = parent
	}
}

// TestNoUpstreamBrandLeak is a guard, not a formality.
//
// This package was extracted from a private application, and the extraction is
// only worth anything if none of that application came with it. The forbidden
// needles are assembled at run time so this file cannot match itself.
func TestNoUpstreamBrandLeak(t *testing.T) {
	// The last of these is not about the upstream application. This package used
	// to depend on a third-party WebView2 binding, and carrying that dependency
	// meant carrying its attribution and its limits. The COM layer is now written
	// here, so the name should appear nowhere - not in an import, not in a
	// notice, not in a comment. A hit means the dependency crept back.
	needles := []string{
		"token" + "pilor",
		"co" + "dex",
		"wa" + "ils",
	}

	err := filepath.WalkDir(moduleRoot(t), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".yml", ".yaml", ".html", ".css", ".js", ".mod", ".sum", ".ps1", "":
		default:
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(data))

		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				t.Errorf("%s contains a forbidden upstream reference %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestNoNetworkListener enforces the promise the README makes on the first
// screen: no local port is ever opened.
//
// That claim was documentation and nothing else - an invariant nobody checked.
// It is the kind of promise that decays quietly: a future contributor reaches for
// httptest to serve a fixture, or falls back to a loopback server "just for
// development", and the library's central security property is gone with a green
// build. Assets are served over an in-process virtual host; there is no case in
// which this package needs a socket.
//
// See docs/decisions/0002-no-local-port.md.
func TestNoNetworkListener(t *testing.T) {
	// Built at run time so this file does not match itself.
	//
	// Two tiers. The listeners are a socket, forbidden in every file: this package
	// opens none. The loopback hosts are forbidden everywhere except the one file
	// that exists to *reject* a non-loopback Config.URL - naming them there pins the
	// URL to the local machine, the opposite of opening a socket. See
	// docs/decisions/0002 (no port) and 0012 (the Config.URL opt-in).
	listeners := []string{
		"net." + "Listen",
		"http." + "ListenAndServe",
		"http." + "Serve(",
		"httptest",
	}
	loopbackLiterals := []string{
		"127.0." + "0.1",
		"local" + "host",
	}
	loopbackAllowed := map[string]bool{
		"loopback.go":      true,
		"loopback_test.go": true,
	}

	err := filepath.WalkDir(moduleRoot(t), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || filepath.Base(path) == "leak_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(data)
		for _, needle := range listeners {
			if strings.Contains(source, needle) {
				t.Errorf("%s contains %q: this package opens no sockets, and serves its assets over an in-process virtual host", path, needle)
			}
		}
		if !loopbackAllowed[filepath.Base(path)] {
			for _, needle := range loopbackLiterals {
				if strings.Contains(source, needle) {
					t.Errorf("%s contains %q: only loopback.go may name a loopback host, and only to reject a non-loopback Config.URL", path, needle)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestNoNonASCIIInSource keeps the project in one language. It also catches
// half-translated comments left over from the extraction.
func TestNoNonASCIIInSource(t *testing.T) {
	err := filepath.WalkDir(moduleRoot(t), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for index, line := range strings.Split(string(data), "\n") {
			for _, char := range line {
				if char > 127 {
					t.Errorf("%s:%d contains a non-ASCII character %q", path, index+1, char)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
