//go:build windows

package host

import (
	"strings"
	"testing"
	"time"
)

func TestRenderWatchdogLogsTimeout(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: 10 * time.Millisecond})
	host.MarkFrontendPhase("frontend boot started")
	host.diagnostics.recordAsset(assetResponse{
		status:      200,
		contentType: "text/javascript; charset=utf-8",
		request:     assetRequest{path: "app.js", category: "asset"},
	}, "GET")
	host.diagnostics.recordBridge("Ping", "completed")
	host.startRenderWatchdog()

	logText := waitForLog(t, logger, "mullion: frontend render timeout", time.Second)
	if !strings.Contains(logText, "level=ERROR") {
		t.Fatalf("render timeout was not logged at ERROR:\n%s", logText)
	}
	// The summary is the whole point of the watchdog: it has to say enough to
	// tell "the document never arrived" apart from "it arrived and threw".
	for _, expected := range []string{
		"phase=frontend boot started",
		"asset=app.js",
		"asset_status=200",
		"script=1",
		"last_bridge=Ping:completed",
	} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("timeout summary missing %q:\n%s", expected, logText)
		}
	}
}

func TestRenderWatchdogStopsOnFrontendReady(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: 20 * time.Millisecond})
	host.startRenderWatchdog()
	host.MarkFrontendReady()
	time.Sleep(60 * time.Millisecond)

	logText := logger.String()
	if !strings.Contains(logText, "mullion: frontend ready") {
		t.Fatalf("frontend ready was not logged:\n%s", logText)
	}
	if strings.Contains(logText, "mullion: frontend render timeout") {
		t.Fatalf("watchdog fired after the frontend reported ready:\n%s", logText)
	}
}

// The two frontend-ready marks are exported with the any-goroutine contract
// (see the Host doc): they must post their WebView work to the UI thread, never
// run it on the caller's goroutine, because syncWebViewBounds touches the
// STA-bound WebView2 controller and races host.browser when called off-thread.
//
// The synchronous path is observable without a window: syncWebViewBounds with
// no browser embedded always logs a bounds-sync line ("skipped" here, since the
// host is not StartHidden), so its absence proves the sync now travels as a
// message instead of running inline. Before the fix both tests fail on that
// line; the posted message itself needs a pumping window and stays a live
// check.

func TestMarkFrontendReadyPostsBoundsSyncInsteadOfTouchingCOM(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendReady()

	text := logger.String()
	if !strings.Contains(text, "mullion: frontend ready") {
		t.Fatalf("log missing the frontend ready line:\n%s", text)
	}
	if strings.Contains(text, "webview bounds sync") {
		t.Fatalf("MarkFrontendReady ran the bounds sync on the caller's goroutine:\n%s", text)
	}
}

func TestMarkFrontendShellReadyPostsBoundsSyncInsteadOfTouchingCOM(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendShellReady()

	text := logger.String()
	if !strings.Contains(text, "mullion: frontend shell ready") {
		t.Fatalf("log missing the frontend shell ready line:\n%s", text)
	}
	if strings.Contains(text, "webview bounds sync") {
		t.Fatalf("MarkFrontendShellReady ran the bounds sync on the caller's goroutine:\n%s", text)
	}
}

// A second MarkFrontendReady is a no-op: the render watchdog is already
// cancelled and no further bounds sync is queued. Locks the early-return so the
// posted sync cannot multiply if a frontend calls ready() repeatedly.
func TestMarkFrontendReadyIsIdempotent(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendReady()
	host.MarkFrontendReady()

	// Count the exact line: with no window the bounds post may also log a
	// "frontend ready bounds post failed" warning, which a substring count
	// would miscount as a second ready.
	if got := strings.Count(logger.String(), "msg=mullion: frontend ready\n"); got != 1 {
		t.Fatalf("frontend ready logged %d times, want 1:\n%s", got, logger.String())
	}
}

// TestMarkFrontendShellReadyIsIdempotent is the sibling of
// TestMarkFrontendReadyIsIdempotent (issue #47). shellReady() is a reserved
// bridge method reachable from any page the bridge trusts, so the host must not
// rely on the frontend calling it once: without the gate every repeat re-logs
// the INFO line and posts another bounds sync that means nothing after the
// first call.
func TestMarkFrontendShellReadyIsIdempotent(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendShellReady()
	host.MarkFrontendShellReady()

	// Count the exact line: with no window the bounds post may also log a
	// "frontend shell ready bounds post failed" warning, which a substring
	// count would miscount as a second ready.
	if got := strings.Count(logger.String(), "msg=mullion: frontend shell ready\n"); got != 1 {
		t.Fatalf("frontend shell ready logged %d times, want 1:\n%s", got, logger.String())
	}
}

// TestShellReadyAndReadyAreIndependentSignals pins that the two readiness
// gates use separate flags. A copy-paste that gated MarkFrontendShellReady on
// frontendReady would pass both single-signal idempotency tests while silently
// swallowing the later ready() call - no timing summary, and a shellReady()
// would cancel the render watchdog. Here both marks run on one host and both
// INFO lines must appear exactly once.
func TestShellReadyAndReadyAreIndependentSignals(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendShellReady()
	host.MarkFrontendReady()

	if got := strings.Count(logger.String(), "msg=mullion: frontend shell ready\n"); got != 1 {
		t.Fatalf("frontend shell ready logged %d times, want 1:\n%s", got, logger.String())
	}
	if got := strings.Count(logger.String(), "msg=mullion: frontend ready\n"); got != 1 {
		t.Fatalf("frontend ready logged %d times, want 1 - a shared flag would swallow it:\n%s", got, logger.String())
	}
}

// TestStartRenderWatchdogRearmsShellReady pins the flag's lifecycle: the shell
// gate is per embed cycle, reset by startRenderWatchdog exactly like
// frontendReady, so a fresh embed can report shell readiness again. Removing
// the reset would leave the gate latched for the life of the host.
func TestStartRenderWatchdogRearmsShellReady(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: -1})

	host.MarkFrontendShellReady()
	host.startRenderWatchdog()
	host.MarkFrontendShellReady()

	if got := strings.Count(logger.String(), "msg=mullion: frontend shell ready\n"); got != 2 {
		t.Fatalf("frontend shell ready logged %d times across two embed cycles, want 2:\n%s", got, logger.String())
	}
}

func TestRenderWatchdogDisabled(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: -1})
	host.startRenderWatchdog()
	time.Sleep(40 * time.Millisecond)

	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("watchdog fired although RenderTimeout was negative")
	}
}

func TestNativeWindowStyleKeepsResizeWithoutNativeTitlebar(t *testing.T) {
	if wsNativeWindow&wsCaption != 0 {
		t.Fatal("wsNativeWindow includes WS_CAPTION")
	}
	if wsNativeWindow&wsSysMenu != 0 {
		t.Fatal("wsNativeWindow includes WS_SYSMENU")
	}
	for name, flag := range map[string]uint32{
		"WS_THICKFRAME":  wsThickFrame,
		"WS_MINIMIZEBOX": wsMinimizeBox,
		"WS_MAXIMIZEBOX": wsMaximizeBox,
	} {
		if wsNativeWindow&flag == 0 {
			t.Fatalf("wsNativeWindow missing %s", name)
		}
	}
}
