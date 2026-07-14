//go:build windows

package mullion

import (
	"os"
	"strconv"
	"sync/atomic"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

var nativeTooltipTraceSeq uint64

func (host *Host) traceNativeTooltipHitDecision(hwnd windowHandle, message uint32, lParam uintptr, projectHit, candidateHit uintptr, decision nativeCaptionDecision) {
	if !nativeTooltipTraceReady() {
		return
	}
	cursor := pointFromLParam(lParam)
	context := formatNativeTooltipTraceContext(hwnd) +
		formatNativeTooltipWindowFromPointContext(hwnd, cursor, int32(candidateHit))
	if shouldTrackNativeTooltipMouse(int32(projectHit), int32(decision.dwmResult), decision.dwmHandled) || int32(candidateHit) == htMaxButton {
		host.warnIf("tooltip trace trackmouse", host.trackNativeTooltipMouse(hwnd))
	}
	host.log.Debug("mullion: tooltip trace, seq=" + nextNativeTooltipTraceSeq() +
		", message=" + formatNativeTooltipMessage(message) +
		", return_route=" + formatNativeCaptionRoute(decision.route) +
		", project_hit=" + formatNativeTooltipHit(int32(projectHit)) +
		", candidate_hit=" + formatNativeTooltipHit(int32(candidateHit)) +
		", return_hit=" + formatNativeTooltipHit(int32(decision.result)) +
		", dwm_handled=" + strconv.FormatBool(decision.dwmHandled) +
		", dwm_hit=" + formatNativeTooltipHit(int32(decision.dwmResult)) +
		", use_dwm=" + strconv.FormatBool(decision.useDWM) +
		context +
		", cursor_x=" + formatInt32(cursor.X) +
		", cursor_y=" + formatInt32(cursor.Y))
}

func (host *Host) traceNativeTooltipMessageDecision(hwnd windowHandle, message uint32, wParam, lParam uintptr, decision nativeCaptionDecision) {
	if !nativeTooltipTraceReady() {
		return
	}
	hit := int32(wParam)
	if message == wmSetCursor {
		hit = int32(lParam & 0xffff)
		mouseMessage := uint32((lParam >> 16) & 0xffff)
		host.log.Debug("mullion: tooltip trace, seq=" + nextNativeTooltipTraceSeq() +
			", message=" + formatNativeTooltipMessage(message) +
			", return_route=" + formatNativeCaptionRoute(decision.route) +
			", hit=" + formatNativeTooltipHit(hit) +
			", mouse_message=" + formatNativeTooltipMessage(mouseMessage) +
			", return_result=" + strconv.FormatUint(uint64(decision.result), 10) +
			", dwm_handled=" + strconv.FormatBool(decision.dwmHandled) +
			", dwm_result=" + strconv.FormatUint(uint64(decision.dwmResult), 10) +
			", use_dwm=" + strconv.FormatBool(decision.useDWM) +
			formatNativeTooltipTraceContext(hwnd))
		return
	}
	if shouldRetrackNativeTooltipMessage(message) {
		host.warnIf("tooltip trace trackmouse", host.trackNativeTooltipMouse(hwnd))
	}
	cursor := pointFromLParam(lParam)
	host.log.Debug("mullion: tooltip trace, seq=" + nextNativeTooltipTraceSeq() +
		", message=" + formatNativeTooltipMessage(message) +
		", return_route=" + formatNativeCaptionRoute(decision.route) +
		", hit=" + formatNativeTooltipHit(hit) +
		", return_result=" + strconv.FormatUint(uint64(decision.result), 10) +
		", dwm_handled=" + strconv.FormatBool(decision.dwmHandled) +
		", dwm_result=" + strconv.FormatUint(uint64(decision.dwmResult), 10) +
		", use_dwm=" + strconv.FormatBool(decision.useDWM) +
		formatNativeTooltipTraceContext(hwnd) +
		", cursor_x=" + formatInt32(cursor.X) +
		", cursor_y=" + formatInt32(cursor.Y))
}

func nativeTooltipTraceReady() bool {
	return os.Getenv("MULLION_TOOLTIP_TRACE") == "1"
}

func nextNativeTooltipTraceSeq() string {
	return strconv.FormatUint(atomic.AddUint64(&nativeTooltipTraceSeq, 1), 10)
}

func shouldTrackNativeTooltipMouse(projectHit, dwmHit int32, dwmHandled bool) bool {
	if projectHit != htClient {
		return true
	}
	if !dwmHandled {
		return false
	}
	switch dwmHit {
	case htCaption, htMinButton, htMaxButton, htClose:
		return true
	default:
		return false
	}
}

func shouldRetrackNativeTooltipMessage(message uint32) bool {
	return message == wmNCMouseHover
}

func formatNativeTooltipTraceContext(hwnd windowHandle) string {
	style, err := windowStyle(hwnd)
	styleBits := "style=unavailable"
	if err == nil {
		styleBits = formatNativeTooltipStyleBits(style)
	}
	return ", profile=" + string(activeNativeFrameProfile()) +
		", initial_style=" + nativeInitialWindowStyleName() +
		", " + styleBits
}

func formatNativeTooltipWindowFromPointContext(hwnd windowHandle, cursor point, candidateHit int32) string {
	if candidateHit != htMaxButton {
		return ""
	}
	target := windowFromPoint(cursor)
	return formatNativeTooltipWindowFromPointClassContext(
		classNameForWindow(target),
		target == hwnd,
		isChildWindow(hwnd, target),
	)
}

func formatNativeTooltipWindowFromPointClassContext(className string, isParent bool, isChild bool) string {
	return ", hover_hwnd_class=" + logsafe.Message(className) +
		", hover_hwnd_is_parent=" + strconv.FormatBool(isParent) +
		", hover_hwnd_is_child=" + strconv.FormatBool(isChild)
}

func formatNativeTooltipStyleBits(style uintptr) string {
	return "style=0x" + strconv.FormatUint(uint64(style), 16) +
		", ws_caption=" + strconv.FormatBool(style&uintptr(wsCaption) != 0) +
		", ws_sysmenu=" + strconv.FormatBool(style&uintptr(wsSysMenu) != 0) +
		", ws_thickframe=" + strconv.FormatBool(style&uintptr(wsThickFrame) != 0) +
		", ws_minimizebox=" + strconv.FormatBool(style&uintptr(wsMinimizeBox) != 0) +
		", ws_maximizebox=" + strconv.FormatBool(style&uintptr(wsMaximizeBox) != 0)
}

func formatNativeTooltipMessage(message uint32) string {
	switch message {
	case wmNCHitTest:
		return "WM_NCHITTEST"
	case wmNCPaint:
		return "WM_NCPAINT"
	case wmNCActivate:
		return "WM_NCACTIVATE"
	case wmSetCursor:
		return "WM_SETCURSOR"
	case wmNCMouseMove:
		return "WM_NCMOUSEMOVE"
	case wmNCMouseHover:
		return "WM_NCMOUSEHOVER"
	case wmNCMouseLeave:
		return "WM_NCMOUSELEAVE"
	case wmNCLButtonDown:
		return "WM_NCLBUTTONDOWN"
	case wmMove:
		return "WM_MOVE"
	default:
		return "0x" + strconv.FormatUint(uint64(message), 16)
	}
}

func formatNativeTooltipHit(hit int32) string {
	switch hit {
	case htClient:
		return "HTCLIENT"
	case htCaption:
		return "HTCAPTION"
	case htMinButton:
		return "HTMINBUTTON"
	case htMaxButton:
		return "HTMAXBUTTON"
	case htClose:
		return "HTCLOSE"
	default:
		return logsafe.Message(formatInt32(hit))
	}
}
