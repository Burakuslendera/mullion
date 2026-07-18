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

	err := host.navigateOrTearDown(browser, func() error { return wantErr })

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

	if err := host.navigateOrTearDown(browser, func() error { return nil }); err != nil {
		t.Fatalf("navigateOrTearDown err = %v, want nil", err)
	}
	if host.browser != browser {
		t.Fatal("a successful navigation must keep the committed browser")
	}
	if browser.IsShuttingDown() {
		t.Fatal("a successful navigation must not tear the browser down")
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

	_ = host.navigateOrTearDown(browser, func() error { return errors.New("navigate failed") })
	time.Sleep(60 * time.Millisecond)

	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("the render watchdog fired after the failed navigation tore the webview down")
	}
}
