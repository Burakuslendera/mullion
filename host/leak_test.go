package host

import (
	"os"
	"os/exec"
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

// TestLeakScanScansNonASCIINames locks the fix for issue #29 in scripts/leak-scan.ps1.
//
// git ls-files quotes any tracked path containing non-ASCII bytes when
// core.quotePath is on (the default): a file actually named "<e-acute>name.go"
// comes back as the octal-escaped literal "\303\251name.go". The old scan handed
// that literal to Select-String -LiteralPath, which cannot open it, and swallowed
// the error with -ErrorAction SilentlyContinue - so a leak in a non-ASCII-named
// file sailed straight past a run that still reported clean. This is the sibling
// of the glob-name skip that #16 closed with -LiteralPath; that fix does not
// cover this one.
//
// The leak_test.go guards above walk the file system directly and so never hit
// git's quoting; only the PowerShell scan does. This test therefore runs the real
// script against a throwaway repo holding one non-ASCII-named file with a planted
// marker, and asserts the scan flags it - which it can only do if it enumerated
// the file under its real name. It is the sole regression lock on the script, so
// it runs wherever pwsh exists (Windows and Linux CI) and skips elsewhere.
func TestLeakScanScansNonASCIINames(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not on PATH; this locks a PowerShell script and only runs where pwsh exists")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}

	// The script locates the repo root as the parent of its own directory and
	// scans from there, so the throwaway repo must mirror that layout: a copy of
	// the real script under <root>/scripts, with the bait file at <root>.
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	realScript, err := os.ReadFile(filepath.Join(moduleRoot(t), "scripts", "leak-scan.ps1"))
	if err != nil {
		t.Fatalf("read leak-scan.ps1: %v", err)
	}
	scriptCopy := filepath.Join(scriptsDir, "leak-scan.ps1")
	if err := os.WriteFile(scriptCopy, realScript, 0o644); err != nil {
		t.Fatalf("copy leak-scan.ps1: %v", err)
	}

	// A file named with a non-ASCII byte (U+00E9), built from a rune so this
	// test's own source stays ASCII, carrying a marker the scan is built to catch:
	// an absolute Windows path. If the file is scanned, that is a finding; if it is
	// skipped, the run reports clean. The name is asserted on below, so it must not
	// appear anywhere else the scan would report it - it does not.
	baitName := string(rune(0x00e9)) + "name.go"
	const marker = "leak here: C:\\Users\\example\\secret"
	baitBody := []byte("package x\n// " + marker + "\n")
	if err := os.WriteFile(filepath.Join(root, baitName), baitBody, 0o644); err != nil {
		t.Fatalf("write bait file: %v", err)
	}

	// A fresh repo with core.quotePath forced on, so the trigger does not depend on
	// the runner's global git config. Nothing is committed: git ls-files reports
	// staged files, and with no HEAD the script's commit-message scan is skipped,
	// leaving the bait file as the only thing that can produce a finding.
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-q")
	gitRun("config", "core.quotePath", "true")
	// Pin line-ending policy so a contributor whose global git has safecrlf/autocrlf
	// set does not get git add rejecting the LF-only bait, which would fail this test
	// red for a reason unrelated to what it locks.
	gitRun("config", "core.autocrlf", "false")
	gitRun("config", "core.safecrlf", "false")
	gitRun("add", "-A")

	cmd := exec.Command(pwsh, "-NoProfile", "-File", scriptCopy)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running leak-scan.ps1: %v\n%s", err, out)
	}

	// Exit 1 alone proves a finding; requiring the real name in the output proves
	// it was this file, scanned under its true name rather than the octal-escaped
	// literal git quotes it to.
	if exitCode == 0 {
		t.Fatalf("leak-scan reported clean: the non-ASCII-named file was silently skipped\n%s", out)
	}
	if !strings.Contains(string(out), baitName) {
		t.Fatalf("leak-scan did not name %q in its output, so it was not scanned under its real name\n%s", baitName, out)
	}
}

// TestLeakScanScansCommitMessages locks the commit-message half of the scan
// (issue #71): leak-scan.ps1 flags a forbidden shape in a commit MESSAGE, not
// only in a tracked file. That half is what a shallow CI checkout (the default
// fetch-depth of 1) silently truncated to the tip commit; with full history it
// must catch a marker in any commit's message. This runs the real script against
// a throwaway repo whose single, otherwise-clean commit carries an absolute
// Windows path in its message, and asserts the run flags a commit.
func TestLeakScanScansCommitMessages(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not on PATH; this locks a PowerShell script and only runs where pwsh exists")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	realScript, err := os.ReadFile(filepath.Join(moduleRoot(t), "scripts", "leak-scan.ps1"))
	if err != nil {
		t.Fatalf("read leak-scan.ps1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "leak-scan.ps1"), realScript, 0o644); err != nil {
		t.Fatalf("copy leak-scan.ps1: %v", err)
	}
	// The tracked file is clean; the forbidden shape lives only in the commit
	// message, so a finding can come only from the commit-message half.
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("nothing to see here\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Author/committer identity via the environment so the commit does not depend
	// on the runner's global git config; line-ending policy pinned as in the
	// non-ASCII test.
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-q")
	gitRun("config", "core.autocrlf", "false")
	gitRun("config", "core.safecrlf", "false")
	gitRun("add", "-A")
	gitRun("commit", "-q", "-m", "leak in the message: C:\\Users\\example\\secret")

	cmd := exec.Command(pwsh, "-NoProfile", "-File", filepath.Join(scriptsDir, "leak-scan.ps1"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running leak-scan.ps1: %v\n%s", err, out)
	}

	if exitCode == 0 {
		t.Fatalf("leak-scan reported clean: the marker in the commit message was not scanned\n%s", out)
	}
	if !strings.Contains(string(out), "commit") {
		t.Fatalf("leak-scan finding does not reference a commit, so the commit-message half did not run\n%s", out)
	}
}
