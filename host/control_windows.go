//go:build windows

package host

import (
	"errors"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func (host *Host) Show() error {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: show requested")

	// Admission is counted rather than represented by a held mutex: a deferred
	// embed may pump a bridge readiness callback, and Logger callbacks may also
	// re-enter Host. endRun still waits for this complete synchronous result.
	result, err := host.sendRunCommand(admission, wmNativeShow, 0)
	stillOriginatingRun := host.runMatches(admission)
	if stillOriginatingRun {
		host.warnIf("show send", err)
	}
	if err != nil {
		return err
	}
	if result == 0 {
		// wmNativeShow's UI-thread handler has already reported why it could not
		// become visible. The public caller owns the returned error, not a second
		// generic log entry (decision 0038).
		return errors.New("native show did not become visible")
	}
	return nil
}

func (host *Host) Hide() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: hide requested")
	host.warnIf("hide post", host.postRunCommand(admission, wmNativeHide, 0))
}

func (host *Host) Quit() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: quit requested")
	host.warnIf("quit post", host.postRunCommand(admission, wmNativeQuit, 0))
}

func (host *Host) Minimise() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: minimize requested")
	host.warnIf("minimize post", host.postRunCommand(admission, wmNativeMinimize, 0))
}

func (host *Host) ToggleMaximise() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: maximize toggle requested")
	host.warnIf("maximize toggle post", host.postRunCommand(admission, wmNativeMaxToggle, 0))
}

func (host *Host) StartDrag() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.log.Debug("mullion: titlebar drag requested")
	host.warnIf("titlebar drag post", host.postRunCommand(admission, wmNativeStartDrag, 0))
}

func (host *Host) StartResize(edge string) {
	admission := host.enterRun()
	defer host.leaveRun()
	hit, ok := resizeHitTestForEdge(edge)
	if !ok {
		host.log.Warn("mullion: resize requested with unknown edge, edge=" + logsafe.Diagnostic(edge))
		return
	}
	host.log.Debug("mullion: resize requested, edge=" + logsafe.Diagnostic(edge))
	host.warnIf("resize post", host.postRunCommand(admission, wmNativeStartResize, uintptr(hit)))
}

func (host *Host) IsMaximised() bool {
	admission := host.enterRun()
	defer host.leaveRun()
	if !admission.running || admission.hwnd == 0 {
		return false
	}
	// Keep lookup and use under the same HWND ownership read lock. WM_DESTROY
	// must clear ownership under the write lock before Windows may recycle it.
	host.mu.RLock()
	defer host.mu.RUnlock()
	if admission.token != host.activeRunToken || admission.hwnd != host.hwnd {
		return false
	}
	if host.queryNativeMaximised != nil {
		return host.queryNativeMaximised(admission.hwnd)
	}
	return isZoomed(admission.hwnd)
}

// SetTitle updates the window title. With a custom title bar the caption is not
// painted by the shell, so this is what the taskbar, Alt+Tab and the window
// switcher show. The UTF-16 payload is synchronous and the private command is
// session-tagged, so a recycled HWND can never receive an older Run's title.
func (host *Host) SetTitle(title string) {
	admission := host.enterRun()
	defer host.leaveRun()
	text, err := windows.UTF16PtrFromString(title)
	if err != nil {
		host.warnIf("set title", err)
		return
	}
	_, err = host.sendRunCommand(admission, wmNativeSetTitle, uintptr(unsafe.Pointer(text)))
	runtime.KeepAlive(text)
	host.warnIf("set title", err)
}

func (host *Host) postRunCommand(admission runAdmission, message uint32, wParam uintptr) error {
	// PostMessage itself does not dispatch, so HWND ownership can remain pinned
	// across lookup/use without re-entrancy. WM_DESTROY cannot clear/recycle the
	// handle until the tagged command has been queued.
	host.mu.RLock()
	defer host.mu.RUnlock()
	if admission.token != host.activeRunToken || admission.hwnd != host.hwnd {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	if host.postNativeCommand != nil {
		return host.postNativeCommand(admission.hwnd, message, wParam, admission.token)
	}
	return postWindowMessageArgs(admission.hwnd, message, wParam, admission.token)
}

func (host *Host) sendRunCommand(admission runAdmission, message uint32, wParam uintptr) (uintptr, error) {
	// SendMessage may re-enter the embed pump, so it cannot hold mu across the
	// call. Reject known-stale ownership here; the process-global lParam token is
	// the final guard if destruction/reuse wins after this snapshot.
	if !host.runMatches(admission) {
		return 0, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	if host.sendNativeCommand != nil {
		return host.sendNativeCommand(admission.hwnd, message, wParam, admission.token)
	}
	return sendWindowMessageResult(admission.hwnd, message, wParam, admission.token)
}

func (host *Host) showFromMessage() bool {
	return host.showFromMessageWithEnsure(host.ensureWebView)
}

// showFromMessageWithEnsure keeps the terminal reporting boundary headless-testable.
// A create error cannot be returned through SendMessage's integer result, so this
// UI-thread handler owns its one report; Show only returns the public error.
func (host *Host) showFromMessageWithEnsure(ensure func(string) error) bool {
	host.log.Debug("mullion: show applying")
	if err := ensure("show"); err != nil {
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
	}
	host.log.Warn("mullion: show unexpected state")
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
		host.requestDeferredBoundsSync(boundsSyncWParamDeferredRestore)
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
	host.requestDeferredBoundsSync(boundsSyncWParamDeferredMaximize)
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
