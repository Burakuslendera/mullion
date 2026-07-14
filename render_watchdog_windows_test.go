//go:build windows

package mullion

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
