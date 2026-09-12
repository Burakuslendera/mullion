//go:build windows

package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The tagged terminal command's dispatch contract (issue #155, decision 0052):
// applyBrowserProcessExitTeardown runs through dispatchNativeHostCommand, whose
// HWND-and-token gate refuses a delivery once WM_DESTROY's teardown has cleared
// the stored HWND - even when the replayed delivery still carries the active
// Run's token and the destroyed window's handle. The destroy body therefore
// cannot run twice, and the refusal leaves the latched terminal cause and Run's
// ErrBrowserProcessExited return untouched.

func TestProcessExitSecondDeliveryAfterTeardownIsRefused(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1558)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	browser := host.newWebViewBrowser()
	host.browser = browser
	posts := 0
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		posts++
		return nil
	}

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})
	if posts != 1 {
		t.Fatalf("terminal posts = %d, want exactly one", posts)
	}

	// DestroyWindow dispatches WM_DESTROY synchronously inside the first
	// delivery; drive the same audited teardown seams it reaches.
	host.beginWindowDestroy(hwnd)
	host.windowDestroyTeardown()

	// The Run token is still the active session's; only the stored HWND is
	// gone. A replayed delivery of the same command - the token was valid when
	// it was posted - must be refused at the dispatch gate, never reaching the
	// command body a second time.
	current := host.currentRun()
	if current.token != run.token || current.hwnd != 0 {
		t.Fatalf("post-teardown admission = (token=%#x, hwnd=%#x), want the same token with a cleared hwnd",
			current.token, current.hwnd)
	}
	applied := 0
	host.applyNativeCommand = func(windowHandle, uint32, uintptr) uintptr {
		applied++
		return 0
	}
	host.windowProc(hwnd, wmNativeProcessExit, 0, run.token)

	if applied != 0 {
		t.Fatalf("the replayed delivery reached the command body %d time(s)", applied)
	}
	if strings.Contains(logger.String(), "level=WARN") {
		t.Fatalf("the refused delivery degraded into a warning:\n%s", logger.String())
	}
	if posts != 1 {
		t.Fatalf("terminal posts after the refusal = %d, want still one", posts)
	}
	if !host.windowDestroyed || host.browser != nil {
		t.Fatalf("teardown state after the refusal: destroyed=%t committed=%t, want true false",
			host.windowDestroyed, host.browser != nil)
	}
	if !host.browserExitTerminal {
		t.Fatal("the refusal cleared the latched terminal cause")
	}
	if !errors.Is(host.terminalOutcome(), ErrBrowserProcessExited) {
		t.Fatal("the refusal changed Run's terminal outcome")
	}
}
