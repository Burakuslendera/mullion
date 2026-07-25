//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The ledger of cancelled navigations (issue #73, decisions/0027). Three
// defects shared one root - the gate committed to a cancel before the runtime
// had performed it, and never learned whether it took - and each half is locked
// here: deciding has no side effects, more than one cancel can be outstanding,
// and a cancel with no identity is still recognised when its completion arrives.
//
// The pair of calls a real NavigationStarting makes is `cancelNavigation`
// (systembrowser_windows_test.go). A test that wants the failed-cancel path
// calls only the first half, which is exactly what the runtime does when
// put_Cancel fails.

// Deciding to cancel must change nothing. Everything that follows a cancel -
// the ledger entry, the system-browser hand-off - belongs to the navigation
// having actually been abandoned, and the decision is made before anyone knows
// that. Before this split, a put_Cancel that failed left the foreign document
// loading in the WebView, the same target opened in the browser, and the
// document's own completion swallowed as though it had been cancelled.
func TestGateDecisionAloneCommitsToNothing(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	if !host.shouldCancelNavigation("https://evil.example/x") {
		t.Fatal("the gate did not decide to cancel an off-origin navigation")
	}

	if len(opened) != 0 {
		t.Fatalf("deciding to cancel handed %v to the system browser", opened)
	}
	if logger.String() != "" {
		t.Fatalf("deciding to cancel wrote to the log:\n%s", logger.String())
	}
	// The navigation is going ahead, so its completion belongs to the machine.
	// Consuming it here is what hid a foreign document from the host entirely.
	if host.noteGateCancelledOutcome(true, statusNone, 4) {
		t.Fatal("a completion was consumed for a cancel that was never confirmed")
	}
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 4) {
		t.Fatal("a failed completion was consumed for a cancel that was never confirmed")
	}
}

// More than one cancel can be outstanding. The single slot this replaced meant
// the second cancel evicted the first, and the evicted navigation's own
// OperationCanceled completion then reached the error-surface machine, armed it
// and tore the live frontend down into the fallback page - the exact failure the
// id consumption was added to prevent.
//
// 0021's live probe is why this is not hypothetical: the runtime was watched
// starting a second navigation of its own after the first ended, so "a top-frame
// navigation completes before the next starts" is not a rule.
func TestLedgerHoldsSeveralOutstandingCancels(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(5); id <= 8; id++ {
		if !cancelNavigation(host, "https://evil.example/", id, true) {
			t.Fatalf("the gate did not cancel navigation %d", id)
		}
	}

	// Completions arrive out of order, as completions do.
	for _, id := range []uint64{7, 5, 8, 6} {
		if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, id) {
			t.Fatalf("the completion of cancelled navigation %d was not recognised", id)
		}
	}
	if host.errorSurfaceActive || host.errorSurfacePending || host.errorSurfaceLoading {
		t.Fatalf("a cancelled navigation armed the error surface: active=%v pending=%v loading=%v",
			host.errorSurfaceActive, host.errorSurfacePending, host.errorSurfaceLoading)
	}
}

// The ledger is bounded, and reaching the bound is news rather than silence: the
// navigation dropped to make room reverts to the behaviour this issue is about,
// and nothing downstream could otherwise say which one it was.
func TestLedgerReportsWhatItForgets(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	for id := uint64(1); id <= cancelledNavSlots; id++ {
		cancelNavigation(host, "https://evil.example/", id, true)
	}
	if strings.Contains(logger.String(), "cancelled navigation forgotten") {
		t.Fatalf("the ledger reported an eviction before it was full:\n%s", logger.String())
	}

	cancelNavigation(host, "https://evil.example/", cancelledNavSlots+1, true)

	if !strings.Contains(logger.String(), "cancelled navigation forgotten, ledger full, id=1") {
		t.Fatalf("the evicted navigation was dropped silently:\n%s", logger.String())
	}
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 1) {
		t.Fatal("the evicted navigation was still in the ledger")
	}
	// The one that took its place is held, and so are the ones it did not evict.
	for _, id := range []uint64{2, cancelledNavSlots + 1} {
		if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, id) {
			t.Fatalf("navigation %d should still be in the ledger", id)
		}
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
	if host.errorSurfaceActive || host.errorSurfacePending || host.errorSurfaceLoading {
		t.Fatal("an id-less cancel armed the error surface")
	}
	// Exactly one: the count is not a licence to swallow every later cancel.
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 0) {
		t.Fatal("the id-less cancel was consumed twice")
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
