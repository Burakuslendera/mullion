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
	if id == 0 {
		return host.planNavigationOutcomeObserved(
			false,
			webview2.WebErrorStatusConnectionAborted,
			navigationIdentity{},
		) != noErrorSurfacePlan
	}
	return host.noteNavigationOutcome(false, webview2.WebErrorStatusConnectionAborted, id)
}

func noteCancel(host *Host, id uint64) bool {
	if id == 0 {
		return host.planNavigationOutcomeObserved(
			false,
			webview2.WebErrorStatusOperationCanceled,
			navigationIdentity{},
		) != noErrorSurfacePlan
	}
	return host.noteNavigationOutcome(false, webview2.WebErrorStatusOperationCanceled, id)
}

func noteOK(host *Host, id uint64) bool {
	if id == 0 {
		host.noteNavigationSuccessObserved(navigationIdentity{})
		return false
	}
	return host.noteNavigationOutcome(true, statusNone, id)
}
func issueCurrentErrorSurface(t *testing.T, host *Host) errorSurfacePlan {
	t.Helper()
	plan := host.errorSurfacePlan
	if !host.issueErrorSurfaceNavigation(plan, "data:text/html,surface") {
		t.Fatal("classified fallback plan could not be issued")
	}
	return plan
}

func issueAndClaimEmptySurface(t *testing.T, host *Host, id uint64) {
	t.Helper()
	issueCurrentErrorSurface(t, host)
	if !host.noteSurfaceNavigationStarting("", id) {
		t.Fatal("a successfully read empty surface start must claim the issued generation")
	}
}

// newSurfaceHost builds a host whose frontend is served by the caller over a
// loopback URL. That is the setting the identity orderings below were measured
// in: the live timelines behind 0020 and 0021 are a dead loopback endpoint,
// where the ConnectionAborted noteFail sends is a real "could not load" and must
// arm the surface. Serving the embedded fs.FS in process there is no socket that
// can fail, so the same status is a benign abort instead - which the
// TestErrorSurfaceAbort* set in errorsurface_abort_windows_test.go locks from
// both sides (issue #72, decisions/0024).
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

// A navigation failure only makes the fallback pending. Admission begins after
// its NavigationStarting URI is successfully read and claimed.
func TestErrorSurfaceAdmitsEmptySourceOnlyAfterClaim(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if !noteFail(host, 0) {
		t.Fatal("the first navigation failure must ask for the surface to be shown")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("arming alone must not admit an empty source")
	}
	issueAndClaimEmptySurface(t, host, 0)
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("a successfully claimed empty-source surface must be admitted")
	}
	if host.errorSurfaceMessageAllowed("https://evil.example/x") {
		t.Fatal("claiming the surface must not admit foreign origins")
	}
	if host.source.messageSourceTrusted("") {
		t.Fatal("an empty source must never be trusted for Config.Bridge, error surface or not")
	}
}

// The surface's own load completing is not a departure: the empty source stays
// admitted afterwards, which is when a human actually clicks the caption
// buttons.
func TestErrorSurfaceStaysAdmittedThroughItsOwnLoad(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0)
	issueAndClaimEmptySurface(t, host, 0)

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
	issueAndClaimEmptySurface(t, host, 0)
	noteOK(host, 0) // the surface's own load
	host.noteAndGateNavigation(host.source.origin.text+"/index.html", 0)
	if !host.errorSurfaceSuspended || host.errorSurfaceMessageAllowed("") {
		t.Fatal("Retry start did not suspend the visible surface before completion")
	}
	noteOK(host, 0) // Retry reached the origin

	if host.errorSurfaceMessageAllowed("") || host.errorSurfaceSuspended {
		t.Fatal("leaving the surface must clear empty-source admission and its suspension")
	}
}

// A Retry that fails again re-shows the surface, and the admission must follow
// it through the whole loop: fail, load, fail again, load again.
func TestErrorSurfaceRearmsWhenRetryFailsAgain(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	noteFail(host, 0) // failure: surface armed
	issueAndClaimEmptySurface(t, host, 0)
	noteOK(host, 0) // the surface's own load

	if !noteFail(host, 0) {
		t.Fatal("a failed Retry must show the surface again")
	}
	issueAndClaimEmptySurface(t, host, 0)
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
	issueAndClaimEmptySurface(t, host, 0)

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
		t.Fatal("an absorbed failure must leave a trace, or a genuinely dead surface becomes undiagnosable")
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
	issueAndClaimEmptySurface(t, host, 0)
	noteOK(host, 0)   // the surface's own load
	noteFail(host, 0) // Retry fails: surface re-armed and re-navigated
	issueAndClaimEmptySurface(t, host, 0)
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
	issueAndClaimEmptySurface(t, host, 0)
	noteOK(host, 0)   // the surface's own load
	noteFail(host, 0) // Retry click one fails: surface re-armed
	issueAndClaimEmptySurface(t, host, 0)
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
	issueAndClaimEmptySurface(t, host, 0)
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
