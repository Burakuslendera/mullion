package host

import (
	"os"
	"path/filepath"
	"regexp"
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
	body := callbackSource(t, "browser.NavigationCompletedCallback = func(", "browser.ProcessFailedCallback = func(")

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

// The other half of the same gap: the two navigation callbacks are wired inside
// createWebView, so the wiring itself is beyond every behavioural test in this
// suite. An audit measured what that costs - the whole pre-issue-73 shape can be
// restored in these six lines, or the cancelled callback deleted outright, with
// the entire suite green - so the wiring gets the same treatment as the bodies
// (issue #73, decisions/0027).
func TestTheNavigationCallbacksAreWiredToTheirOwnHalves(t *testing.T) {
	starting := callbackSource(t, "browser.NavigationStartingCallback = func(", "browser.NavigationCancelledCallback = func(")
	if !strings.Contains(starting, "host.noteAndGateNavigation(") {
		t.Fatal("NavigationStartingCallback no longer asks the gate for a decision")
	}
	// The decision half must commit to nothing: that is the fix.
	for _, banned := range []string{"noteNavigationCancelled(", "rememberCancelledNavigation(", "openInSystemBrowser("} {
		if strings.Contains(starting, banned) {
			t.Fatalf("NavigationStartingCallback contains %q: it would commit to a cancel the runtime has not performed (issue #73)", banned)
		}
	}

	cancelled := callbackSource(t, "browser.NavigationCancelledCallback = func(", "browser.NavigationCompletedCallback = func(")
	if !strings.Contains(cancelled, "host.noteNavigationCancelled(") {
		t.Fatal("NavigationCancelledCallback is not wired to the commit, so confirmed cancels are remembered nowhere and every one of them reaches the error-surface machine")
	}

	// And the completion callback must act on the ledger's verdict rather than
	// merely consulting it.
	completed := callbackSource(t, "browser.NavigationCompletedCallback = func(", "browser.ProcessFailedCallback = func(")
	const verdictCall = "if host.noteGateCancelledOutcome("
	verdict := strings.Index(completed, verdictCall)
	if verdict < 0 {
		t.Fatal("NavigationCompletedCallback no longer branches on the cancelled-navigation ledger alone")
	}
	// "if" attached directly to the call is not enough on its own: a condition
	// conjoined with the verdict turns the branch off while a guard that only
	// looks for the call itself stays perfectly happy. This guard's first version
	// said exactly that in a comment and then did not check it, and the mutant
	// `&& navigationID == 0` walked through it with the suite green. So the
	// condition between the call and its brace has to be the verdict and nothing
	// else.
	tail := completed[verdict+len(verdictCall):]
	brace := strings.Index(tail, "{")
	if brace < 0 {
		t.Fatal("the ledger verdict's branch has no body: the guard cannot tell what acting on it would mean")
	}
	if condition := tail[:brace]; strings.ContainsAny(condition, "&|") {
		t.Fatalf("the ledger's verdict is conjoined with something else (%q): the branch can be turned off without this guard noticing (issue #73, decisions/0027)", strings.TrimSpace(condition))
	}
	branch := tail[brace:]
	if closed := strings.Index(branch, "}"); closed >= 0 {
		branch = branch[:closed]
	}
	if !strings.Contains(branch, "return") {
		t.Fatal("NavigationCompletedCallback consults the ledger and carries on regardless: a deliberate cancel would still be resynced, re-evaluated and fed to the error-surface machine")
	}
}

var sourceComment = regexp.MustCompile(`(?m)//.*$`)

// callbackSource returns one callback assignment's source, and fails loudly if
// either delimiter has moved - a rename that silently emptied a guard would
// leave its assertions trivially true.
//
// Comments are stripped first, the way the sibling guard in
// internal/webview2/browser_events_source_test.go already does it. Without that,
// every literal these assertions search for can be supplied - or, worse,
// withheld - by a comment: deleting the real `return` and leaving the word in a
// comment above it kept this guard green while a cancelled navigation was fed
// straight back to the error-surface machine.
func callbackSource(t *testing.T, open, close string) string {
	t.Helper()
	source := sourceComment.ReplaceAllString(readRepoFile(t, "host", "webview_windows.go"), "")
	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("%q not found in host/webview_windows.go - this guard is scoped by that literal", open)
	}
	end := strings.Index(source[start:], close)
	if end < 0 {
		t.Fatalf("%q not found after it - the guard cannot tell where the closure ends", close)
	}
	return source[start : start+end]
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
