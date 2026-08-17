//go:build windows

package host

import "github.com/Burakuslendera/mullion/internal/logsafe"

func (host *Host) windowProc(hwnd windowHandle, message uint32, wParam, lParam uintptr) uintptr {
	if isNativeHostCommand(message) {
		return host.dispatchNativeHostCommand(hwnd, message, wParam, lParam)
	}
	switch message {
	case wmClose:
		host.log.Debug("mullion: close requested")
		host.logNativeWindowActionState("close_before", hwnd)
		if host.config.OnClose != nil && host.config.OnClose() {
			host.log.Debug("mullion: close intercepted")
			host.logNativeWindowActionState("close_intercepted", hwnd)
			return 0
		}
		host.log.Debug("mullion: close allowed")
	case wmDestroy:
		// Clear the exported-call target before any teardown work: a concurrent
		// call must fail its zero-handle guard as soon as WM_DESTROY begins, and
		// an embed pump must observe that the window was destroyed.
		host.beginWindowDestroy(hwnd)
		host.log.Debug("mullion: destroy requested")
		// PostQuitMessage must run even if teardown panics. The window procedure
		// is panic-guarded (win32_call_windows.go), so a panic in ShuttingDown
		// would otherwise be recovered into DefWindowProc - which posts no quit -
		// and the message loop would block forever with the HWND already gone, so
		// Run never returns. runWindowDestroy posts the quit from a defer, which
		// runs during the panic unwind (before the guard's recover), then lets the
		// panic propagate so the guard still logs it.
		runWindowDestroy(host.windowDestroyTeardown, func() { procPostQuitMessage.Call(0) })
		return 0
	case wmNCCalcSize:
		if nativeFrameProfileHandlesNCCalcSize(activeNativeFrameProfile()) {
			result := host.applyNativeNCCalcClientRect(hwnd, wParam, lParam)
			if result.reason != "" {
				host.log.Warn("mullion: nccalc client extension degraded, action=" + result.action.String() + ", reason=" + logsafe.Message(result.reason))
			}
			if result.action == nativeNCCalcClaim {
				return 0
			}
		}
	case wmNCHitTest:
		hit := host.nativeHitTest(hwnd, lParam)
		policy := activeDWMCaptionPolicyForWindow(hwnd)
		tooltipTraceReady := nativeTooltipTraceReady()
		captionHit := nativeCaptionButtonHitIfNeeded(
			host,
			hwnd,
			lParam,
			policy,
			tooltipTraceReady,
			nativeCaptionPassthroughDiagnosticEnabled(),
			nativeCaptionButtonHitForWindow,
		)
		decision := host.nativeDWMCaptionHitTestDecisionForPolicy(hwnd, message, wParam, lParam, policy, hit, captionHit)
		host.traceNativeTooltipHitDecision(hwnd, message, lParam, hit, captionHit, decision)
		return decision.result
	case wmSetCursor, wmNCMouseMove, wmNCMouseHover, wmNCMouseLeave:
		decision := host.nativeDWMCaptionMessageDecision(hwnd, message, wParam, lParam)
		host.traceNativeTooltipMessageDecision(hwnd, message, wParam, lParam, decision)
		if decision.override {
			return decision.result
		}
		return host.callDefaultWindowProc(hwnd, message, wParam, lParam)
	case wmGetMinMaxInfo:
		if host.applyMonitorWorkArea(hwnd, lParam) {
			return 0
		}
	case wmInitMenu:
		// DefWindowProc does not refresh the system menu item states from the window
		// state, so they are forced here, just before the menu is shown.
		host.syncTabTitlebarSystemMenuState("wm_initmenu")
	case wmEraseBkgnd:
		// The WebView covers the entire client area, so erasing the background paints
		// nothing that survives; claiming the message avoids a flash of the class brush.
		return 1
	case wmDPIChanged:
		if host.applyDPIChangedRect(hwnd, wParam, lParam) {
			// The window rect is now the new monitor's; the WebView2 content scale
			// must move with it, or the frontend keeps rendering at the old
			// monitor's DPI. wParam's low word carries the new DPI.
			host.syncRasterizationScale("wm_dpi_changed", uint32(wParam&0xffff))
			host.syncWebViewBounds("wm_dpi_changed")
			return 0
		}
		host.log.Warn("mullion: dpi bounds sync failed")
	case wmSize:
		host.syncBoundsForWindowMessage("wm_size")
		host.syncTabTitlebarSystemMenuState("wm_size")
	case wmMove:
		host.syncBoundsForWindowMessage("wm_move")
	case wmMoving:
		// WM_MOVING exposes a proposed rect before Windows applies it. WM_MOVE owns
		// the position sync after DefWindowProc; syncing here would notify WebView2
		// against the previous parent location.
		// A Windows 11 bug can roll an interrupted maximised drag-down back to
		// its pre-loop placement after a shell overlay is cancelled; stock
		// Notepad reproduces it. This state gates only mullion's pointer
		// overlays. Keep DefWindowProc's cancellation path authoritative rather
		// than issuing a synthetic maximise/restore command here.
	case wmEnterSizeMove:
		host.setMoveSizeActive(true)
	case wmExitSizeMove:
		host.setMoveSizeActive(false)
		host.requestDeferredBoundsSync(boundsSyncWParamDeferredExitSizeMove)
	}
	return host.callDefaultWindowProc(hwnd, message, wParam, lParam)
}

// callDefaultWindowProc preserves the production default dispatch while letting
// headless routing tests observe fallback without invoking user32.
func (host *Host) callDefaultWindowProc(hwnd windowHandle, message uint32, wParam, lParam uintptr) uintptr {
	if host.defaultWindowProc != nil {
		return host.defaultWindowProc(hwnd, message, wParam, lParam)
	}
	return defWindowProc(hwnd, message, wParam, lParam)
}

func isNativeHostCommand(message uint32) bool {
	switch message {
	case wmNativeShow, wmNativeHide, wmNativeQuit, wmNativeMinimize,
		wmNativeMaxToggle, wmNativeStartDrag, wmNativeStartResize,
		wmNativeSyncBounds, wmNativeSetTitle:
		return true
	default:
		return false
	}
}

// dispatchNativeHostCommand is the sole mutation boundary for private window
// commands. Both the callback HWND and lParam token must still name the active
// Run before any command-specific log, browser call or Win32 mutation occurs.
func (host *Host) dispatchNativeHostCommand(hwnd windowHandle, message uint32, wParam, token uintptr) uintptr {
	host.mu.RLock()
	current := host.hwnd
	currentToken := host.activeRunToken
	host.mu.RUnlock()
	if hwnd == 0 || hwnd != current || token == 0 || token != currentToken {
		return 0
	}
	if message == wmNativeStartResize && !isResizeHitTest(wParam) {
		host.log.Warn("mullion: resize rejected, reason=invalid hit")
		return 0
	}
	if host.applyNativeCommand != nil {
		result := host.applyNativeCommand(hwnd, message, wParam)
		if message == wmNativeShow && result == 0 {
			host.retryStartupShowAfterFailedApplication(hwnd, token)
		}
		return result
	}
	switch message {
	case wmNativeShow:
		if host.showFromMessage() {
			return 1
		}
		host.retryStartupShowAfterFailedApplication(hwnd, token)
	case wmNativeHide:
		host.hideFromMessage()
	case wmNativeQuit:
		host.log.Debug("mullion: quit applying")
		procDestroyWindow.Call(uintptr(hwnd))
	case wmNativeMinimize:
		host.minimizeFromMessage()
	case wmNativeMaxToggle:
		host.toggleMaximiseFromMessage()
	case wmNativeStartDrag:
		host.startDragFromMessage()
	case wmNativeStartResize:
		host.startResizeFromMessage(int32(wParam))
	case wmNativeSyncBounds:
		host.syncWebViewBounds(boundsSyncSourceFromWParam(wParam))
	case wmNativeSetTitle:
		host.warnIf("set title", setWindowTextPointer(hwnd, wParam))
	}
	return 0
}

// runWindowDestroy runs the WM_DESTROY teardown and guarantees quit() runs even
// if teardown panics: quit is deferred, so it fires during the panic unwind, and
// the panic then propagates for the window procedure's guard to log. Extracted so
// the "quit always posts" invariant is unit-testable without a window - a skipped
// quit would hang the message loop (see the wmDestroy case).
func runWindowDestroy(teardown, quit func()) {
	defer quit()
	teardown()
}
func (host *Host) beginWindowDestroy(hwnd windowHandle) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.hwnd != hwnd {
		return
	}
	host.windowDestroyed = true
	host.quitPending = true
	host.hwnd = 0
}

// windowDestroyTeardown performs the teardown invoked by WM_DESTROY. Its
// extracted headless seam covers timer state, absence of a later watchdog
// timeout, Browser helper shutdown state, and stored Host transitions; it does
// not exercise real WM_DESTROY dispatch, live HWND/controller ordering, or
// already-fired callback races. The browser reference is cleared only after a
// successful shutdown; if shutdown panics, beginRun refuses to reuse the
// incompletely torn-down Host.
func (host *Host) windowDestroyTeardown() {
	host.stopRenderWatchdog()
	host.stopStartupShowGate()
	if host.browser != nil {
		// Tear the control down while the native parent is in WM_DESTROY.
		// Closing the controller later orphans the runtime's child windows.
		host.log.Debug("mullion: webview2 shutdown requested")
		host.browser.ShuttingDown()
		host.browser = nil
	}
}
