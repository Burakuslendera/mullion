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
	// Validate caller-controlled strings before reserving a pending Host identity.
	// createWin32Window repeats this at its ownership boundary so a direct change
	// cannot leave a pending registry entry after a fallible conversion.
	if _, _, err := prepareWindowStrings(host.config.ClassName, host.config.Title); err != nil {
		return err
	}
	host.instance = instance
	host.wndProc = sharedWindowProcCallback()
	token := sharedWindowProcHosts.reserve(host)
	created := false
	defer func() {
		if !created {
			sharedWindowProcHosts.rollback(token, host)
		}
	}()

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
	hwnd, err := host.createWin32Window(host.config.ClassName, host.config.Title, instance, host.wndProc, token, x, y, width, height)
	if err != nil {
		return err
	}
	created = true
	host.mu.Lock()
	host.hwnd = hwnd
	host.mu.Unlock()
	return nil
}

// messageLoopExitNeedsTeardown is the ownership decision shared by both loop
// exits. A zero result normally follows WM_DESTROY, but WM_QUIT is a thread
// message and can be posted without destroying the window; the teardown helper
// is idempotent and sees the cleared handle on the ordinary path. A minus-one
// result likewise leaves the window live unless we destroy it explicitly.
func messageLoopExitNeedsTeardown(result int32) bool {
	return result == 0 || result == -1
}

func (host *Host) messageLoop() error {
	var message msg
	for {
		result, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if messageLoopExitNeedsTeardown(int32(result)) {
			host.destroyWindowOutsideLoop("message_loop_exit")
		}
		switch int32(result) {
		case -1:
			host.log.Error("mullion: message loop failed, reason=" + logsafe.Reason(err))
			// The loop dies without ever reading the WM_QUIT a WM_DESTROY would
			// post: no teardown has run and none will, so without the ownership
			// decision above the browser process, COM references and class
			// registration all outlive Run (issue #54). The destroy dispatches
			// WM_DESTROY synchronously while the HWND is still alive. Best effort
			// by construction: the documented causes of -1 are a corrupted queue
			// or an invalid handle (unverified - this branch has never been
			// observed live), and on a wounded thread the destroy and drain may
			// themselves fail.
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

// destroyWindowOutsideLoop tears the HWND down whenever no further dispatch can
// be relied upon to do it: Run failed after createWindow but before the loop
// started (issue #48), GetMessage failed (issue #54), or WM_QUIT reached the
// queue without WM_DESTROY (issue #97).
//
// Without it the window outlives Run invisibly: the deferred
// unregisterWindowClass fails with ERROR_CLASS_HAS_WINDOWS against the live
// window, and a later Run in the same process cannot register the class again.
// DestroyWindow dispatches WM_DESTROY synchronously on this thread. The HWND is
// cleared at WM_DESTROY entry, but quit ownership remains independent: an embed
// pump can consume and re-post WM_QUIT before Run reaches its outer loop. The
// pending marker makes the final check drain that quit without ever feeding the
// cleared, potentially recycled handle back to DestroyWindow.
func windowExitCleanupDecision(hwnd windowHandle, destroyed, quitPending bool) (destroy, drain bool) {
	if hwnd == 0 {
		return false, quitPending
	}
	return !destroyed, true
}

func (host *Host) destroyWindowOutsideLoop(reason string) {
	hwnd := host.window()
	destroy, drain := windowExitCleanupDecision(hwnd, host.windowDestroyed, host.quitPending)
	if !destroy && !drain {
		return
	}
	host.log.Debug("mullion: window teardown outside the loop, reason=" + reason)
	if destroy {
		// Order is the contract: DestroyWindow synchronously runs WM_DESTROY,
		// which posts WM_QUIT, so the drain must follow it.
		procDestroyWindow.Call(uintptr(hwnd))
	}
	if drain {
		// A mid-embed WM_DESTROY may have cleared the HWND while its pump
		// re-posted WM_QUIT; quit ownership survives the handle clear.
		drainThreadQuitMessage()
	}
	host.mu.Lock()
	host.hwnd = 0
	host.quitPending = false
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
