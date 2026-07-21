//go:build windows

package host

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// TestNavigateFailureUncommitsAndTearsDownBrowser locks the post-Embed error
// path. Once createWebView has committed host.browser, the only releaser of the
// browser's COM references is ShuttingDown from WM_DESTROY - and on the initial
// embed path a Navigate failure returns out of Run before the message loop
// starts, so WM_DESTROY never comes and the browser process leaks with COM
// still referenced past CoUninitialize.
//
// A fresh webview2.Browser has nil COM fields and ShuttingDown tolerates them,
// so this drives the real control flow without a runtime, the same way the
// registerEventsOrTearDown tests do for the in-Embed half. The actual Release
// calls are live-only; the load-bearing headless half is that a Navigate
// failure uncommits host.browser and runs the teardown at all.
func TestNavigateFailureUncommitsAndTearsDownBrowser(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.browser = browser
	wantErr := errors.New("navigate failed")

	err := host.navigateOrTearDown(func() error { return wantErr })

	if !errors.Is(err, wantErr) {
		t.Fatalf("navigateOrTearDown err = %v, want %v", err, wantErr)
	}
	if host.browser != nil {
		t.Fatal("a Navigate failure must uncommit host.browser, or ensureWebView reuses a torn-down browser on retry")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("a Navigate failure must tear the browser down, or the browser process and COM references outlive Run")
	}
}

// TestNavigateSuccessKeepsBrowser is the other half: success must not tear
// anything down, or every window would be destroyed at startup.
func TestNavigateSuccessKeepsBrowser(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.browser = browser

	if err := host.navigateOrTearDown(func() error { return nil }); err != nil {
		t.Fatalf("navigateOrTearDown err = %v, want nil", err)
	}
	if host.browser != browser {
		t.Fatal("a successful navigation must keep the committed browser")
	}
	if browser.IsShuttingDown() {
		t.Fatal("a successful navigation must not tear the browser down")
	}
}

// Embed pumps the message loop, so ensureWebView can be re-entered from inside
// its own create. The single-flight flag must make the inner call fail without
// running a second embed - two browsers would race for one host.browser commit
// and the loser would leak, browser process and all (issue #23, defect 1).
func TestEnsureWebViewRefusesAReentrantEmbed(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	var outerRuns, innerRuns int
	var innerErr error

	err := host.ensureWebViewWith("initial", func() error {
		outerRuns++
		innerErr = host.ensureWebViewWith("show", func() error {
			innerRuns++
			return nil
		})
		return nil
	})

	if err != nil {
		t.Fatalf("outer ensureWebViewWith err = %v, want nil", err)
	}
	if outerRuns != 1 {
		t.Fatalf("outer create ran %d times, want 1", outerRuns)
	}
	if innerErr == nil {
		t.Fatal("the re-entrant call must fail while an embed is in flight")
	}
	if innerRuns != 0 {
		t.Fatalf("inner create ran %d times, want 0: a second embed leaks a browser", innerRuns)
	}
}

// An already-embedded browser short-circuits before any guard: the post-commit
// show path relies on this returning nil without running create again.
func TestEnsureWebViewReturnsImmediatelyWhenEmbedded(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.browser = webview2.New()

	err := host.ensureWebViewWith("show", func() error {
		t.Error("create must not run when a browser is already embedded")
		return nil
	})
	if err != nil {
		t.Fatalf("ensureWebViewWith with an embedded browser err = %v, want nil", err)
	}
}

// The in-flight flag must clear on both exits, or one failed embed would
// refuse every retry for the life of the host.
func TestEnsureWebViewClearsTheInFlightFlag(t *testing.T) {
	host, _ := newTestHost(t, Config{})

	if err := host.ensureWebViewWith("initial", func() error { return errors.New("embed failed") }); err == nil {
		t.Fatal("a failing create must propagate its error")
	}
	var retried bool
	if err := host.ensureWebViewWith("show", func() error { retried = true; return nil }); err != nil {
		t.Fatalf("retry after a failed embed err = %v, want nil", err)
	}
	if !retried {
		t.Fatal("the retry must run create again: the failure path left the flag set")
	}
}

// A destroyed window has nothing to embed into: create must never run.
func TestEnsureWebViewRefusesAfterDestroy(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.windowDestroyed = true

	err := host.ensureWebViewWith("show", func() error {
		t.Error("create must not run against a destroyed window")
		return nil
	})
	if err == nil {
		t.Fatal("ensureWebView must refuse once the window is destroyed")
	}
}

// TestCommitRefusedAfterMidEmbedDestroy locks defect 2 of issue #23: a
// WM_DESTROY dispatched inside the embed pump skips ShuttingDown because
// host.browser is still nil, so committing the browser afterwards would strand
// it forever - its teardown moment has already passed. The commit must tear it
// down instead.
func TestCommitRefusedAfterMidEmbedDestroy(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()
	host.windowDestroyed = true

	err := host.commitEmbeddedBrowser(browser)

	if err == nil {
		t.Fatal("committing after a mid-embed destroy must fail")
	}
	if host.browser != nil {
		t.Fatal("a browser must not be committed to a destroyed window")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("the uncommitted browser must be torn down, or its COM references and process leak")
	}
}

func TestCommitAssignsTheBrowserOnALiveWindow(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	browser := webview2.New()

	if err := host.commitEmbeddedBrowser(browser); err != nil {
		t.Fatalf("commitEmbeddedBrowser err = %v, want nil", err)
	}
	if host.browser != browser {
		t.Fatal("a live window must receive the embedded browser")
	}
	if browser.IsShuttingDown() {
		t.Fatal("a committed browser must not be torn down")
	}
}

// The watchdog is armed immediately before Navigate, so the failure path must
// disarm it: with the webview torn down, a later "frontend render timeout"
// ERROR would point at a window that no longer exists.
func TestNavigateFailureStopsTheRenderWatchdog(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: 20 * time.Millisecond})
	browser := webview2.New()
	host.browser = browser
	host.startRenderWatchdog()

	_ = host.navigateOrTearDown(func() error { return errors.New("navigate failed") })
	time.Sleep(60 * time.Millisecond)

	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("the render watchdog fired after the failed navigation tore the webview down")
	}
}

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

// statusNone stands in for the status of a successful completion; the machine
// must not read it.
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
	host, logger := newTestHost(t, Config{})
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
	host, _ := newTestHost(t, Config{})
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
	host, logger := newTestHost(t, Config{})
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
	host, logger := newTestHost(t, Config{})
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
	host, _ := newTestHost(t, Config{})
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
	host, _ := newTestHost(t, Config{})
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
	host, _ := newTestHost(t, Config{})
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
	host, _ := newTestHost(t, Config{})
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
