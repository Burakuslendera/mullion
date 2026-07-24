//go:build windows

package webview2

// The tests that look at this machine instead of at a fake: whether a runtime is
// installed, whether the DLL discovery chose is the one it claims, and whether it
// still exports the entry point this package is built on. They skip when there is
// nothing installed to look at, which keeps the suite runnable for a contributor
// with no runtime; MULLION_REQUIRE_WEBVIEW2=1 turns that skip into a failure so a
// job meant to exercise a real runtime cannot go green without one.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		// Not a skip. requireOrSkip answers "this machine has no runtime to look
		// at", and by this point it demonstrably has one: FindRuntime succeeded and
		// the file is on disk. A runtime DLL with no readable version resource is
		// therefore a defect in what discovery chose, and skipping would hide it.
		t.Fatalf("cannot read the version resource of %s at %q: %v", clientDLL, client, err)
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
