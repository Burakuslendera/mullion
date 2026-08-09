//go:build windows

package webview2

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The NavigationStarting handler's ordering, guarded by reading the source.
//
// The handler is a closure registered against a live ICoreWebView2, and its
// arguments are a COM event-args object with a real vtable, so nothing in this
// suite can invoke it. That is what let issue #73 sit in it: the host was told
// to commit to a cancel before put_Cancel had been attempted, and no test could
// have noticed. The ordering is the whole fix, so it gets a guard of the kind
// this repository already uses where the type system cannot see an invariant.
//
// The first version of this guard was measured and found bypassable four ways:
// a comment could supply either literal it searched for, the `return` it
// required could belong to a neighbouring nil check, and its "both getters are
// reported" test counted a third report that belongs to put_Cancel. All four
// mutants restored issue #73 with the guard green. So: comments are stripped
// before anything is searched, the return has to sit inside the error branch,
// and each getter's report is asserted in its own span.

var sourceComment = regexp.MustCompile(`(?m)//.*$`)

// handlerSource returns the source of one registered handler with its comments
// removed, and fails loudly if it cannot find it - a rename that silently
// emptied this guard would leave every assertion below trivially true.
func handlerSource(t *testing.T, open, close string) string {
	t.Helper()
	data, err := os.ReadFile("browser_events_windows.go")
	if err != nil {
		t.Fatalf("read browser_events_windows.go: %v", err)
	}
	source := sourceComment.ReplaceAllString(string(data), "")
	start := strings.Index(source, open)
	if start < 0 {
		t.Fatalf("%q not found: this guard is scoped by that literal", open)
	}
	end := strings.Index(source[start:], close)
	if end < 0 {
		t.Fatalf("%q not found after it: the guard cannot tell where the handler ends", close)
	}
	return source[start : start+end]
}

func navigationStartingSource(t *testing.T) string {
	t.Helper()
	return handlerSource(t, "func (browser *Browser) handleNavigationStarting(", "func (browser *Browser) handleNavigationCompleted(")
}

func TestNavigationStartingCancelsBeforeItTellsTheHost(t *testing.T) {
	body := navigationStartingSource(t)

	// The combined form is the assertion: the error is captured and tested, not
	// discarded.
	cancel := strings.Index(body, "args.PutCancel(true); err != nil {")
	if cancel < 0 {
		t.Fatal("the NavigationStarting handler no longer tests put_Cancel's error")
	}
	notify := strings.Index(body, "NavigationCancelledCallback(")
	if notify < 0 {
		t.Fatal("the NavigationStarting handler no longer tells the host about a cancel")
	}
	if notify < cancel {
		t.Fatal("the host is told about the cancel before put_Cancel is attempted: it would commit to a cancel that may not happen (issue #73, decisions/0027)")
	}

	// And the failed attempt must tell nobody at all, which is the half that
	// matters: the navigation is still going ahead. The return has to be inside
	// the error branch, so the check stops at that branch's closing brace rather
	// than accepting any return between here and the notify.
	branch := body[cancel:notify]
	if closed := strings.Index(branch, "}"); closed >= 0 {
		branch = branch[:closed]
	}
	if !strings.Contains(branch, "return") {
		t.Fatal("put_Cancel's error branch does not return, so a failed cancel is reported to the host as a cancel (issue #73)")
	}
}

// One call site, inside that handler. A second one anywhere else would be
// outside the span this guard reads, and would tell the host about a cancel on
// terms nothing here checks.
func TestTheHostIsToldAboutACancelInExactlyOnePlace(t *testing.T) {
	data, err := os.ReadFile("browser_events_windows.go")
	if err != nil {
		t.Fatalf("read browser_events_windows.go: %v", err)
	}
	source := sourceComment.ReplaceAllString(string(data), "")
	if got := strings.Count(source, "NavigationCancelledCallback("); got != 1 {
		t.Fatalf("NavigationCancelledCallback is called %d times in this file, want exactly 1 - the guard above reads only the NavigationStarting handler, so a second call site is unchecked", got)
	}
}
