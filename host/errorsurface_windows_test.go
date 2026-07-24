//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The tests below lock the error-surface admission state machine (issues #56,
// #68 and the identity follow-up under #6). The runtime reports a data:
// document's source as the empty string - measured live at both the event args
// and the core - so the fallback error surface can only be recognised by
// navigation state, and these transitions are what decide whether its caption
// buttons work. Completions carrying a navigation id are attributed positively
// against the id noteSurfaceNavigationStarting claimed (decisions/0021); the
// id-less drives (navigation id 0) lock the order-based fallback, which must
// stay exactly decision 0020's machine. Each test walks the note* methods the
// way the navigation callbacks would.

// statusNone is COREWEBVIEW2_WEB_ERROR_STATUS_UNKNOWN (0): the status a
// successful completion carries, which the machine must not read, and equally
// the stand-in for a failure whose status is none of the ones it branches on.
const statusNone = webview2.WebErrorStatus(0)

// noteFail, noteCancel and noteOK drive noteNavigationOutcome the way the
// completion callback would: a network failure, a superseded navigation's
// cancellation, and a success. Passing id 0 models a completion whose identity
// is unavailable, which is what routes the machine into the order-based
// fallback the id-less tests lock.
func noteFail(host *Host, id uint64) bool {
	return host.noteNavigationOutcome(false, webview2.WebErrorStatusConnectionAborted, id)
}

func noteCancel(host *Host, id uint64) bool {
	return host.noteNavigationOutcome(false, webview2.WebErrorStatusOperationCanceled, id)
}

func noteOK(host *Host, id uint64) bool {
	return host.noteNavigationOutcome(true, statusNone, id)
}

// newSurfaceHost builds a host whose frontend is served by the caller over a
// loopback URL. That is the setting the identity orderings below were measured
// in: the live timelines behind 0020 and 0021 are a dead loopback endpoint,
// where the ConnectionAborted noteFail sends is a real "could not load" and must
// arm the surface. Serving the embedded fs.FS in process there is no socket that
// can fail, so the same status is a benign abort instead - the split
// TestErrorSurfaceAbort* pair below locks both sides (issue #72, decisions/0024).
func newSurfaceHost(t *testing.T) (*Host, *captureLogger) {
	t.Helper()
	return newTestHost(t, Config{URL: testExternalURL})
}

// A host that never saw a navigation failure must keep rejecting the empty
// source: it is also what about:blank-adjacent opaque documents report, and
// admitting it unconditionally would hand every such frame the window controls.
func TestErrorSurfaceEmptySourceRejectedByDefault(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("an empty source must be rejected while no error surface is up")
	}
	if host.errorSurfaceMessageAllowed("https://evil.example/x") {
		t.Fatal("a non-empty foreign source must never pass the error-surface gate")
	}
}

// A navigation failure arms the surface immediately - before its load
// completes - because the injected diagnostics post from document creation,
// ahead of NavigationCompleted. Arming late would reject exactly the flurry
// issue #56 was reported with.
func TestErrorSurfaceAdmitsEmptySourceOnNavigationFailure(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if !noteFail(host, 0) {
		t.Fatal("the first navigation failure must ask for the surface to be shown")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface's empty-source messages must be admitted from the moment it is navigated to")
	}
	if host.errorSurfaceMessageAllowed("https://evil.example/x") {
		t.Fatal("arming the surface must not admit foreign origins")
	}
	if host.config.messageSourceTrusted("") {
		t.Fatal("an empty source must never be trusted for Config.Bridge, error surface or not")
	}
}

// The surface's own load completing is not a departure: the empty source stays
// admitted afterwards, which is when a human actually clicks the caption
// buttons.
func TestErrorSurfaceStaysAdmittedThroughItsOwnLoad(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0)

	if noteOK(host, 0) {
		t.Fatal("the surface's own successful load must not trigger another surface navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface must stay admitted after its own load completes")
	}
}

// A successful navigation away from the surface - Retry reaching the origin,
// or the frontend recovering - disarms it: whatever document is up now owns
// the window, and an empty source is foreign again.
func TestErrorSurfaceDisarmsWhenNavigationLeavesIt(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0) // failure: surface armed and navigated
	noteOK(host, 0)   // the surface's own load
	noteOK(host, 0)   // Retry reached the origin

	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("leaving the surface must disarm the empty-source admission")
	}
}

// A Retry that fails again re-shows the surface, and the admission must follow
// it through the whole loop: fail, load, fail again, load again.
func TestErrorSurfaceRearmsWhenRetryFailsAgain(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0) // failure: surface armed
	noteOK(host, 0)   // the surface's own load

	if !noteFail(host, 0) {
		t.Fatal("a failed Retry must show the surface again")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the re-shown surface must be admitted like the first one")
	}
}

// A failure completion while the surface's own load is in flight is not the
// surface dying: observed live (issue #68), a Retry against a still-down server
// delivers a second failure completion 23ms after the one that armed the
// surface. Reading it as the surface's own load failing - which is what this
// test's driving sequence used to lock, as
// TestErrorSurfaceDisarmsWhenTheSurfaceItselfFailsToLoad - sealed the admission
// and left the surface that then finished loading with dead caption buttons.
// The surface is a data: URL whose load realistically cannot fail, so the
// failure is absorbed: no re-navigation (the recursion guard), no seal, and the
// admission stays with the surface (decisions/0020; driven id-less, this locks
// the order-based fallback).
func TestErrorSurfaceStaysAdmittedWhenAFailureRacesItsOwnLoad(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	noteFail(host, 0) // failure: surface armed and navigated

	if noteFail(host, 0) {
		t.Fatal("a failure during the surface's load must not re-navigate: that is the loop the recursion guard exists for")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("a failed Retry's second completion must not disarm the surface on screen (issue #68)")
	}
	if strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("an absorbed failure must not be reported as the surface dying")
	}
	if !strings.Contains(logger.String(), "navigation failure absorbed") {
		t.Fatal("an absorbed failure must leave a debug trace, or a genuinely dead surface becomes undiagnosable")
	}
}

// The issue #68 ordering, as observed live: a Retry against a still-down server
// fails and re-arms the surface, its second failure completion lands while the
// surface loads, and the surface's own success completion arrives last. That
// success must be read as the surface's load - not as a navigation away - so
// the surface the user is looking at keeps working caption buttons.
func TestErrorSurfaceSurvivesAFailedRetry(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0) // initial load fails: surface armed
	noteOK(host, 0)   // the surface's own load
	noteFail(host, 0) // Retry fails: surface re-armed and re-navigated
	noteFail(host, 0) // the failed Retry's second completion
	noteOK(host, 0)   // the surface's own load, again

	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the re-shown surface must stay admitted after a failed Retry's double completion (issue #68)")
	}
	// The success above must have resolved the surface's load: the next success
	// is a departure and must disarm, or a recovered frontend inherits a stale
	// empty-source admission.
	noteOK(host, 0)
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a navigation away must still disarm the admission after an absorbed failure")
	}
}

// A rapid Retry double-click delivers at least one more failure completion
// before the surface's load resolves. Absorption is unbounded on purpose: an
// absorb-exactly-one bound would seal on the extra failure and re-create the
// dead surface one click deeper.
func TestErrorSurfaceSurvivesARapidRetryDoubleClick(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0) // initial load fails: surface armed
	noteOK(host, 0)   // the surface's own load
	noteFail(host, 0) // Retry click one fails: surface re-armed
	noteFail(host, 0) // its second completion
	noteFail(host, 0) // Retry click two's failure
	noteOK(host, 0)   // the surface's own load

	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("absorption must hold for every failure racing the surface's load, not just the first")
	}
}

// Absorption is total while the surface's load is in flight: however many
// failure completions a pathological schedule delivers, none may re-navigate
// and none may take the admission away from the surface that will finish
// loading.
func TestErrorSurfaceAbsorbsAFailureStorm(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	if !noteFail(host, 0) {
		t.Fatal("the first failure must arm and navigate the surface")
	}
	for i := 0; i < 8; i++ {
		if noteFail(host, 0) {
			t.Fatalf("failure %d during the surface's load asked to re-navigate: recursion", i+2)
		}
		if !host.errorSurfaceMessageAllowed("") {
			t.Fatalf("failure %d during the surface's load dropped the admission", i+2)
		}
	}
	noteOK(host, 0) // the surface's own load
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface must be admitted once its load completes, storm or no storm")
	}
	if strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("a storm inside the loading window must not be reported as the surface dying")
	}
}

// The identity tests below drive noteSurfaceNavigationStarting the way the
// NavigationStartingCallback would, with an arming failure first so the claim
// window is open. armAndClaim is that shared preamble: a foreign failure arms
// and asks for the surface, and the surface's own start is claimed under id.
func armAndClaim(t *testing.T, host *Host, foreignID, surfaceID uint64) {
	t.Helper()
	host.errorSurfaceURL = "data:text/html,surface"
	if !noteFail(host, foreignID) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, surfaceID) {
		t.Fatal("the surface's own navigation start must be claimed while the arming is pending")
	}
}

// The claim is double-gated: nothing is claimed before the host decides to
// navigate to the surface, a foreign http(s) start inside the window passes
// through unclaimed, and the tolerated data:-reporting variants all claim -
// exactly once.
func TestErrorSurfaceClaimsOnlyItsOwnNavigationStart(t *testing.T) {
	for _, tc := range []struct {
		name string
		uri  string
	}{
		{"exact data: URL", "data:text/html,surface"},
		{"empty (issue #56's erasure, if NavigationStarting shares it)", ""},
		{"truncated data: form", "data:text/html,other-shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, _ := newTestHost(t, Config{})
			host.errorSurfaceURL = "data:text/html,surface"

			if host.noteSurfaceNavigationStarting(tc.uri, 3) {
				t.Fatal("no start may be claimed before the surface is armed")
			}
			noteFail(host, 0) // arm: the claim window opens
			if host.noteSurfaceNavigationStarting("https://evil.example/", 4) {
				t.Fatal("a foreign http(s) start racing the surface must not be claimed")
			}
			if !host.noteSurfaceNavigationStarting(tc.uri, 5) {
				t.Fatal("the surface's own start must be claimed")
			}
			if host.errorSurfaceNavID != 5 {
				t.Fatalf("claimed navigation id = %d, want 5", host.errorSurfaceNavID)
			}
			if host.noteSurfaceNavigationStarting(tc.uri, 6) {
				t.Fatal("the claim must happen exactly once per arming")
			}
		})
	}
}

// The identity form of the issue #68 ordering: the straggler failure carries
// the failed Retry's id, not the surface's, so it is absorbed as positively
// foreign, and the surface's own completion - matched by id - admits it.
func TestErrorSurfaceIdentityAttributesTheRetryStraggler(t *testing.T) {
	host, logger := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6) // Retry id 5 failed; surface claimed as id 6

	if noteFail(host, 5) {
		t.Fatal("the failed Retry's second completion must not re-navigate")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("a completion attributed to the failed Retry must not disarm the surface")
	}
	if noteOK(host, 6) {
		t.Fatal("the surface's own load must not trigger another navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface must be admitted once its own completion arrives")
	}
	if strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("nothing in this ordering is the surface dying")
	}
}

// A foreign success landing while the surface is still loading - a queued
// navigation committing first - takes the screen, so the admission must drop
// with it; and when the surface then commits anyway, its own success must
// re-admit it. Under the order-based machines this ordering mis-attributed
// both completions and ended with the visible surface unadmitted (the
// success-echo tail of issue #68's class); identity resolves each completion
// to its own navigation (decisions/0021).
func TestErrorSurfaceSurvivesAForeignSuccessDuringItsLoad(t *testing.T) {
	host, _ := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6)

	if noteOK(host, 4) {
		t.Fatal("a foreign success must not trigger a surface navigation")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a foreign document committing must drop the empty-source admission")
	}
	if noteOK(host, 6) {
		t.Fatal("the surface's own late commit must not trigger another navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface committing after the foreign document must re-admit it: it is the document on screen")
	}
}

// A superseded surface Navigate completes OperationCanceled. That is cleanup,
// not the surface dying: no seal, no re-navigation, and the machine is clean
// enough afterwards to arm again on the next failure.
func TestErrorSurfaceSupersededNavigateCleansUpQuietly(t *testing.T) {
	host, logger := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6)
	noteOK(host, 7) // a newer navigation won the race and committed

	if noteCancel(host, 6) {
		t.Fatal("the superseded surface completion must not re-navigate")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a canceled surface navigation must not leave the admission armed against the winner")
	}
	if strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("a superseded Navigate is not the surface dying and must not be reported as one")
	}
	if !noteFail(host, 8) {
		t.Fatal("the machine must arm again once the superseded navigation is cleaned up")
	}
}

// When the surface's own completion - matched by id - reports a genuine
// failure, the surface really did die: nothing on screen is mullion's page, so
// the admission drops and nothing re-navigates. This is the seal the
// pre-identity machines could never target (0020 absorbed every failure in
// the window because it could not tell whose it was).
func TestErrorSurfaceSealsWhenItsOwnLoadFails(t *testing.T) {
	host, logger := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6)

	if noteFail(host, 6) {
		t.Fatal("the surface's own load failing must not re-navigate in a loop")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a surface that failed to load must not keep the empty source admitted")
	}
	if !strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("the dead-surface branch must say so in the log")
	}
}

// A superseded surface generation's canceled completion can arrive after a
// fresh failure has already armed the NEXT surface generation. The stale id
// must not be carried into that new arming: if it were, the old cancel would
// be mis-attributed to the new generation, unwind its claim window before its
// start ever fired, and leave the freshly loaded surface unadmitted - the
// dead-buttons symptom again, now on the identity path (found by the
// pre-merge audit of decisions/0021).
func TestErrorSurfaceLateCancelDoesNotDisturbANewArming(t *testing.T) {
	host, _ := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6) // generation one claimed as id 6
	noteOK(host, 7)            // a foreign navigation wins and commits

	if !noteFail(host, 8) {
		t.Fatal("a fresh failure after the foreign document must arm the next surface generation")
	}
	if noteCancel(host, 6) {
		t.Fatal("generation one's late cancel must not re-navigate")
	}
	if !host.noteSurfaceNavigationStarting("data:text/html,surface", 9) {
		t.Fatal("generation two's own start must still be claimable: the stale cancel must not have closed its claim window")
	}
	if noteOK(host, 9) {
		t.Fatal("generation two's own load must not trigger another navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("generation two's surface must be admitted once it commits")
	}
}

// A completion cannot precede its own navigation's start, so an identified
// completion arriving while the surface's start is still unclaimed is
// necessarily some other navigation's. A foreign success in that window must
// drop the admission without closing the claim window, so the surface's own
// late commit still re-admits it.
func TestErrorSurfaceIdentifiedCompletionsBeforeTheClaimAreForeign(t *testing.T) {
	host, _ := newSurfaceHost(t)
	host.errorSurfaceURL = "data:text/html,surface"
	if !noteFail(host, 5) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	if noteOK(host, 7) {
		t.Fatal("an identified foreign success must not trigger a surface navigation")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a foreign document committing must drop the admission even before the surface's start is claimed")
	}
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 6) {
		t.Fatal("the surface's own start must still be claimable after the foreign success")
	}
	noteOK(host, 6)
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface committing after the foreign document must re-admit it")
	}
}

// A single completion whose id read failed lands in the order-based fallback
// even though the surface's own id is known. The fallback must not destroy
// that identity: when the surface's real, identified completion arrives, it
// must still be attributed - not read as foreign because the fallback
// clobbered the claimed id.
func TestErrorSurfaceIdlessCompletionDoesNotDestroyTheClaimedIdentity(t *testing.T) {
	host, _ := newSurfaceHost(t)
	armAndClaim(t, host, 5, 6)
	noteOK(host, 0) // an id-less success: the fallback takes it as the surface's load

	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the fallback must admit the surface on the first success inside the window")
	}
	if noteOK(host, 6) {
		t.Fatal("the surface's identified completion must not trigger another navigation")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the surface's own identified success must keep it admitted, not read as a foreign departure")
	}
}

// A Navigate call that fails synchronously delivers no start and no
// completion, ever. The arming must unwind - holding the admission open with
// nothing left to resolve it was the completion-less residual decision 0020
// accepted - and the machine must stay able to arm on the next failure.
func TestErrorSurfaceNavigateFailureUnwindsTheArming(t *testing.T) {
	host, _ := newSurfaceHost(t)
	if !noteFail(host, 5) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	host.noteSurfaceNavigateFailed()

	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("an arming whose Navigate never started must not keep the empty source admitted")
	}
	if host.noteSurfaceNavigationStarting("data:text/html,x", 9) {
		t.Fatal("no claim may remain pending after the Navigate failed")
	}
	if !noteFail(host, 10) {
		t.Fatal("the machine must arm again after an unwound Navigate failure")
	}
}

// The four tests below lock what an aborted navigation means (issue #72,
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
	host.noteAndGateNavigation(host.config.trustedOrigin()+"/index.html?in=1", 3, true)

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

	if !noteFail(host, 3) {
		t.Fatal("an aborted navigation against a caller-served URL must still show the fallback surface")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the arming must admit the empty source the surface posts from")
	}
}

// The exemption is for the abort status alone. Any other failure in process is
// still a failure and still arms.
func TestErrorSurfaceOtherFailuresStillArmInProcess(t *testing.T) {
	host, _ := newTestHost(t, Config{})

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
	host.noteAndGateNavigation("https://evil.example/", 3, true)

	if !noteFail(host, 3) {
		t.Fatal("an aborted off-origin navigation must still show the fallback surface")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the arming must admit the empty source the surface posts from")
	}
}

// The exemption belongs to the navigation the runtime last reported starting.
// A completion for an older one cannot borrow that answer - nothing says where
// *it* was going - so it falls through and arms, the safe direction.
func TestErrorSurfaceAbortWithAStaleIdStillArms(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.noteAndGateNavigation(host.config.trustedOrigin()+"/a.html", 3, true)
	host.noteAndGateNavigation(host.config.trustedOrigin()+"/b.html", 4, true)

	if !noteFail(host, 3) {
		t.Fatal("an abort whose id is not the last start's must arm")
	}
}

// The seal - the surface's own load failing - must stay locked in the default
// in-process mode, not only in the external-URL mode the identity tests model.
// Without this, widening the abort exemption to noteSurfaceOwnOutcome would
// leave the whole suite green while a failed surface load stopped sealing.
func TestErrorSurfaceSealsInProcessToo(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 5) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
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

// The suppression is a no-op besides its log line, and that has to stay true:
// the surface itself can be on screen when an abort arrives - its Retry aborting
// is exactly issue #72's loop - and dropping the admission there would kill the
// visible surface's caption buttons, which is the issue #56 symptom.
func TestErrorSurfaceAbortLeavesAVisibleSurfaceAdmitted(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.errorSurfaceURL = "data:text/html,surface"
	if !host.noteNavigationOutcome(false, statusNone, 1) {
		t.Fatal("the arming failure must ask for the surface to be shown")
	}
	if !host.noteSurfaceNavigationStarting(host.errorSurfaceURL, 2) {
		t.Fatal("the surface's own start must be claimed")
	}
	if noteOK(host, 2) {
		t.Fatal("the surface's own load must not trigger another navigation")
	}

	// The surface is the document on screen. Its Retry aborts in process.
	host.noteAndGateNavigation(host.config.trustedOrigin()+"/index.html", 3, true)
	if noteFail(host, 3) {
		t.Fatal("the aborted Retry must not re-navigate")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the visible surface must keep its admission, or its caption buttons go dead")
	}
}

// TestGateCancelledCompletionDoesNotArmTheSurface locks the F1 fix: a navigation
// the PinNavigationToOrigin gate cancels completes with OperationCanceled, and
// that completion must be consumed as a deliberate cancel rather than reaching
// the error-surface machine, which would read a foreign failure as "navigate to
// the fallback surface" and tear down the live frontend (issue #6, decisions/0023).
func TestGateCancelledCompletionDoesNotArmTheSurface(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	// The gate cancels a foreign navigation and records its id. Routing the
	// target is not this test's concern and does not happen: every test host
	// stubs the system-browser seam (newTestHost, issue #76).
	if !host.shouldCancelNavigation("https://evil.example/", 7, true) {
		t.Fatal("gate did not cancel a foreign navigation")
	}
	if host.cancelledNavID != 7 {
		t.Fatalf("cancelledNavID = %d, want 7", host.cancelledNavID)
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
	if host.cancelledNavID != 0 {
		t.Fatalf("cancelledNavID = %d after the completion, want 0 (cleared)", host.cancelledNavID)
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
	host.errorSurfaceURL = "data:text/html,surface"
	host.errorSurfacePending = true

	// The surface's own start reported as an empty URI (a GetUri failure or the
	// runtime erasing the data: URI) is claimed, and must NOT be cancelled even
	// though "" is off-origin to the gate.
	if host.noteAndGateNavigation("", 4, false) {
		t.Fatal("the gate cancelled the surface's own navigation (empty URI)")
	}
	if host.errorSurfacePending {
		t.Fatal("the surface start was not claimed")
	}

	// Outside the claim window, a foreign navigation is still cancelled.
	if !host.noteAndGateNavigation("https://evil.example/", 5, true) {
		t.Fatal("the gate did not cancel a foreign navigation outside the claim")
	}
}
