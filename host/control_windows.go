//go:build windows

package host

import (
	"errors"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// Show makes the active Run's window and WebView2 controller visible, creating
// the deferred controller when StartHidden is set. It returns an error when no
// Run is active or the window cannot become visible.
func (host *Host) Show() error {
	admission := host.enterRun()
	defer host.leaveRun(admission)
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

// Hide hides the active Run's window and WebView2 controller. When no Run is
// active, it has no window effect.
func (host *Host) Hide() {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	host.log.Debug("mullion: hide requested")
	host.warnIf("hide post", host.postRunCommand(admission, wmNativeHide, 0))
}

// Quit requests destruction of the active Run's window so Run can return. When
// no Run is active, it has no window effect.
func (host *Host) Quit() {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	host.log.Debug("mullion: quit requested")
	host.warnIf("quit post", host.postRunCommand(admission, wmNativeQuit, 0))
}

// Minimise minimises the active Run's window. When no Run is active, it has no
// window effect.
func (host *Host) Minimise() {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	host.log.Debug("mullion: minimize requested")
	host.warnIf("minimize post", host.postRunCommand(admission, wmNativeMinimize, 0))
}

// ToggleMaximise toggles the active Run's window between maximised and restored.
// When no Run is active, it has no window effect.
func (host *Host) ToggleMaximise() {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	host.log.Debug("mullion: maximize toggle requested")
	host.warnIf("maximize toggle post", host.postRunCommand(admission, wmNativeMaxToggle, 0))
}

// StartDrag begins the system caption-drag operation for the active Run's window.
// When no Run is active, it has no window effect.
func (host *Host) StartDrag() {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	host.log.Debug("mullion: titlebar drag requested")
	host.warnIf("titlebar drag post", host.postRunCommand(admission, wmNativeStartDrag, 0))
}

// StartResize begins a system resize for left, right, top, bottom, top-left,
// top-right, bottom-left, or bottom-right. An unknown edge is rejected. When no
// Run is active, it has no window effect.
func (host *Host) StartResize(edge string) {
	admission := host.enterRun()
	defer host.leaveRun(admission)
	hit, ok := resizeHitTestForEdge(edge)
	if !ok {
		host.log.Warn("mullion: resize requested with unknown edge, edge=" + logsafe.Field(edge))
		return
	}
	host.log.Debug("mullion: resize requested, edge=" + logsafe.Field(edge))
	host.warnIf("resize post", host.postRunCommand(admission, wmNativeStartResize, uintptr(hit)))
}

// IsMaximised reports whether the active Run's window is maximised. It returns
// false when no Run is active and otherwise pins HWND ownership across a direct
// cross-thread-safe native query.
func (host *Host) IsMaximised() bool {
	admission := host.enterRun()
	defer host.leaveRun(admission)
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
	defer host.leaveRun(admission)
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

type showDisposition uint8

const (
	showVisible showDisposition = iota
	showRetryableHidden
	showCancelled
	showTerminal
)

func (host *Host) showFromMessage(intent uint64) showDisposition {
	return host.showFromMessageWithEnsure(intent, host.ensureWebView)
}

// showFromMessageWithEnsure keeps the terminal reporting boundary headless-testable.
// A create error cannot be returned through SendMessage's integer result, so this
// UI-thread handler owns its one report; Show only returns the public error.
func (host *Host) showFromMessageWithEnsure(intent uint64, ensure func(string) error) showDisposition {
	return host.showFromMessageWithEffects(intent, ensure, host.applyShowAfterEnsure)
}

// showFromMessageWithEffects keeps the post-ensure HWND/controller boundary
// explicit. A failed required-script barrier must not reach any visibility,
// foreground, bounds, or controller effect in apply.
func (host *Host) showFromMessageWithEffects(intent uint64, ensure func(string) error, apply func(uint64) showDisposition) showDisposition {
	host.log.Debug("mullion: show applying")
	if err := ensure("show"); err != nil {
		host.log.Error("mullion: show failed, reason=" + logsafe.Reason(err))
		return showCancelled
	}
	if !host.visibilityIntentMatches(intent) {
		return showCancelled
	}
	return apply(intent)
}

func (host *Host) applyShowAfterEnsure(intent uint64) showDisposition {
	admission := host.currentRun()
	hwnd := admission.hwnd
	browser := host.browser
	if hwnd == 0 || browser == nil || !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	if err := host.setControllerVisibility(true); err != nil {
		host.log.Warn("mullion: webview show failed, source=show, reason=" + logsafe.Reason(err))
		if !host.showOwnershipMatches(admission, browser, intent) {
			return showCancelled
		}
		if !host.parentVisible(hwnd) {
			return showRetryableHidden
		}
		return host.rollbackShow(admission, browser, intent)
	}
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	showErr := host.setParentVisibility(hwnd, swShow)
	host.warnIf("show apply", showErr)
	if host.showOwnershipMatches(admission, browser, intent) {
		host.warnIf("foreground apply", host.setParentForeground(hwnd))
	}
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	updateErr := host.updateParent(hwnd)
	host.warnIf("update apply", updateErr)
	if host.showOwnershipMatches(admission, browser, intent) && showErr == nil && updateErr == nil && host.parentVisible(hwnd) {
		host.syncBoundsForWindowMessage("show")
		if !host.showOwnershipMatches(admission, browser, intent) {
			return showCancelled
		}
		host.recordStartupWindowVisible()
		if !host.showOwnershipMatches(admission, browser, intent) {
			return showCancelled
		}
		host.log.Info("mullion: window visible")
		return showVisible
	}
	host.log.Warn("mullion: show unexpected state")
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	return host.rollbackShow(admission, browser, intent)
}

func (host *Host) showOwnershipMatches(admission runAdmission, browser *webview2.Browser, intent uint64) bool {
	return host.runMatches(admission) && host.browser == browser && host.window() == admission.hwnd && host.visibilityIntentMatches(intent)
}

func (host *Host) rollbackShow(admission runAdmission, browser *webview2.Browser, intent uint64) showDisposition {
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	controllerErr := host.setControllerVisibility(false)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	parentErr := host.setParentVisibility(admission.hwnd, swHide)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	visible := host.parentVisible(admission.hwnd)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return showCancelled
	}
	if controllerErr != nil || parentErr != nil || visible {
		return showTerminal
	}
	return showRetryableHidden
}

func (host *Host) setControllerVisibility(visible bool) error {
	if host.applyControllerVisibility != nil {
		return host.applyControllerVisibility(visible)
	}
	if host.browser == nil {
		return errors.New("webview unavailable")
	}
	if visible {
		return host.browser.Show()
	}
	return host.browser.Hide()
}

func (host *Host) setParentVisibility(hwnd windowHandle, command int32) error {
	if host.applyParentVisibility != nil {
		return host.applyParentVisibility(hwnd, command)
	}
	return showWindow(hwnd, command)
}
func (host *Host) parentVisible(hwnd windowHandle) bool {
	if host.queryParentVisible != nil {
		return host.queryParentVisible(hwnd)
	}
	return isWindowVisible(hwnd)
}
func (host *Host) updateParent(hwnd windowHandle) error {
	if host.applyParentUpdate != nil {
		return host.applyParentUpdate(hwnd)
	}
	return updateWindow(hwnd)
}
func (host *Host) setParentForeground(hwnd windowHandle) error {
	if host.applyParentForeground != nil {
		return host.applyParentForeground(hwnd)
	}
	return setForegroundWindow(hwnd)
}

func (host *Host) hideFromMessage(intent uint64) {
	admission := host.currentRun()
	hwnd := admission.hwnd
	browser := host.browser
	host.log.Debug("mullion: hide applying")
	if !host.showOwnershipMatches(admission, browser, intent) {
		return
	}
	if host.isWebViewDeferred() {
		host.log.Debug("mullion: webview hide skipped, reason=deferred")
	} else if host.browser == nil {
		host.log.Warn("mullion: webview unavailable during hide")
	} else if err := host.setControllerVisibility(false); err != nil {
		host.log.Warn("mullion: webview hide failed, reason=" + logsafe.Reason(err))
	}
	if !host.showOwnershipMatches(admission, browser, intent) {
		return
	}
	hideErr := host.setParentVisibility(hwnd, swHide)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return
	}
	host.warnIf("hide apply", hideErr)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return
	}
	visible := host.parentVisible(hwnd)
	if !host.showOwnershipMatches(admission, browser, intent) {
		return
	}
	if hideErr == nil && visible {
		host.log.Warn("mullion: hide unexpected state")
		if !host.showOwnershipMatches(admission, browser, intent) {
			return
		}
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

// isResizeHitTest is the receiver-side half of resizeHitTestForEdge. Private
// messages are process-visible, so the UI-thread receiver must preserve the
// sender's eight-edge contract before it can release capture or ask
// DefWindowProc to start non-client interaction.
func isResizeHitTest(hit uintptr) bool {
	switch hit {
	case htLeft, htRight, htTop, htBottom, htTopLeft, htTopRight, htBottomLeft, htBottomRight:
		return true
	default:
		return false
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
