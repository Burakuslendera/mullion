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

// TestNoUpstreamBrandLeak scans selected repository text extensions for the
// configured known forbidden references and brands. It is a portable
// known-needle guard, not proof of source provenance or a complete publication
// scan; the needles are assembled at run time so this file cannot match itself.
func TestNoUpstreamBrandLeak(t *testing.T) {
	// The last configured needle names a third-party WebView2 binding this
	// package used to depend on. This guard rejects that known name in the text
	// extensions below; it does not prove attribution or dependency provenance.
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
		case ".go", ".md", ".yml", ".yaml", ".html", ".css", ".js", ".cs", ".mod", ".sum", ".ps1", "":
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

// TestNoNonASCIIInSource checks only Go source files for non-ASCII text. It
// catches non-English or half-translated Go comments using that narrow proxy.
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
