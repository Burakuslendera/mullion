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

// teardownOutsideLoop's ordering is the contract: the destroy's WM_DESTROY
// posts the WM_QUIT, so draining before the destroy would remove nothing and
// leave the thread queue poisoned for the next Run on this thread (issues #48
// and #54 - the loop is not running to consume it, having never started or
// having just died).
func TestTeardownOutsideLoopDrainsAfterTheDestroy(t *testing.T) {
	var order []string
	teardownOutsideLoop(
		func() { order = append(order, "destroy") },
		func() { order = append(order, "drain") },
	)
	if len(order) != 2 || order[0] != "destroy" || order[1] != "drain" {
		t.Fatalf("teardown order = %v, want [destroy drain]", order)
	}
}

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

// The drain must actually remove a pending WM_QUIT from the thread queue - the
// ordering test above proves when it runs, this proves what it does. WM_QUIT
// is a thread message, so the check needs a locked thread and no window.
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

	host.windowDestroyTeardown()

	host.startupMu.Lock()
	gateArmed := host.startupShowTimer != nil
	host.startupMu.Unlock()
	if gateArmed {
		t.Fatal("the startup show gate survived WM_DESTROY; it would post wmNativeShow to a dead HWND")
	}
	if !browser.IsShuttingDown() {
		t.Fatal("the WM_DESTROY teardown must shut the committed browser down")
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(logger.String(), "mullion: frontend render timeout") {
		t.Fatal("the render watchdog fired after WM_DESTROY tore the window down")
	}
}
