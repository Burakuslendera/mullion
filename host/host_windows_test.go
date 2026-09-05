//go:build windows

package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

func TestRunPreStartFailureHasOneTerminalReporter(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	wantErr := errors.New("dpi setup failed")
	host.dpiAwarenessErr = wantErr

	err := host.runAfterRuntimeDiscovery()

	if !errors.Is(err, wantErr) {
		t.Fatalf("runAfterRuntimeDiscovery err = %v, want %v", err, wantErr)
	}
	logText := logger.String()
	if got := strings.Count(logText, wantErr.Error()); got != 1 {
		t.Fatalf("failure reason reports = %d, want exactly 1:\n%s", got, logText)
	}
	if got := strings.Count(logText, "level=ERROR"); got != 1 {
		t.Fatalf("terminal error lines = %d, want exactly 1:\n%s", got, logText)
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

// A WM_QUIT is owned by the thread that receives it, so this self-test runs in
// a child process. The child pins its current OS thread for the whole sequence
// and exits without unlocking it for reuse. It only checks and removes WM_QUIT;
// it never translates, dispatches, waits, creates a window, or queries a
// foreign queue.
const drainThreadQuitMessageHelperEnv = "MULLION_DRAIN_THREAD_QUIT_MESSAGE_HELPER"

func TestDrainThreadQuitMessageRemovesAPendingQuit(t *testing.T) {
	if os.Getenv(drainThreadQuitMessageHelperEnv) == "1" {
		runtime.LockOSThread()
		// Deliberately do not unlock: this child exits after the self-test, so
		// the queue-owning OS thread must never return for reuse.
		runDrainThreadQuitMessageChild(t)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable,
		"-test.run=^TestDrainThreadQuitMessageRemovesAPendingQuit$",
		"-test.count=1",
		"-test.v",
	)
	cmd.Env = append(os.Environ(), drainThreadQuitMessageHelperEnv+"=1")
	hideChildConsole(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("WM_QUIT self-test timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("WM_QUIT self-test failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--- PASS: TestDrainThreadQuitMessageRemovesAPendingQuit") {
		t.Fatalf("WM_QUIT self-test did not report its child test as passed:\n%s", output)
	}
}

func runDrainThreadQuitMessageChild(t *testing.T) {
	t.Helper()
	if threadQuitMessagePending() {
		t.Fatal("child queue already had a WM_QUIT before the self-test")
	}
	procPostQuitMessage.Call(0)
	if !threadQuitMessagePending() {
		t.Fatal("PostQuitMessage did not leave a WM_QUIT for the drain")
	}
	drainThreadQuitMessage()
	if threadQuitMessagePending() {
		t.Fatal("drainThreadQuitMessage left a WM_QUIT in the child queue")
	}
}

func threadQuitMessagePending() bool {
	var message msg
	got, _, _ := procPeekMessage.Call(
		uintptr(unsafe.Pointer(&message)), 0, wmQuit, wmQuit, pmNoRemove,
	)
	return got != 0
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

// TestWindowDestroyTeardownStopsTheTimersAndBrowser locks the extracted teardown
// seam: pending startup-timer state is cleared, no later watchdog timeout
// appears, the Browser helper enters shutdown, and stored HWND, Browser, and
// destruction state make their expected transitions. It does not dispatch a
// real WM_DESTROY or prove live HWND/controller ordering or races with callbacks
// that already fired.
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
