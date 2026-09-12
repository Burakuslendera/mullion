//go:build windows

package host

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// ProcessFailed(BrowserProcessExited) headless contract (issue #155, decision
// 0052). The runtime's own state transition - a closed WebView, the vendor
// contract that only recreation can recover - is documented, not reproducible
// here: the suite never kills a browser process and never creates a window
// (decision 0006). What these tests own is the host policy on top of it: the
// production callback latches exactly one tagged terminal command, the audited
// WM_DESTROY teardown removes every ownership field, the committed-browser
// guard can no longer present a closed WebView as embedded, Run reports
// ErrBrowserProcessExited instead of a false normal close, and every other
// kind keeps the observation-only wait policy.

func TestProcessFailedBrowserExitPostsOneTaggedTerminalCommand(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1551)
	run := beginHeadlessLifecycleRun(t, host, hwnd)
	browser := host.newWebViewBrowser()
	host.browser = browser

	var posts []uint32
	host.postNativeCommand = func(got windowHandle, message uint32, wParam, token uintptr) error {
		if got != hwnd || wParam != 0 || token != run.token {
			t.Fatalf("terminal post = (hwnd=%#x, wParam=%#x, token=%#x), want (hwnd=%#x, 0, %#x)",
				got, wParam, token, hwnd, run.token)
		}
		posts = append(posts, message)
		return nil
	}

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})
	// A duplicate event must not schedule a second teardown: the tag is the
	// exactly-once guard, and the command dispatch plus WM_DESTROY are
	// idempotent behind it.
	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})

	if len(posts) != 1 || posts[0] != wmNativeProcessExit {
		t.Fatalf("terminal posts = %v, want exactly one %#x", posts, wmNativeProcessExit)
	}
	if !host.browserExitTerminal {
		t.Fatal("browser exit did not latch the terminal cause")
	}
	// The callback runs inside the runtime's event dispatch: it must stay
	// observation-only and never tear down inline.
	if host.browser != browser || browser.IsShuttingDown() || host.windowDestroyed {
		t.Fatal("the callback performed inline teardown instead of posting the terminal command")
	}
	logged := logger.String()
	if !strings.Contains(logged, "level=ERROR msg=mullion: webview2 process failed, kind=0") {
		t.Fatalf("browser exit was not reported at ERROR:\n%s", logged)
	}
	if !errors.Is(host.browserExitTerminalOutcome(), ErrBrowserProcessExited) {
		t.Fatal("latched browser exit did not become Run's terminal outcome")
	}
}

func TestProcessFailedBrowserExitTerminalLeavesNoLiveHostWithAClosedWebView(t *testing.T) {
	host, logger := newTestHost(t, Config{RenderTimeout: time.Hour})
	const hwnd = windowHandle(0x1552)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := host.newWebViewBrowser()
	host.browser = browser
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error { return nil }

	// The strand the defect produces: browser committed, frontend ready, the
	// ready path has stopped the watchdog. Re-arm one so the terminal teardown
	// must stop it again.
	host.MarkFrontendReady()
	host.startRenderWatchdog()
	host.renderMu.Lock()
	generation := host.renderGeneration
	armed := host.renderTimer != nil
	host.renderMu.Unlock()
	if !armed {
		t.Fatal("watchdog was not armed for the terminal teardown")
	}

	// Before the transition runs, the committed guard still presents the dead
	// control as embedded - the exact defect mechanism.
	if err := host.ensureWebViewWith("pre-terminal", func() error {
		t.Fatal("pre-terminal re-embed attempted a second embed")
		return nil
	}); err != nil {
		t.Fatalf("committed guard refused the dead browser before the transition: %v", err)
	}

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})
	// DestroyWindow dispatches WM_DESTROY synchronously; drive the same audited
	// seams, then the loop-exit drain that clears quit ownership.
	host.beginWindowDestroy(hwnd)
	host.windowDestroyTeardown()
	host.destroyWindowOutsideLoop("terminal_exit_drain")

	host.renderMu.Lock()
	stopped := host.renderTimer == nil && host.renderGeneration != generation
	host.renderMu.Unlock()
	if !stopped {
		t.Fatal("terminal teardown did not stop the render watchdog and bump its generation")
	}
	if host.browser != nil || !browser.IsShuttingDown() {
		t.Fatalf("terminal ownership after teardown: committed=%t shuttingDown=%t, want false true",
			host.browser == browser, browser.IsShuttingDown())
	}
	// The committed-browser guard can no longer masquerade: the destroyed
	// window refuses every re-embed without reaching the embed seam.
	reEmbedded := false
	if err := host.ensureWebViewWith("post-terminal", func() error {
		reEmbedded = true
		return nil
	}); err == nil || reEmbedded {
		t.Fatalf("destroyed window allowed a post-terminal embed: err=%v reEmbedded=%t", err, reEmbedded)
	}
	if !errors.Is(host.browserExitTerminalOutcome(), ErrBrowserProcessExited) {
		t.Fatal("terminal teardown did not keep the browser-exit run outcome")
	}
	// A stale timer identity after the teardown must stay inert.
	host.fireRenderWatchdog(generation, host.currentRun())
	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatalf("stale watchdog identity fired after the terminal teardown:\n%s", logger.String())
	}

	// Next-Run isolation: the terminal session leaves nothing behind.
	recycleHeadlessLifecycleRun(t, host, hwnd+0x100)
	if host.browserExitTerminal {
		t.Fatal("terminal cause leaked into the next session")
	}
	if host.browser != nil {
		t.Fatal("browser ownership leaked into the next session")
	}
	if host.browserExitTerminalOutcome() != nil {
		t.Fatal("next session inherited the terminal outcome")
	}
}

func TestProcessFailedOtherKindsKeepTheObservationOnlyPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind webview2.ProcessFailedKind
	}{
		{"render process exited", webview2.ProcessFailedKindRenderProcessExited},
		{"render process unresponsive", webview2.ProcessFailedKindRenderProcessUnresponsive},
		{"future unnamed kind", webview2.ProcessFailedKind(7)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, logger := newTestHost(t, Config{})
			beginHeadlessLifecycleRun(t, host, windowHandle(0x1553))
			browser := host.newWebViewBrowser()
			host.browser = browser
			host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
				t.Fatal("a non-terminal kind scheduled the terminal teardown")
				return nil
			}

			// The runtime recovers these kinds itself (a new renderer, an error
			// page, or an advisory), so one event - and a repeated one - must
			// stay observation-only.
			browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: tc.kind})
			browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: tc.kind})

			if host.browserExitTerminal {
				t.Fatal("a non-terminal kind latched the terminal cause")
			}
			if host.browser != browser || browser.IsShuttingDown() || host.windowDestroyed {
				t.Fatal("a non-terminal kind tore down the live control")
			}
			want := "level=ERROR msg=mullion: webview2 process failed, kind=" + formatInt32(int32(tc.kind))
			if got := strings.Count(logger.String(), want); got != 2 {
				t.Fatalf("kind observations = %d, want one per event:\n%s", got, logger.String())
			}
			if host.browserExitTerminalOutcome() != nil {
				t.Fatal("a non-terminal kind became Run's terminal outcome")
			}
		})
	}
}

func TestProcessFailedKindGetterFailureStaysObservationOnly(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	beginHeadlessLifecycleRun(t, host, windowHandle(0x1554))
	browser := host.newWebViewBrowser()
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		t.Fatal("a failed kind getter scheduled the terminal teardown")
		return nil
	}

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{KindErr: errors.New("kind unavailable")})

	if host.browserExitTerminal {
		t.Fatal("a failed kind getter latched the terminal cause")
	}
	if !strings.Contains(logger.String(), "event=ProcessFailed, getter=GetProcessFailedKind") {
		t.Fatalf("kind getter failure was not reported:\n%s", logger.String())
	}
}

func TestProcessFailedBrowserExitAfterDestroyRecordsOutcomeWithoutPost(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1555)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := host.newWebViewBrowser()
	host.browser = browser
	// WM_DESTROY entered: the ordinary teardown already owns the outcome, and
	// there is no live window left to receive a command.
	host.beginWindowDestroy(hwnd)
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		t.Fatal("a terminal request without a live window posted a command")
		return nil
	}

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})

	if !host.browserExitTerminal {
		t.Fatal("terminal cause was not recorded for the run outcome")
	}
	if strings.Contains(logger.String(), "level=WARN") {
		t.Fatalf("the skipped post degraded into a warning:\n%s", logger.String())
	}
	if !errors.Is(host.browserExitTerminalOutcome(), ErrBrowserProcessExited) {
		t.Fatal("browser exit after destroy did not become Run's terminal outcome")
	}
}

func TestProcessFailedAfterBrowserShutdownIsIgnored(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1557)
	beginHeadlessLifecycleRun(t, host, hwnd)
	browser := host.newWebViewBrowser()
	host.browser = browser
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		t.Fatal("a shut-down browser scheduled the terminal teardown")
		return nil
	}
	browser.ShuttingDown()

	browser.ProcessFailedCallback(webview2.ProcessFailedObservation{Kind: webview2.ProcessFailedKindBrowserProcessExited})

	if host.browserExitTerminal {
		t.Fatal("a shut-down browser latched the terminal cause")
	}
	// The observation itself is real and keeps its ERROR report.
	if !strings.Contains(logger.String(), "level=ERROR msg=mullion: webview2 process failed, kind=0") {
		t.Fatalf("shut-down browser observation lost its report:\n%s", logger.String())
	}
}

func TestProcessExitCommandAppliesOnlyForTheOriginatingRun(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1556)
	run := beginHeadlessLifecycleRun(t, host, hwnd)

	host.windowProc(hwnd, wmNativeProcessExit, 0, run.token+1)
	if strings.Contains(logger.String(), "browser process exit terminal applying") {
		t.Fatal("a stale token applied the terminal teardown")
	}

	host.windowProc(hwnd, wmNativeProcessExit, 0, run.token)
	if !strings.Contains(logger.String(), "browser process exit terminal applying") {
		t.Fatal("the originating run's command did not apply the terminal teardown")
	}
}
