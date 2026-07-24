//go:build windows

package host

import (
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// The HWND and the thread's message queue: creating the window, pumping the
// loop, and tearing the window down when the loop is not there to do it. Split
// from host_windows.go, which keeps the Host type and the startup sequence that
// drives all three.
//
// windowproc_windows.go owns what happens inside the loop; this file owns the
// loop itself. The out-of-loop teardown (issues #48 and #54) belongs here
// because it is the same concern read backwards: the window and the thread
// queue must end as cleanly as they began.

func (host *Host) createWindow() error {
	host.log.Debug("mullion: module handle requested")
	instance, err := getModuleHandle()
	if err != nil {
		return err
	}
	host.instance = instance
	host.wndProc = newWindowCallback(host.windowProc, host.reportWindowProcPanic)

	// Centered on the primary work area, DPI-scaled (issue #59, decision 0018).
	// A failed resolution falls back to the shell's default position with the
	// unscaled size - the pre-#59 behaviour - and says so, because a window
	// that silently appears in the wrong place looks like a placement bug with
	// no evidence trail.
	x, y := uintptr(cwUseDefault), uintptr(cwUseDefault)
	width, height := host.config.Width, host.config.Height
	if place, ok := host.initialWindowPlacement(); ok {
		x, y = uintptr(place.X), uintptr(place.Y)
		width, height = place.Width, place.Height
		host.log.Debug(formatInitialPlacementLog(place))
	} else {
		host.log.Warn("mullion: initial placement unresolved, using the system default position")
	}

	host.log.Debug("mullion: win32 class/window create requested")
	hwnd, err := host.createWin32Window(host.config.ClassName, host.config.Title, instance, host.wndProc, x, y, width, height)
	if err != nil {
		return err
	}
	host.mu.Lock()
	host.hwnd = hwnd
	host.mu.Unlock()
	return nil
}

func (host *Host) messageLoop() error {
	var message msg
	for {
		result, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		switch int32(result) {
		case -1:
			host.log.Error("mullion: message loop failed, reason=" + logsafe.Reason(err))
			// The loop dies without ever reading the WM_QUIT a WM_DESTROY would
			// post: no teardown has run and none will, so without this the
			// browser process, the COM references and the class registration all
			// outlive Run - the same shape issue #39 closed for the Navigate
			// failure, on the abnormal exit instead (issue #54). The destroy
			// dispatches WM_DESTROY synchronously while the HWND is still alive.
			// Best effort by construction: the documented causes of -1 are a
			// corrupted queue or an invalid handle (unverified - this branch has
			// never been observed live), and on a wounded thread the destroy and
			// drain may themselves fail. Never worse than returning with
			// everything alive, and strictly better whenever the handle still
			// is.
			host.destroyWindowOutsideLoop("abnormal_loop_exit")
			return syscallError(err)
		case 0:
			host.log.Debug("mullion: message loop exited")
			return nil
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

func (host *Host) window() windowHandle {
	host.mu.RLock()
	defer host.mu.RUnlock()
	return host.hwnd
}

// destroyWindowOutsideLoop tears the HWND down when the message loop is not
// running to do it: Run failed after createWindow but before the loop started
// (issue #48), or the loop itself died on a GetMessage failure (issue #54).
//
// Without it the window outlives Run invisibly: the deferred
// unregisterWindowClass fails with ERROR_CLASS_HAS_WINDOWS against the live
// window, and a second Run in the same process cannot register the class again.
// DestroyWindow dispatches WM_DESTROY synchronously on this thread - the
// teardown case runs (shutting down a still-committed browser on the abnormal
// exit or an OnReady panic after the embed; on the other pre-loop paths
// host.browser is nil or already torn down) and posts WM_QUIT. With no loop left to consume it, that WM_QUIT would sit in
// the thread queue and poison the next message loop on this thread - a later
// Run would read it first and exit immediately, a silent one-shot failure - so
// the quit is drained right after the destroy. The stored handle is cleared
// last, so a stray exported call afterwards fails the zero-handle guard
// instead of posting to a recycled HWND.
func (host *Host) destroyWindowOutsideLoop(reason string) {
	hwnd := host.window()
	if hwnd == 0 {
		return
	}
	host.log.Debug("mullion: window teardown outside the loop, reason=" + reason)
	if host.windowDestroyed {
		// A Quit dispatched inside the embed pump already destroyed the real
		// window: the stored handle is stale and is not fed back to
		// DestroyWindow, on the off-chance the value was recycled. Only the
		// drain is still owed - the pump re-posts the quit it swallowed
		// mid-wait, so it is pending right now.
		drainThreadQuitMessage()
	} else {
		// Order is the contract: the destroy's WM_DESTROY posts the WM_QUIT, so
		// the drain must run after it - draining first would remove nothing and
		// leave the poison behind.
		procDestroyWindow.Call(uintptr(hwnd))
		drainThreadQuitMessage()
	}
	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
}

// drainThreadQuitMessage removes any pending WM_QUIT from the calling thread's
// queue. WM_QUIT is a thread message, not a window message, so it survives the
// window's destruction and would be the first thing the next GetMessage on
// this thread returns.
func drainThreadQuitMessage() {
	var message msg
	for {
		got, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, wmQuit, wmQuit, pmRemove)
		if got == 0 {
			return
		}
	}
}
