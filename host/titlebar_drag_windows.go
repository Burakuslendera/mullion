//go:build windows

package host

import (
	"os"
	"strconv"
)

type titlebarDragDispatcher struct {
	releaseCapture func() error
	cursor         point
	send           func(uintptr) error
}

func (host *Host) applyTitlebarDrag(dispatcher titlebarDragDispatcher) bool {
	host.warnIf("release capture", dispatcher.releaseCapture())
	lParam := pointToLParam(dispatcher.cursor)
	host.log.Debug("mullion: titlebar drag start point selected, source=cursor")
	if err := dispatcher.send(lParam); err != nil {
		host.warnIf("titlebar drag send", err)
		return false
	}
	return true
}

func (host *Host) startDragFromMessage() {
	host.log.Debug("mullion: titlebar drag applying")
	hwnd := host.window()
	if hwnd == 0 {
		host.log.Warn("mullion: titlebar drag skipped, reason=window unavailable")
		return
	}
	cursor, err := getCursorPos()
	if err != nil {
		host.log.Warn("mullion: titlebar drag skipped, reason=cursor unavailable")
		return
	}
	windowRect, ok := getWindowRect(hwnd)
	if !ok {
		host.log.Warn("mullion: titlebar drag skipped, reason=window rect unavailable")
		return
	}
	dpi := dpiForWindow(hwnd)
	maximized := isZoomed(hwnd)
	if maximized {
		windowRect = windowRectForMaximizedHitTest(hwnd, windowRect)
	}
	hit := nativeHitTestForRect(host.config.hitTestMetrics(), windowRect, cursor, dpi, maximized)
	host.logTitlebarDragHitTestDiagnostic(cursor, windowRect, dpi, maximized, hit)
	if hit != htCaption {
		host.log.Debug("mullion: titlebar drag skipped, hit=" + nativeHitTestName(hit))
		return
	}
	host.applyTitlebarDrag(titlebarDragDispatcher{
		releaseCapture: releaseCapture,
		cursor:         cursor,
		send: func(lParam uintptr) error {
			return sendWindowMessage(hwnd, wmNCLButtonDown, htCaption, lParam)
		},
	})
}

func (host *Host) logTitlebarDragHitTestDiagnostic(cursor point, windowRect rect, dpi uint32, maximized bool, hit int32) {
	if !nativeHitTestDiagnosticEnabled() {
		return
	}
	host.log.Debug("mullion: hittest diagnostic, source=titlebar_drag" +
		", cursor_y=" + formatInt32(cursor.Y) +
		", window_top=" + formatInt32(windowRect.Top) +
		", top_border=" + formatInt32(scaleLogicalPixels(host.config.ResizeBorder, dpi)) +
		", titlebar_height=" + formatInt32(scaleLogicalPixels(host.config.HitTestTitlebarHeight, dpi)) +
		", maximized=" + strconv.FormatBool(maximized) +
		", hit=" + nativeHitTestName(hit))
}

func nativeHitTestDiagnosticEnabled() bool {
	value := os.Getenv("MULLION_HITTEST_DIAG")
	return value == "1" || value == "true" || value == "TRUE"
}

func nativeHitTestName(hit int32) string {
	switch hit {
	case htClient:
		return "HTCLIENT"
	case htCaption:
		return "HTCAPTION"
	case htMinButton:
		return "HTMINBUTTON"
	case htMaxButton:
		return "HTMAXBUTTON"
	case htLeft:
		return "HTLEFT"
	case htRight:
		return "HTRIGHT"
	case htTop:
		return "HTTOP"
	case htTopLeft:
		return "HTTOPLEFT"
	case htTopRight:
		return "HTTOPRIGHT"
	case htBottom:
		return "HTBOTTOM"
	case htBottomLeft:
		return "HTBOTTOMLEFT"
	case htBottomRight:
		return "HTBOTTOMRIGHT"
	case htClose:
		return "HTCLOSE"
	default:
		return "HTUNKNOWN"
	}
}
