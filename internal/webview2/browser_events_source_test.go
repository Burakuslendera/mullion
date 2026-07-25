//go:build windows

package webview2

import (
	"os"
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
// It is deliberately narrow. It says the cancel is attempted before the host is
// told, that a failed attempt tells nobody, and that neither getter's failure is
// discarded. It says nothing about what the host then does, which is host code
// and is tested there.

// navigationStartingBody returns the source of the NavigationStarting handler,
// and fails loudly if it cannot find it - a rename that silently emptied this
// guard would leave every assertion below trivially true.
func navigationStartingBody(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("browser_events_windows.go")
	if err != nil {
		t.Fatalf("read browser_events_windows.go: %v", err)
	}
	source := string(data)
	const (
		open  = "NewNavigationStartingHandler("
		close = "NewNavigationCompletedHandler("
	)
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

func TestNavigationStartingCancelsBeforeItTellsTheHost(t *testing.T) {
	body := navigationStartingBody(t)

	cancel := strings.Index(body, "args.PutCancel(true)")
	if cancel < 0 {
		t.Fatal("the NavigationStarting handler no longer calls PutCancel")
	}
	notify := strings.Index(body, "NavigationCancelledCallback(")
	if notify < 0 {
		t.Fatal("the NavigationStarting handler no longer tells the host about a cancel")
	}
	if notify < cancel {
		t.Fatal("the host is told about the cancel before put_Cancel is attempted: it would commit to a cancel that may not happen (issue #73, decisions/0027)")
	}
	// And a failed attempt must tell nobody at all, which is the half that
	// matters: the navigation is still going ahead.
	between := body[cancel:notify]
	if !strings.Contains(between, "return") {
		t.Fatal("nothing returns between put_Cancel failing and the host being told, so a failed cancel is reported as a cancel (issue #73)")
	}
}

// Both getters report their failures. The id already did; the URI did not, and
// an unreadable URI is not cosmetic - it reaches a host gate as the empty
// string, which is no origin's, so the gate decides against a navigation it
// could not read. That has to be diagnosable (issue #73).
func TestNavigationStartingReportsBothGetterFailures(t *testing.T) {
	body := navigationStartingBody(t)

	for _, want := range []string{"uri, err := args.GetUri()", "id, err := args.GetNavigationID()"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the NavigationStarting handler does not keep the error from %q", want)
		}
	}
	if strings.Contains(body, "args.GetUri()") && strings.Contains(body, "uri, _ :=") {
		t.Fatal("the URI getter's error is discarded again")
	}
	if strings.Count(body, "browser.reportWarning(err)") < 2 {
		t.Fatal("both getter failures must be reported, not just one")
	}
}
