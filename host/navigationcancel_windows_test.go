//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// What a cancel means (issue #73, decisions/0027). Three defects shared one
// root - the gate committed to a cancel before the runtime had performed it,
// and never learned whether it took - and the contract that replaced them is
// locked here: deciding has no side effects, a cancel with no identity is still
// recognised when its completion arrives, and a target the runtime could not
// read is cancelled rather than let through.
//
// The ledger those cancels are entered in is navigationledger_windows_test.go,
// which is also where outstandingCancels and wantOutstanding live.

// Deciding to cancel must change nothing. Everything that follows a cancel -
// the ledger entry, the system-browser hand-off - belongs to the navigation
// having actually been abandoned, and the decision is made before anyone knows
// that. Before this split, a put_Cancel that failed left the foreign document
// loading in the WebView, the same target opened in the browser, and the
// document's own completion swallowed as though it had been cancelled.
//
// It drives noteAndGateNavigation, which is what the runtime's callback calls.
// Probing shouldCancelNavigation instead left the fail-open reachable one level
// up with the whole suite green.
func TestGateDecisionAloneCommitsToNothing(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	if !host.noteAndGateNavigation("https://evil.example/x", 4) {
		t.Fatal("the gate did not decide to cancel an off-origin navigation")
	}

	if len(opened) != 0 {
		t.Fatalf("deciding to cancel handed %v to the system browser", opened)
	}
	if logger.String() != "" {
		t.Fatalf("deciding to cancel wrote to the log:\n%s", logger.String())
	}
	wantOutstanding(t, host)
	// The navigation is going ahead, so its completion belongs to the machine.
	// Consuming it here is what hid a foreign document from the host entirely.
	if host.noteGateCancelledOutcome(true, statusNone, 4) {
		t.Fatal("a completion was consumed for a cancel that was never confirmed")
	}
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 4) {
		t.Fatal("a failed completion was consumed for a cancel that was never confirmed")
	}
}

// A cancel the runtime gave no id for still has to be recognised when its
// completion arrives. There was no protection at all: noteGateCancelledOutcome
// returned early on id 0, so the deliberate cancel's OperationCanceled reached
// the machine as a load failure and replaced the live frontend with the fallback
// page. Order is all that is left without identity, which is the same trade
// decision 0020 makes for the error surface.
func TestIdlessCancelIsStillRecognised(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	if !cancelNavigation(host, "https://evil.example/", 0, true) {
		t.Fatal("the gate did not cancel an off-origin navigation with no id")
	}

	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 0) {
		t.Fatal("the id-less cancel's completion was not recognised")
	}
	// Exactly one: the count is not a licence to swallow every later cancel.
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 0) {
		t.Fatal("the id-less cancel was consumed twice")
	}
	// And the completion that is no longer consumed is a real failure again: it
	// reaches the machine and asks for the surface, which is what the id-less
	// branch is spending its one credit to prevent.
	if !host.noteNavigationOutcome(false, webview2.WebErrorStatusOperationCanceled, 0) {
		t.Fatal("an unconsumed cancellation must reach the machine as a failure")
	}
}

// Without an id the only evidence is the status, so the id-less branch is
// narrowed to the one a cancel is documented to produce. An ordinary failure
// with no id is a real failure and must still reach the machine - the fallback
// surface exists for it.
func TestIdlessCancelDoesNotSwallowAnOrdinaryFailure(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	if !cancelNavigation(host, "https://evil.example/", 0, true) {
		t.Fatal("the gate did not cancel an off-origin navigation with no id")
	}

	if host.noteGateCancelledOutcome(false, statusNone, 0) {
		t.Fatal("an id-less failure that is not a cancellation was swallowed as one")
	}
	if !host.noteNavigationOutcome(false, statusNone, 0) {
		t.Fatal("that failure must still ask for the fallback surface")
	}
}

// A navigation the gate cancels whose URI could not be read is cancelled anyway
// - a gate that lets through what it cannot identify is not a gate - but that is
// a legitimate in-origin navigation as far as anyone downstream can tell, so it
// is reported rather than dropped as an "unsupported scheme", which is what the
// empty string used to be mistaken for. It is never routed: handing the empty
// string to the system browser would be nonsense.
func TestUnreadableTargetIsCancelledLoudlyAndNeverRouted(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	if !cancelNavigation(host, "", 9, true) {
		t.Fatal("an unreadable target must be cancelled, not let through")
	}

	if len(opened) != 0 {
		t.Fatalf("an unreadable target reached the system browser: %v", opened)
	}
	logged := logger.String()
	if !strings.Contains(logged, "level=WARN msg=mullion: navigation cancelled off origin, target unreadable, id=9") {
		t.Fatalf("an unreadable target was not reported:\n%s", logged)
	}
	if strings.Contains(logged, "unsupported scheme") {
		t.Fatalf("an unreadable target was reported as an unsupported scheme:\n%s", logged)
	}
}
