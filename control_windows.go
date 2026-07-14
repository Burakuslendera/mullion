//go:build windows

package mullion

import (
	"errors"
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func (host *Host) Show() error {
	host.log.Debug("mullion: show requested")
	result, err := sendWindowMessageResult(host.window(), wmNativeShow, 0, 0)
	host.warnIf("show send", err)
	if err != nil {
		return err
	}
	if result == 0 {
		err := errors.New("native show did not become visible")
		host.log.Warn("mullion: show failed, reason=" + logsafe.Reason(err))
		return err
	}
	return nil
}

func (host *Host) Hide() {
	host.log.Debug("mullion: hide requested")
	host.warnIf("hide post", postWindowMessage(host.window(), wmNativeHide))
}

func (host *Host) Quit() {
	host.log.Debug("mullion: quit requested")
	host.warnIf("quit post", postWindowMessage(host.window(), wmNativeQuit))
}

func (host *Host) Minimise() {
	host.log.Debug("mullion: minimize requested")
	host.warnIf("minimize post", postWindowMessage(host.window(), wmNativeMinimize))
}

func (host *Host) ToggleMaximise() {
	host.log.Debug("mullion: maximize toggle requested")
	host.warnIf("maximize toggle post", postWindowMessage(host.window(), wmNativeMaxToggle))
}

func (host *Host) StartDrag() {
	host.log.Debug("mullion: titlebar drag requested")
	host.warnIf("titlebar drag post", postWindowMessage(host.window(), wmNativeStartDrag))
}

func (host *Host) StartResize(edge string) {
	hit, ok := resizeHitTestForEdge(edge)
	if !ok {
		host.log.Warn("mullion: resize requested with unknown edge, edge=" + logsafe.Message(edge))
		return
	}
	host.log.Debug("mullion: resize requested, edge=" + logsafe.Message(edge))
	host.warnIf("resize post", postWindowMessageArgs(host.window(), wmNativeStartResize, uintptr(hit), 0))
}

func (host *Host) IsMaximised() bool {
	return isZoomed(host.window())
}

// SetTitle updates the window title. With a custom title bar the caption is not
// painted by the shell, so this is what the taskbar, Alt+Tab and the window
// switcher show.
func (host *Host) SetTitle(title string) {
	host.warnIf("set title", setWindowText(host.window(), title))
}

func (host *Host) showFromMessage() bool {
	host.log.Debug("mullion: show applying")
	if err := host.ensureWebView("show"); err != nil {
		host.log.Error("mullion: show failed, reason=" + logsafe.Reason(err))
		return false
	}
	hwnd := host.window()
	showErr := showWindow(hwnd, swShow)
	host.warnIf("show apply", showErr)
	if showErr == nil && !isWindowVisible(hwnd) && host.config.StartHidden {
		host.log.Debug("mullion: show retry requested, reason=startup_hidden")
		showErr = showWindow(hwnd, swShow)
		host.warnIf("show retry", showErr)
	}
	host.warnIf("foreground apply", setForegroundWindow(hwnd))
	updateErr := updateWindow(hwnd)
	host.warnIf("update apply", updateErr)
	chromiumVisible := host.showChromium("show")
	if chromiumVisible {
		host.syncWebViewBounds("show")
	}
	if showErr == nil && updateErr == nil && chromiumVisible && isWindowVisible(hwnd) {
		host.recordStartupWindowVisible()
		host.log.Info("mullion: window visible")
		return true
	} else {
		host.log.Warn("mullion: show unexpected state")
	}
	return false
}

func (host *Host) showChromium(source string) bool {
	if host.browser == nil {
		host.log.Warn("mullion: webview unavailable during show, source=" + logsafe.Message(source))
		return false
	}
	if err := host.browser.Show(); err != nil {
		host.log.Warn("mullion: webview show failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return false
	}
	host.log.Debug("mullion: webview visible, source=" + logsafe.Message(source))
	return true
}

func (host *Host) hideFromMessage() {
	host.log.Debug("mullion: hide applying")
	hwnd := host.window()
	if host.isWebViewDeferred() {
		host.log.Debug("mullion: webview hide skipped, reason=deferred")
	} else if host.browser == nil {
		host.log.Warn("mullion: webview unavailable during hide")
	} else if err := host.browser.Hide(); err != nil {
		host.log.Warn("mullion: webview hide failed, reason=" + logsafe.Reason(err))
	}
	hideErr := showWindow(hwnd, swHide)
	host.warnIf("hide apply", hideErr)
	if hideErr == nil && isWindowVisible(hwnd) {
		host.log.Warn("mullion: hide unexpected state")
	}
	host.logNativeWindowActionState("hide", hwnd)
}

func (host *Host) minimizeFromMessage() {
	host.log.Debug("mullion: minimize applying, method=wm_syscommand")
	hwnd := host.window()
	err := sendWindowMessage(hwnd, wmSysCommand, scMinimize, 0)
	host.warnIf("minimize send", err)
	host.logNativeWindowActionState("minimize", hwnd)
	if err == nil && !isIconic(hwnd) {
		host.log.Warn("mullion: minimize unexpected state")
	}
}

func (host *Host) toggleMaximiseFromMessage() {
	host.log.Debug("mullion: maximize toggle applying")
	hwnd := host.window()
	if host.IsMaximised() {
		host.log.Debug("mullion: restore applying, method=wm_syscommand")
		err := sendWindowMessage(hwnd, wmSysCommand, scRestore, 0)
		host.warnIf("restore send", err)
		host.syncWebViewBounds("restore")
		host.requestDeferredBoundsSync("restore")
		host.logNativeWindowActionState("restore", hwnd)
		if err == nil && isZoomed(hwnd) {
			host.log.Warn("mullion: restore unexpected state")
		}
		return
	}
	host.log.Debug("mullion: maximize applying, method=wm_syscommand")
	err := sendWindowMessage(hwnd, wmSysCommand, scMaximize, 0)
	host.warnIf("maximize send", err)
	host.syncWebViewBounds("maximize")
	host.requestDeferredBoundsSync("maximize")
	host.logNativeWindowActionState("maximize", hwnd)
	if err == nil && !isZoomed(hwnd) {
		host.log.Warn("mullion: maximize unexpected state")
	}
}

func (host *Host) startResizeFromMessage(hit int32) {
	host.log.Debug("mullion: resize applying, hit=" + formatInt32(hit))
	hwnd := host.window()
	if hwnd == 0 {
		host.log.Warn("mullion: resize skipped, reason=window unavailable")
		return
	}
	if isZoomed(hwnd) {
		host.log.Debug("mullion: resize skipped, reason=maximized")
		return
	}
	host.warnIf("resize foreground apply", setForegroundWindow(hwnd))
	host.warnIf("release capture", releaseCapture())
	cursor, source, ok := host.resizeStartPoint(hwnd, hit)
	if !ok {
		host.log.Warn("mullion: resize skipped, reason=start point unavailable")
		return
	}
	host.log.Debug("mullion: resize start point selected, source=" + source)
	host.warnIf("resize send", sendWindowMessage(hwnd, wmNCLButtonDown, uintptr(hit), pointToLParam(cursor)))
}

func resizeHitTestForEdge(edge string) (int32, bool) {
	switch edge {
	case "left":
		return htLeft, true
	case "right":
		return htRight, true
	case "top":
		return htTop, true
	case "bottom":
		return htBottom, true
	case "top-left":
		return htTopLeft, true
	case "top-right":
		return htTopRight, true
	case "bottom-left":
		return htBottomLeft, true
	case "bottom-right":
		return htBottomRight, true
	default:
		return htClient, false
	}
}

func (host *Host) logNativeWindowActionState(action string, hwnd windowHandle) {
	host.log.Debug("mullion: " + action + " state, iconic=" + strconv.FormatBool(isIconic(hwnd)) +
		", zoomed=" + strconv.FormatBool(isZoomed(hwnd)) +
		", visible=" + strconv.FormatBool(isWindowVisible(hwnd)))
}

func (host *Host) resizeStartPoint(hwnd windowHandle, hit int32) (point, string, bool) {
	cursor, err := getCursorPos()
	if err == nil {
		return cursor, "cursor", true
	}
	host.log.Warn("mullion: resize cursor unavailable, reason=" + logsafe.Reason(err))
	windowRect, ok := getWindowRect(hwnd)
	if !ok {
		host.log.Warn("mullion: resize fallback unavailable, reason=window rect unavailable")
		return point{}, "unavailable", false
	}
	fallback, ok := resizeFallbackPoint(windowRect, hit)
	if !ok {
		host.log.Warn("mullion: resize fallback unavailable, reason=unknown hit")
		return point{}, "unavailable", false
	}
	return fallback, "fallback", true
}
