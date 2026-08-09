//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The completions that must NOT arm the fallback surface, and the one start
// that must never be cancelled. Two suppressions sit in front of the machine:
// benignAbort, which reads ConnectionAborted as harmless only where mullion
// served the bytes itself and the navigation was on-origin (issue #72,
// decisions/0024), and noteGateCancelledOutcome, which consumes the
// PinNavigationToOrigin gate's own OperationCanceled before the machine sees it
// (issue #6, decisions/0023). Both fail open by design: without identity the
// surface arms.

// The tests below lock what an aborted navigation means (issue #72,
// decisions/0024). A same-origin document navigation was observed completing
// ConnectionAborted although its asset had been served 200, with the runtime
// starting the navigation again by itself - and arming on that abort replaced a
// live frontend with the fallback page, whose Retry aborted the same way, so it
// looped until an attempt happened to survive.

// Serving the embedded fs.FS in process, an abort of a navigation that was
// headed for the trusted origin cannot mean "could not load": mullion produced
// every byte of it.
func TestErrorSurfaceAbortDoesNotArmWhenAssetsAreServedInProcess(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	// The start the completion below belongs to - issue #72's sequence. The gate
	// is off in this config, so this only records the target.
	host.noteAndGateNavigation(host.source.origin.text+"/index.html?in=1", 3)

	if noteFail(host, 3) {
		t.Fatal("an aborted navigation must not ask for the fallback surface when mullion serves the assets itself")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("nothing was armed, so the empty source must stay rejected")
	}
	if !strings.Contains(logger.String(), "navigation aborted, not arming the error surface") {
		t.Fatal("a suppressed abort must say so, or it is indistinguishable in a report from a failure that went missing")
	}
}

// The other side of the same rule: with Config.URL set the caller serves the
// frontend over a socket, and ConnectionAborted is exactly what a dead endpoint
// produces (measured live, issue #68 and 0020's timeline) - the case the
// fallback surface exists for.
func TestErrorSurfaceAbortStillArmsWhenTheCallerServesTheURL(t *testing.T) {
	host, _ := newSurfaceHost(t)
	// The start has to be recorded, or the exemption is refused on the id and
	// this test would pass with the mode condition deleted.
	host.noteAndGateNavigation(host.source.origin.text+"/index.html", 3)

	if !noteFail(host, 3) {
		t.Fatal("an aborted navigation against a caller-served URL must still show the fallback surface")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("arming must remain pending until the surface start is claimed")
	}
}

// The exemption is for the abort status alone. Any other failure in process is
// still a failure and still arms.
func TestErrorSurfaceOtherFailuresStillArmInProcess(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	// Recorded so this reaches the status check: without a start the exemption
	// is refused on the id and the assertion below would hold either way.
	host.noteAndGateNavigation(host.source.origin.text+"/index.html", 3)

	if !host.noteNavigationOutcome(false, statusNone, 3) {
		t.Fatal("only an abort is benign in process; another failure must still arm")
	}
}

// Without an id nothing ties this completion to the navigation whose asset was
// served, so the exemption does not apply and decision 0020's machine stands.
// Suppressing the surface on that guess would fail open in the one case it is
// for.
func TestErrorSurfaceAbortWithoutIdentityStillArms(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if !noteFail(host, 0) {
		t.Fatal("an id-less abort must still arm: 0020's machine is the fallback wherever identity is unavailable")
	}
}

// Serving the assets in process does not keep the top frame on the trusted
// origin: PinNavigationToOrigin is off by default, so a link or a script
// assignment can take it anywhere, and that navigation is a real socket load
// mullion serves none of. Its abort is a real failure and must still arm - or
// the user is left on a chromeless foreign page with no caption buttons, which
// is issue #3, the state the surface exists to prevent.
func TestErrorSurfaceAbortOffOriginStillArms(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	// Gate off: this start is recorded, never cancelled and never routed.
	host.noteAndGateNavigation("https://evil.example/", 3)

	if !noteFail(host, 3) {
		t.Fatal("an aborted off-origin navigation must still show the fallback surface")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("arming must remain pending until the surface start is claimed")
	}
}

// The exemption belongs to the navigation the runtime last reported starting.
// A completion for an older one cannot borrow that answer - nothing says where
// *it* was going - so it falls through and arms, the safe direction.
func TestErrorSurfaceAbortWithAStaleIdStillArms(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.noteAndGateNavigation(host.source.origin.text+"/a.html", 3)
	host.noteAndGateNavigation(host.source.origin.text+"/b.html", 4)

	if !noteFail(host, 3) {
		t.Fatal("an abort whose id is not the last start's must arm")
	}
}

// The seal - the surface's own load failing - must stay locked in the default
// in-process mode, not only in the external-URL mode the identity tests model.
//
// This comment used to claim the test catches a widening of the abort exemption
// into noteSurfaceOwnOutcome. It does not, and the audit behind decisions/0026
// measured it: no start identity is recorded here while the completion carries
// known id 5, and benignAbort refuses before the status is ever consulted - the
// inserted branch is dead in this test and the suite
// stays green. What actually makes that widening harmless is a property of the
// surface, not of this test: errorPageURL is always a data: URL, and the seal is
// only reachable once noteSurfaceNavigationStarting claimed a start matching it,
// every accepted form of which leaves navStartInOrigin false. The test below
// still earns its place - it locks the seal in the mode the identity tests do
// not cover - it just does not lock what it said it did.
func TestErrorSurfaceSealsInProcessToo(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 5) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 6) {
		t.Fatal("the surface's own start must be claimed")
	}

	if host.noteNavigationOutcome(false, webview2.WebErrorStatusConnectionAborted, 6) {
		t.Fatal("the surface's own load failing must not re-navigate in a loop")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("nothing on screen is mullion's page, so the admission must drop")
	}
	if !strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("the surface dying must be reported")
	}
}

// A Retry start suspends controls while its destination is unresolved, then a
// positively classified benign abort restores them because the fallback remains
// the visible document. Restoring at start would admit the departing document;
// never restoring would leave the visible surface's caption buttons dead.
func TestErrorSurfaceAbortLeavesAVisibleSurfaceAdmitted(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 1) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2) {
		t.Fatal("the surface's own start must be claimed")
	}
	if noteOK(host, 2) {
		t.Fatal("the surface's own load must not trigger another navigation")
	}

	// The surface is the document on screen. Its Retry aborts in process.
	host.noteAndGateNavigation(host.source.origin.text+"/index.html", 3)
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("the Retry start kept controls live before its completion classified the departure")
	}
	if !host.errorSurfaceSuspended {
		t.Fatal("the Retry start did not preserve a restorable visible-surface suspension")
	}
	if noteFail(host, 3) {
		t.Fatal("the aborted Retry must not re-navigate")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the visible surface must keep its admission, or its caption buttons go dead")
	}
}

func TestGateCancelledDepartureRestoresVisibleSurfaceAdmission(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 1) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2) {
		t.Fatal("the surface's own start must be claimed")
	}
	noteOK(host, 2)

	if !cancelNavigation(host, "https://evil.example/", 7, true) {
		t.Fatal("the foreign departure was not cancelled")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("the cancelled departure kept controls live before confirmation")
	}
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7) {
		t.Fatal("the confirmed cancellation was not consumed")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("confirmed cancellation did not restore the still-visible surface")
	}
}

func TestOlderCancelledDepartureCannotRestoreNewerSuspension(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	host.noteNavigationOutcome(false, statusNone, 1)
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2) {
		t.Fatal("the surface's own start must be claimed")
	}
	noteOK(host, 2)

	if !cancelNavigation(host, "https://evil.example/first", 7, true) ||
		!cancelNavigation(host, "https://evil.example/second", 8, true) {
		t.Fatal("foreign departures were not cancelled")
	}
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7) {
		t.Fatal("older confirmed cancellation was not consumed")
	}
	if host.errorSurfaceMessageAllowed("") || !host.errorSurfaceSuspended {
		t.Fatal("older cancellation restored admission reserved for the newer departure")
	}
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 8) {
		t.Fatal("newer confirmed cancellation was not consumed")
	}
	if !host.errorSurfaceMessageAllowed("") || host.errorSurfaceSuspended {
		t.Fatal("matching newer cancellation did not restore the visible surface")
	}
}

func TestGenericDepartureFailureClearsVisibleSurfaceSuspension(t *testing.T) {
	host, _ := newSurfaceHost(t)
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 1) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2) {
		t.Fatal("the surface's own start must be claimed")
	}
	noteOK(host, 2)

	host.noteAndGateNavigation(host.source.origin.text+"/index.html", 3)
	if !host.errorSurfaceSuspended || host.errorSurfaceMessageAllowed("") {
		t.Fatal("departure did not suspend the visible fallback before completion")
	}
	if !noteFail(host, 3) {
		t.Fatal("a genuine external-source failure must show a new fallback generation")
	}
	issueCurrentErrorSurface(t, host)
	if host.errorSurfaceSuspended || host.errorSurfaceActive || !host.errorSurfacePending {
		t.Fatalf("generic failure retained the old suspension: suspended=%t active=%t pending=%t",
			host.errorSurfaceSuspended, host.errorSurfaceActive, host.errorSurfacePending)
	}
}

// TestGateCancelledCompletionDoesNotArmTheSurface locks the F1 fix: a navigation
// the PinNavigationToOrigin gate cancels completes with OperationCanceled, and
// that completion must be consumed as a deliberate cancel rather than reaching
// the error-surface machine, which would read a foreign failure as "navigate to
// the fallback surface" and tear down the live frontend (issue #6, decisions/0023).
func TestGateCancelledCompletionDoesNotArmTheSurface(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	// The gate cancels a foreign navigation and, once the runtime confirms it,
	// enters it in the ledger. Routing the target is not this test's concern and
	// does not happen: every test host stubs the system-browser seam
	// (newTestHost, issue #76).
	if !cancelNavigation(host, "https://evil.example/", 7, true) {
		t.Fatal("gate did not cancel a foreign navigation")
	}

	// Its OperationCanceled completion is consumed - the error-surface machine is
	// never reached, so nothing arms and the live frontend stays.
	if !host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7) {
		t.Fatal("the cancelled navigation's completion was not recognised")
	}
	if host.errorSurfaceActive || host.errorSurfacePending || host.errorSurfaceLoading {
		t.Fatalf("the gate's own cancel armed the error surface: active=%v pending=%v loading=%v",
			host.errorSurfaceActive, host.errorSurfacePending, host.errorSurfaceLoading)
	}
	// The entry is gone: a second completion for the same id has nothing to
	// match, so a stale id cannot swallow a later navigation's outcome.
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 7) {
		t.Fatal("the ledger entry survived its own completion")
	}

	// An unrelated completion is not consumed, and a genuinely foreign failure in
	// steady state still arms the fallback surface - the guard is scoped to the
	// gate's own cancel, not every OperationCanceled.
	if host.noteGateCancelledOutcome(false, webview2.WebErrorStatusOperationCanceled, 99) {
		t.Fatal("noteGateCancelledOutcome consumed an unrelated completion")
	}
	if !host.noteNavigationOutcome(false, webview2.WebErrorStatusOperationCanceled, 99) {
		t.Fatal("a genuinely foreign failure in steady state must still arm the surface")
	}
}

// TestNoteAndGateNavigationNeverCancelsTheSurface locks the F2 fix: when the
// error-surface claim matches a NavigationStarting - including the tolerated
// empty-URI form the runtime can report for a data: start - the gate is skipped,
// so it never cancels mullion's own fallback page (issue #6, decisions/0023).
func TestNoteAndGateNavigationNeverCancelsTheSurface(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	host.noteNavigationOutcome(false, statusNone, 1)
	issueCurrentErrorSurface(t, host)

	// The surface's own start successfully reported as an empty URI (the
	// runtime erasing the data: URI) is claimed, and must NOT be cancelled even
	// though "" is off-origin to the gate.
	if host.noteAndGateNavigation("", 4) {
		t.Fatal("the gate cancelled the surface's own navigation (empty URI)")
	}
	if host.errorSurfacePending {
		t.Fatal("the surface start was not claimed")
	}

	// Outside the claim window, a foreign navigation is still cancelled.
	if !host.noteAndGateNavigation("https://evil.example/", 5) {
		t.Fatal("the gate did not cancel a foreign navigation outside the claim")
	}
}
