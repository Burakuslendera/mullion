//go:build windows

package host

import (
	"strings"
	"testing"
)

// Navigation-identity attribution for the error-surface machine (issue #6's
// follow-up, decisions/0021). Where the id-less tests in
// errorsurface_windows_test.go lock the order-based fallback, these drive
// noteSurfaceNavigationStarting so every completion carries an id, and pin what
// identity buys: the surface's own failure becomes reachable and seals, a
// straggler is positively foreign, and a stale generation cannot disturb the
// next arming.

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
