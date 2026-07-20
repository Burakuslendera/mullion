//go:build windows

package host

func (host *Host) windowProc(hwnd windowHandle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmNativeShow:
		if host.showFromMessage() {
			return 1
		}
		return 0
	case wmNativeHide:
		host.hideFromMessage()
		return 0
	case wmNativeQuit:
		host.log.Debug("mullion: quit applying")
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmNativeMinimize:
		host.minimizeFromMessage()
		return 0
	case wmNativeMaxToggle:
		host.toggleMaximiseFromMessage()
		return 0
	case wmNativeStartDrag:
		host.startDragFromMessage()
		return 0
	case wmNativeStartResize:
		host.startResizeFromMessage(int32(wParam))
		return 0
	case wmNativeSyncBounds:
		host.syncWebViewBounds(boundsSyncSourceFromWParam(wParam))
		return 0
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
		// Recorded before the teardown so an embed whose pump dispatched this
		// message can see, once Embed returns, that its window is gone and the
		// browser must be torn down instead of committed (decision 0016).
		host.windowDestroyed = true
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
		if nativeFrameProfileHandlesNCCalcSize(activeNativeFrameProfile(), wParam) {
			host.applyNativeNCCalcClientRect(hwnd, lParam)
			return 0
		}
	case wmNCHitTest:
		hit := host.nativeHitTest(hwnd, lParam)
		captionHit := host.nativeCaptionButtonHit(hwnd, lParam)
		decision := host.nativeDWMCaptionHitTestDecision(hwnd, message, wParam, lParam, hit, captionHit)
		host.traceNativeTooltipHitDecision(hwnd, message, lParam, hit, captionHit, decision)
		return decision.result
	case wmSetCursor, wmNCMouseMove, wmNCMouseHover, wmNCMouseLeave:
		decision := host.nativeDWMCaptionMessageDecision(hwnd, message, wParam, lParam)
		host.traceNativeTooltipMessageDecision(hwnd, message, wParam, lParam, decision)
		if decision.override {
			return decision.result
		}
		return defWindowProc(hwnd, message, wParam, lParam)
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
		host.syncWebViewBounds("wm_size")
		host.syncTabTitlebarSystemMenuState("wm_size")
	case wmWindowPosChanging:
		host.syncWebViewBounds("wm_windowpos_changing")
	case wmWindowPosChanged:
		host.syncWebViewBounds("wm_windowpos_changed")
	case wmMove:
		host.syncWebViewBounds("wm_move")
	case wmMoving:
		host.syncWebViewBounds("wm_moving")
	case wmEnterSizeMove:
		host.syncWebViewBounds("wm_entersizemove")
	case wmExitSizeMove:
		host.syncWebViewBounds("wm_exitsizemove")
		host.requestDeferredBoundsSync(boundsSyncWParamDeferredExitSizeMove)
	}
	return defWindowProc(hwnd, message, wParam, lParam)
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

// windowDestroyTeardown is the WM_DESTROY teardown, extracted so its contract is
// headless-testable. Both timers die with the window: the render watchdog, and
// the startup show gate - left pending, the gate would fire once after
// Config.ShowTimeout and post wmNativeShow to the dead HWND, up to two warn
// lines with nothing to act on (issue #54's companion observation). The WebView
// is then shut down while the HWND is still alive.
func (host *Host) windowDestroyTeardown() {
	host.stopRenderWatchdog()
	host.stopStartupShowGate()
	if host.browser != nil {
		// Tear the control down while the HWND is still alive. Closing the
		// controller after its parent window is gone orphans the runtime's
		// own child windows, and the teardown then reports failures nobody
		// can act on.
		host.log.Debug("mullion: webview2 shutdown requested")
		host.browser.ShuttingDown()
	}
}
