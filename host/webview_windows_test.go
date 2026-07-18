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

// The tests below lock the error-surface admission state machine (issue #56).
// The runtime reports a data: document's source as the empty string - measured
// live at both the event args and the core - so the fallback error surface can
// only be recognised by navigation state, and these transitions are what decide
// whether its caption buttons work. Each test walks noteNavigationOutcome the
// way NavigationCompletedCallback would.

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

	if !host.noteNavigationOutcome(false) {
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
	host.noteNavigationOutcome(false)

	if host.noteNavigationOutcome(true) {
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
	host.noteNavigationOutcome(false) // failure: surface armed and navigated
	host.noteNavigationOutcome(true)  // the surface's own load
	host.noteNavigationOutcome(true)  // Retry reached the origin

	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("leaving the surface must disarm the empty-source admission")
	}
}

// A Retry that fails again re-shows the surface, and the admission must follow
// it through the whole loop: fail, load, fail again, load again.
func TestErrorSurfaceRearmsWhenRetryFailsAgain(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.noteNavigationOutcome(false) // failure: surface armed
	host.noteNavigationOutcome(true)  // the surface's own load

	if !host.noteNavigationOutcome(false) {
		t.Fatal("a failed Retry must show the surface again")
	}
	if !host.errorSurfaceMessageAllowed("") {
		t.Fatal("the re-shown surface must be admitted like the first one")
	}
}

// When the surface itself fails to load, nothing on screen is mullion's own
// page, so the admission must drop with it - a stale allow against an unknown
// document is the hole the gate exists to close.
func TestErrorSurfaceDisarmsWhenTheSurfaceItselfFailsToLoad(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.noteNavigationOutcome(false) // failure: surface armed and navigated

	if host.noteNavigationOutcome(false) {
		t.Fatal("the surface failing to load must not re-navigate in a loop")
	}
	if host.errorSurfaceMessageAllowed("") {
		t.Fatal("a surface that failed to load must not keep the empty source admitted")
	}
	if !strings.Contains(logger.String(), "fallback error surface load failed") {
		t.Fatal("the dead-surface branch must say so in the log")
	}
}
