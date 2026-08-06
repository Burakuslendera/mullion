//go:build windows

package host

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// DPI awareness latches once per process, so a second enable used to come back
// as ERROR_ACCESS_DENIED even though the process was already in exactly the
// requested state - which made any second Host in one process fail at Run
// (issue #48, found live). Both calls must succeed: the first sets, the second
// recognises the already-correct context.
func TestDPIAwarenessEnableIsRepeatable(t *testing.T) {
	if err := enablePerMonitorV2DPIAwareness(); err != nil {
		t.Fatalf("first enable = %v, want nil", err)
	}
	if err := enablePerMonitorV2DPIAwareness(); err != nil {
		t.Fatalf("second enable = %v, want nil: an already-PMv2 process is success, not access denied", err)
	}
	if !alreadyPerMonitorV2DPIAware() {
		t.Fatal("the process just enabled PMv2; the Run-thread re-check must see it")
	}
}

func TestSupportedNewCrossesArchitectureGateIntoDPISetup(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("WebView2 hosting is supported only on Windows/amd64")
	}
	original := applyProcessDPIAwareness
	var calls int
	applyProcessDPIAwareness = func() error {
		calls++
		return nil
	}
	defer func() {
		applyProcessDPIAwareness = original
	}()

	host := New(Config{})
	if host.architectureErr != nil {
		t.Fatalf("supported New architecture error = %v", host.architectureErr)
	}
	if calls != 1 {
		t.Fatalf("supported New DPI setup calls = %d, want 1", calls)
	}
}

// The drain must actually remove a pending WM_QUIT from the thread queue: left
// there, it would poison the next Run on this thread (issues #48 and #54 - the
// loop is not running to consume it, having never started or having just died).
// WM_QUIT is a thread message, so the check needs a locked thread and no window.
func TestDrainThreadQuitMessageRemovesAPendingQuit(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		procPostQuitMessage.Call(0)
		drainThreadQuitMessage()

		var message msg
		got, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, wmQuit, wmQuit, pmRemove)
		if got != 0 {
			t.Error("a WM_QUIT survived the drain: the next message loop on this thread would exit immediately")
		}
	}()
	<-done
}

// With no window there is nothing to tear down: the zero-handle guard must
// return before the destroy, the drain, or the log line.
func TestDestroyWindowOutsideLoopIsANoOpWithoutAWindow(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.destroyWindowOutsideLoop("pre_loop_failure")

	if strings.Contains(logger.String(), "window teardown outside the loop") {
		t.Fatalf("teardown ran without a window:\n%s", logger.String())
	}
}
func TestDestroyWindowOutsideLoopDrainsQuitAfterHandleClear(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.windowDestroyed = true
	host.quitPending = true

	host.destroyWindowOutsideLoop("mid_embed_destroy")

	if host.quitPending {
		t.Fatal("post-create cleanup returned on a zero HWND without discharging pending WM_QUIT ownership")
	}
}

func TestMessageLoopExitTeardownDecisionCoversZeroAndMinusOne(t *testing.T) {
	for _, test := range []struct {
		result int32
		want   bool
	}{
		{result: -1, want: true},
		{result: 0, want: true},
		{result: 1, want: false},
	} {
		if got := messageLoopExitNeedsTeardown(test.result); got != test.want {
			t.Errorf("messageLoopExitNeedsTeardown(%d) = %v, want %v", test.result, got, test.want)
		}
	}
}
func TestWindowExitCleanupDecisionOwnsLiveWindowAndPendingQuit(t *testing.T) {
	tests := []struct {
		name                   string
		hwnd                   windowHandle
		destroyed              bool
		quitPending            bool
		wantDestroy, wantDrain bool
	}{
		{name: "no window", hwnd: 0},
		{name: "mid-embed destroy with cleared handle", quitPending: true, wantDrain: true},
		{name: "pre-loop or loop exit with live window", hwnd: 0x1234, wantDestroy: true, wantDrain: true},
		{name: "destroy already dispatched", hwnd: 0x1234, destroyed: true, quitPending: true, wantDrain: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			destroy, drain := windowExitCleanupDecision(test.hwnd, test.destroyed, test.quitPending)
			if destroy != test.wantDestroy || drain != test.wantDrain {
				t.Fatalf("decision = (destroy=%v, drain=%v), want (%v, %v)", destroy, drain, test.wantDestroy, test.wantDrain)
			}
		})
	}
}

// Shell readiness may arrive while WebView2's embed pump is still inside Run,
// before Run reaches startStartupShowGate. It must latch without posting to a
// zero/pre-window handle, then produce exactly one tagged show post after the
// gate starts.
func TestStartupShowGateLatchesEarlyReadinessUntilStart(t *testing.T) {
	host, logger := newTestHost(t, Config{ShowTimeout: time.Hour})
	if err := host.beginRun(); err != nil {
		t.Fatalf("beginRun = %v", err)
	}
	earlyToken := host.currentRun().token
	var posts []struct {
		hwnd    windowHandle
		message uint32
		token   uintptr
	}
	host.postNativeCommand = func(hwnd windowHandle, message uint32, _ uintptr, token uintptr) error {
		posts = append(posts, struct {
			hwnd    windowHandle
			message uint32
			token   uintptr
		}{hwnd, message, token})
		return nil
	}

	host.MarkFrontendShellReady()
	if len(posts) != 0 {
		t.Fatalf("early shell readiness posted before HWND/gate start: %#v", posts)
	}
	const hwnd = windowHandle(0x4545)
	host.mu.Lock()
	host.hwnd = hwnd
	host.mu.Unlock()
	host.startStartupShowGate()

	var showPosts int
	for _, post := range posts {
		if post.message == wmNativeShow {
			showPosts++
		}
	}
	if showPosts != 1 {
		t.Fatalf("posts after gate start = %#v, want one wmNativeShow", posts)
	}
	if posts[len(posts)-1].hwnd != hwnd || posts[len(posts)-1].token != earlyToken {
		t.Fatalf("latched show target = (hwnd=%#x token=%#x), want (%#x, %#x)", posts[len(posts)-1].hwnd, posts[len(posts)-1].token, hwnd, earlyToken)
	}
	host.startupMu.Lock()
	timer := host.startupShowTimer
	released := host.startupShowReleased
	host.startupMu.Unlock()
	if timer != nil || !released {
		t.Fatalf("gate after early release = (timer=%v released=%v), want detached/released", timer != nil, released)
	}
	if strings.Contains(logger.String(), "frontend_shell_timeout") {
		t.Fatal("early shell readiness emitted a false timeout warning")
	}
}

func TestFiredStartupShowTimerIsDetached(t *testing.T) {
	host, _ := newTestHost(t, Config{ShowTimeout: time.Hour})
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error { return nil }
	host.startStartupShowGate()
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupMu.Unlock()
	if timer == nil {
		t.Fatal("startup show gate did not arm")
	}
	timer.Stop()
	host.fireStartupShowGate(timer, host.currentRun())
	host.startupMu.Lock()
	retained := host.startupShowTimer
	host.startupMu.Unlock()
	if retained != nil {
		t.Fatal("startup show gate retained its fired timer")
	}
}

// A Host supports sequential Run sessions. beginRun is the headless boundary
// that restores per-window state; the callback itself is process-lifetime and
// must be retained rather than consuming another callback-table slot.
func TestBeginRunResetsReusableHostSessionState(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.windowDestroyed = true
	host.startupShowReleased = true
	host.wndProc = 0x1234
	host.log.Warn("previous session")
	host.frontendReady = true
	host.frontendShellReady = true
	oldTiming := host.startupTiming
	oldDiagnostics := host.diagnostics
	oldLog := host.log
	oldDiagnostics.recordFrontendPhase("previous")

	if err := host.beginRun(); err != nil {
		t.Fatalf("beginRun after a completed session = %v", err)
	}
	if host.windowDestroyed {
		t.Fatal("a new Run inherited the previous window's destruction state")
	}
	if host.startupShowReleased {
		t.Fatal("a new Run inherited the previous startup show release")
	}
	if host.wndProc != 0x1234 {
		t.Fatal("a new Run discarded the reusable process-lifetime window callback")
	}
	if host.log != oldLog || host.log.WarnCount() != 1 {
		t.Fatal("a new Run replaced the Logger sink while an older worker could still be using it")
	}
	if host.frontendReady || host.frontendShellReady {
		t.Fatal("a new Run inherited frontend readiness from the previous browser")
	}
	if host.startupTiming == oldTiming {
		t.Fatal("a new Run reused previous-session startup timing")
	}
	if host.diagnostics != oldDiagnostics || !strings.Contains(host.diagnostics.timeoutSummary(), "phase=startup") {
		t.Fatal("a new Run replaced the shared diagnostics pointer or retained its previous state")
	}
	if err := host.beginRun(); err == nil {
		t.Fatal("a concurrent Run on the same Host was accepted")
	}
	host.endRun()

	host.windowDestroyed = true
	host.startupShowReleased = true
	if err := host.beginRun(); err != nil {
		t.Fatalf("second sequential beginRun = %v", err)
	}
	host.endRun()
}
func TestBeginRunRejectsIncompleteBrowserTeardown(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.browser = webview2.New()
	if err := host.beginRun(); err == nil {
		t.Fatal("beginRun accepted a Host that still owns the previous Browser")
	}
}

func TestBeginRunRejectsUndrainedQuit(t *testing.T) {
	host, _ := newTestHost(t, Config{})
	host.quitPending = true
	if err := host.beginRun(); err == nil {
		t.Fatal("beginRun accepted a Host whose previous WM_QUIT was not drained")
	}
}

// TestWindowDestroyTeardownStopsTheTimersAndBrowser locks the WM_DESTROY
// teardown contract: both timers die with the window - a startup show gate
// left armed would fire after the destroy and post wmNativeShow to the dead
// HWND (issue #54's companion observation), and a surviving render watchdog
// would report a render timeout against a window that no longer exists - and
// a committed browser is shut down while the HWND is still alive. The
// watchdog's timer state is not inspectable, so its stop is observed the same
// way TestNavigateFailureStopsTheRenderWatchdog observes it: the timeout ERROR
// must never appear.
func TestWindowDestroyTeardownStopsTheTimersAndBrowser(t *testing.T) {
	host, logger := newTestHost(t, Config{ShowTimeout: time.Hour, RenderTimeout: 20 * time.Millisecond})
	host.startStartupShowGate()
	host.startRenderWatchdog()
	browser := webview2.New()
	host.browser = browser
	host.mu.Lock()
	host.hwnd = windowHandle(0x1234)
	host.mu.Unlock()
	host.beginWindowDestroy(windowHandle(0x9999))
	if host.window() != windowHandle(0x1234) || host.windowDestroyed {
		t.Fatal("WM_DESTROY for a different HWND cleared the active Host session")
	}
	host.beginWindowDestroy(windowHandle(0x1234))
	host.windowDestroyTeardown()

	host.startupMu.Lock()
	gateArmed := host.startupShowTimer != nil
	gateReleased := host.startupShowReleased
	host.startupMu.Unlock()
	if gateArmed {
		t.Fatal("the startup show gate survived WM_DESTROY; it would post wmNativeShow to a dead HWND")
	}
	if !gateReleased {
		t.Fatal("WM_DESTROY did not close the startup show gate")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("the WM_DESTROY teardown must shut the committed browser down")
	}
	if host.window() != 0 {
		t.Fatalf("WM_DESTROY left stored HWND %#x; exported calls could target a recycled window", host.window())
	}
	if host.browser != nil {
		t.Fatal("WM_DESTROY left a stale Browser reference")
	}
	if !host.windowDestroyed {
		t.Fatal("WM_DESTROY did not preserve destruction state for an in-flight embed")
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("the render watchdog fired after WM_DESTROY tore the window down")
	}
}
