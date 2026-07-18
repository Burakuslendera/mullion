//go:build windows

package host

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// teardownBeforeLoop's ordering is the contract: the destroy's WM_DESTROY
// posts the WM_QUIT, so draining before the destroy would remove nothing and
// leave the thread queue poisoned for the next Run on this thread (issue #48).
func TestTeardownBeforeLoopDrainsAfterTheDestroy(t *testing.T) {
	var order []string
	teardownBeforeLoop(
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
func TestDestroyWindowBeforeLoopIsANoOpWithoutAWindow(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.destroyWindowBeforeLoop()

	if strings.Contains(logger.String(), "pre-loop window teardown") {
		t.Fatalf("teardown ran without a window:\n%s", logger.String())
	}
}
