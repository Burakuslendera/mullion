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
		host.syncWebViewBounds("deferred_window_state")
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
		host.log.Debug("mullion: destroy requested")
		host.stopRenderWatchdog()
		if host.browser != nil {
			// Tear the control down while the HWND is still alive. Closing the
			// controller after its parent window is gone orphans the runtime's
			// own child windows, and the teardown then reports failures nobody
			// can act on.
			host.log.Debug("mullion: webview2 shutdown requested")
			host.browser.ShuttingDown()
		}
		procPostQuitMessage.Call(0)
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
		host.requestDeferredBoundsSync("wm_exitsizemove")
	}
	return defWindowProc(hwnd, message, wParam, lParam)
}
