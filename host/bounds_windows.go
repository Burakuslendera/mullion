//go:build windows

package host

import (
	"strconv"
	"time"

	"github.com/Burakuslendera/mullion/internal/webview2"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

type boundsSyncLogState struct {
	clientWidth      int32
	clientHeight     int32
	controllerWidth  int32
	controllerHeight int32
}

func (host *Host) syncWebViewBounds(source string) {
	hwnd := host.window()
	if host.browser == nil {
		if isHotBoundsSyncSource(source) {
			return
		}
		if host.isWebViewDeferred() {
			host.log.Debug("mullion: webview bounds sync deferred, source=" + logsafe.Message(source))
			return
		}
		host.log.Warn("mullion: webview bounds sync skipped, source=" + logsafe.Message(source) + ", reason=webview unavailable")
		return
	}
	controller := host.browser.Controller()
	if controller == nil {
		host.log.Warn("mullion: webview bounds sync skipped, source=" + logsafe.Message(source) + ", reason=controller unavailable")
		return
	}
	client, err := getClientRect(hwnd)
	if err != nil {
		host.log.Warn("mullion: webview bounds sync failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return
	}
	clientWidth := client.Right - client.Left
	clientHeight := client.Bottom - client.Top
	if clientWidth <= 0 || clientHeight <= 0 {
		host.log.Warn("mullion: webview bounds sync failed, source=" + logsafe.Message(source) + ", reason=invalid client rect, client_width=" + formatInt32(clientWidth) + ", client_height=" + formatInt32(clientHeight))
		return
	}

	target := webViewBoundsTarget(hwnd, clientWidth, clientHeight)
	if err := controller.PutBounds(target); err != nil {
		host.log.Warn("mullion: webview bounds apply failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return
	}
	if shouldNotifyBoundsSource(source) {
		if err := host.browser.NotifyParentWindowPositionChanged(); err != nil {
			host.log.Warn("mullion: webview position notify failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		}
	}

	controllerWidth, controllerHeight := readControllerBounds(controller)
	mismatch := webViewBoundsMismatch(clientWidth, clientHeight, controllerWidth, controllerHeight)
	if host.shouldLogBoundsSync(source, clientWidth, clientHeight, controllerWidth, controllerHeight, mismatch) {
		host.log.Debug(formatWebViewBoundsSyncLog(source, clientWidth, clientHeight, dpiForWindow(hwnd), controllerWidth, controllerHeight))
	}
	if mismatch {
		host.log.Warn(formatWebViewBoundsMismatchLog(source, clientWidth, clientHeight, controllerWidth, controllerHeight))
	}
}

func readControllerBounds(controller *webview2.ICoreWebView2Controller) (int32, int32) {
	bounds, err := controller.GetBounds()
	if err != nil {
		return 0, 0
	}
	return bounds.Right - bounds.Left, bounds.Bottom - bounds.Top
}

// webViewBoundsTarget returns the WebView rect for a given client size: the whole
// client area. The frame is client-extended, so there is no native caption left to
// leave room for; any inset here would appear as a dead strip along the top of the
// window that swallows the clicks meant for the HTML title bar.
func webViewBoundsTarget(_ windowHandle, clientWidth, clientHeight int32) webview2.Rect {
	return webview2.Rect{Left: 0, Top: 0, Right: clientWidth, Bottom: clientHeight}
}

func (host *Host) shouldLogBoundsSync(source string, clientWidth, clientHeight, controllerWidth, controllerHeight int32, mismatch bool) bool {
	if !isHotBoundsSyncSource(source) || mismatch {
		host.recordBoundsSyncLog(clientWidth, clientHeight, controllerWidth, controllerHeight)
		return true
	}
	host.boundsMu.Lock()
	defer host.boundsMu.Unlock()
	if host.lastBoundsSyncLog.clientWidth == clientWidth &&
		host.lastBoundsSyncLog.clientHeight == clientHeight &&
		host.lastBoundsSyncLog.controllerWidth == controllerWidth &&
		host.lastBoundsSyncLog.controllerHeight == controllerHeight {
		return false
	}
	host.lastBoundsSyncLog = boundsSyncLogState{clientWidth: clientWidth, clientHeight: clientHeight, controllerWidth: controllerWidth, controllerHeight: controllerHeight}
	return true
}

func (host *Host) recordBoundsSyncLog(clientWidth, clientHeight, controllerWidth, controllerHeight int32) {
	host.boundsMu.Lock()
	defer host.boundsMu.Unlock()
	host.lastBoundsSyncLog = boundsSyncLogState{clientWidth: clientWidth, clientHeight: clientHeight, controllerWidth: controllerWidth, controllerHeight: controllerHeight}
}

func isHotBoundsSyncSource(source string) bool {
	switch source {
	case "wm_size", "wm_move", "wm_moving", "wm_windowpos_changing", "wm_windowpos_changed":
		return true
	default:
		return false
	}
}

func shouldNotifyBoundsSource(source string) bool {
	switch source {
	case "show", "wm_size", "wm_move", "wm_moving", "wm_dpi_changed",
		"wm_windowpos_changing", "wm_windowpos_changed", "wm_entersizemove",
		"wm_exitsizemove", "frontend_shell_ready", "frontend_ready",
		"navigation_completed", "maximize", "restore", "deferred_window_state":
		return true
	default:
		return false
	}
}

func (host *Host) requestDeferredBoundsSync(source string) {
	time.AfterFunc(16*time.Millisecond, func() {
		host.warnIf("deferred bounds sync post", postWindowMessage(host.window(), wmNativeSyncBounds))
	})
}

func webViewBoundsMismatch(clientWidth, clientHeight, controllerWidth, controllerHeight int32) bool {
	if clientWidth < 300 || clientHeight < 200 {
		return false
	}
	if controllerWidth < 200 || controllerHeight < 150 {
		return true
	}
	return int64(controllerWidth)*100 < int64(clientWidth)*75 ||
		int64(controllerHeight)*100 < int64(clientHeight)*75
}

func formatWebViewBoundsSyncLog(source string, clientWidth, clientHeight int32, dpi uint32, controllerWidth, controllerHeight int32) string {
	return "mullion: webview bounds sync, source=" + logsafe.Message(source) +
		", client_width=" + formatInt32(clientWidth) +
		", client_height=" + formatInt32(clientHeight) +
		", dpi=" + strconv.FormatUint(uint64(dpi), 10) +
		", controller_width=" + formatInt32(controllerWidth) +
		", controller_height=" + formatInt32(controllerHeight)
}

func formatWebViewBoundsMismatchLog(source string, clientWidth, clientHeight, controllerWidth, controllerHeight int32) string {
	prefix := "mullion: webview surface tiny/bounds mismatch"
	if source == "frontend_ready" {
		prefix = "mullion: frontend ready but surface tiny/bounds mismatch"
	}
	return prefix +
		", source=" + logsafe.Message(source) +
		", client_width=" + formatInt32(clientWidth) +
		", client_height=" + formatInt32(clientHeight) +
		", controller_width=" + formatInt32(controllerWidth) +
		", controller_height=" + formatInt32(controllerHeight)
}

func formatInt32(value int32) string {
	return strconv.FormatInt(int64(value), 10)
}
