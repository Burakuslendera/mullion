//go:build windows

package host

import (
	"strconv"
)

type nativeWindowStyleAudit struct {
	style              uintptr
	exStyle            uintptr
	cornerPreference   int32
	cornerAvailable    bool
	windowRect         rect
	windowRectValid    bool
	clientRect         rect
	clientRectValid    bool
	extendedFrame      rect
	extendedFrameValid bool
}

func (host *Host) logNativeWindowStyle(hwnd windowHandle, stage string) {
	host.log.Debug(formatNativeWindowStyleLog(stage, readNativeWindowStyleAudit(hwnd)))
}

func readNativeWindowStyleAudit(hwnd windowHandle) nativeWindowStyleAudit {
	audit := nativeWindowStyleAudit{cornerPreference: dwmWindowCornerPreferenceDef}
	if style, err := windowStyle(hwnd); err == nil {
		audit.style = style
	}
	if exStyle, err := windowExStyle(hwnd); err == nil {
		audit.exStyle = exStyle
	}
	if preference, err := getDWMWindowCornerPreference(hwnd); err == nil {
		audit.cornerPreference = preference
		audit.cornerAvailable = true
	}
	if windowRect, err := getWindowRectWithError(hwnd); err == nil {
		audit.windowRect = windowRect
		audit.windowRectValid = true
	}
	if clientRect, err := getClientRect(hwnd); err == nil {
		audit.clientRect = clientRect
		audit.clientRectValid = true
	}
	if extendedFrame, err := getDWMExtendedFrameBounds(hwnd); err == nil {
		audit.extendedFrame = extendedFrame
		audit.extendedFrameValid = true
	}
	return audit
}

func formatNativeWindowStyleLog(stage string, audit nativeWindowStyleAudit) string {
	style := audit.style
	return "mullion: native style audit, stage=" + stage +
		", ws_caption=" + strconv.FormatBool(style&uintptr(wsCaption) != 0) +
		", ws_sysmenu=" + strconv.FormatBool(style&uintptr(wsSysMenu) != 0) +
		", ws_thickframe=" + strconv.FormatBool(style&uintptr(wsThickFrame) != 0) +
		", ws_minimizebox=" + strconv.FormatBool(style&uintptr(wsMinimizeBox) != 0) +
		", ws_maximizebox=" + strconv.FormatBool(style&uintptr(wsMaximizeBox) != 0) +
		", ws_visible=" + strconv.FormatBool(style&uintptr(wsVisible) != 0) +
		", style=0x" + strconv.FormatUint(uint64(style), 16) +
		", exstyle=0x" + strconv.FormatUint(uint64(audit.exStyle), 16) +
		", dwm_corner_preference=" + formatDWMCornerPreference(audit) +
		", window_rect=" + formatAuditRect(audit.windowRect, audit.windowRectValid) +
		", client_rect=" + formatAuditRect(audit.clientRect, audit.clientRectValid) +
		", extended_frame=" + formatAuditRect(audit.extendedFrame, audit.extendedFrameValid)
}

func formatDWMCornerPreference(audit nativeWindowStyleAudit) string {
	if !audit.cornerAvailable {
		return "unavailable"
	}
	return strconv.FormatInt(int64(audit.cornerPreference), 10)
}

func formatAuditRect(value rect, valid bool) string {
	if !valid {
		return "unavailable"
	}
	return strconv.FormatInt(int64(value.Left), 10) + ":" +
		strconv.FormatInt(int64(value.Top), 10) + ":" +
		strconv.FormatInt(int64(value.Right), 10) + ":" +
		strconv.FormatInt(int64(value.Bottom), 10)
}
