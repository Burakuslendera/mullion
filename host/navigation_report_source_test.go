package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one part of the issue #79 contract that no behavioural test can reach.
//
// The failure report was moved out of NavigationCompletedCallback and into the
// branches of the error-surface machine (decisions/0026). Every branch of the
// machine is driven headlessly by errorsurface_logging_windows_test.go - but the
// callback itself is a closure built inside createWebView, which needs a live
// *webview2.Browser and a WebView2 runtime, so no test in this suite can invoke
// it. Put the old line back there and the whole suite stays green while the
// issue is reopened: six suppressed aborts, six warnings, each contradicted by
// the debug line under it.
//
// The gap is closed the way this repository closes the other invariants its type
// system cannot see (TestNoNetworkListener, the leak scan): by reading the
// source. It is a blunt instrument and it is deliberately narrow - it says only
// that the callback reports no outcome of its own, which is the whole claim.
//
// It carries no build tag on purpose: the files it reads are in the tree on
// every platform, so the guard runs in the Linux CI job too, where none of the
// windows-tagged tests do.
func TestNavigationCompletedCallbackReportsNoFailureItself(t *testing.T) {
	const (
		open  = "browser.NavigationCompletedCallback = func("
		close = "browser.ProcessFailedCallback = func("
	)
	source := readRepoFile(t, "host", "webview_windows.go")

	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("%q not found in host/webview_windows.go - this guard is scoped by that literal, and a rename silently empties it", open)
	}
	end := strings.Index(source[start:], close)
	if end < 0 {
		t.Fatalf("%q not found after the completion callback - the guard cannot tell where the closure ends", close)
	}
	body := source[start : start+end]

	// A warning or an error here would be reporting the completion's outcome
	// before anything has classified it, which is the defect. warnIf is a
	// different thing and is allowed: it reports a failed Eval, not the
	// navigation.
	for _, banned := range []string{"host.log.Warn(", "host.log.Error("} {
		if strings.Contains(body, banned) {
			t.Fatalf("NavigationCompletedCallback contains %q: the callback runs before the classification exists, so any level it picks is a guess (issue #79, decisions/0026)", banned)
		}
	}
	// And it must not branch on the outcome at all - the failure is handed down
	// whole, and the branch that classifies it is the branch that reports it.
	if strings.Contains(body, "!success") {
		t.Fatal("NavigationCompletedCallback branches on !success: whatever it does there, the machine below is what decides what a failed completion means (decisions/0026)")
	}

	// The follow-up line the arming path writes says what was done, not that
	// something failed - the failure was already reported, once, by the branch
	// that classified it. At warn it would put the arming back to two warnings
	// for one failure; repeating "navigation failed" would put two hits in front
	// of anyone grepping for it.
	const followUp = `host.log.Info("mullion: showing fallback error surface")`
	surface := readRepoFile(t, "host", "errorsurface_windows.go")
	if !strings.Contains(surface, followUp) {
		t.Fatalf("host/errorsurface_windows.go no longer contains %s", followUp)
	}
}

// readRepoFile reads a file by its path from the module root, so the guards
// above keep working whatever directory the tests are run from - the same
// reason moduleRoot exists for the leak scan.
func readRepoFile(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{moduleRoot(t)}, elements...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
