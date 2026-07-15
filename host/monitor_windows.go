//go:build windows

package host

import (
	"strconv"
	"unsafe"
)

// applyMonitorWorkArea clamps a maximizing window to the monitor work area from
// WM_GETMINMAXINFO. A client-extended (frameless) window has no native frame for
// the shell to reason about, so the default maximized extent spills over the
// taskbar; the work-area rect is what keeps the taskbar visible.
//
// MINMAXINFO.ptMaxPosition is monitor-relative, not screen-relative, which is why
// the monitor origin is subtracted: passing the raw work-area origin would offset
// the window by the monitor's position on a secondary display.
func (host *Host) applyMonitorWorkArea(hwnd windowHandle, lParam uintptr) bool {
	target, ok := readMinMaxInfo(lParam)
	if !ok {
		return false
	}
	info, ok := monitorInfoForWindow(hwnd)
	if !ok {
		return false
	}
	target.MaxPosition.X = info.Work.Left - info.Monitor.Left
	target.MaxPosition.Y = info.Work.Top - info.Monitor.Top
	target.MaxSize.X = info.Work.Right - info.Work.Left
	target.MaxSize.Y = info.Work.Bottom - info.Work.Top
	writeMinMaxInfo(lParam, &target)
	return true
}

func monitorInfoForWindow(hwnd windowHandle) (monitorInfo, bool) {
	monitor, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if monitor == 0 {
		return monitorInfo{}, false
	}
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	result, _, _ := procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return monitorInfo{}, false
	}
	return info, true
}

// applyDPIChangedRect applies the rect Windows suggests in WM_DPICHANGED (lParam)
// verbatim, without layering a DPI factor of its own on top. Under Per-Monitor-V2
// the suggested rect is already scaled for the new DPI, so a second multiply is a
// double-scale bug: the window would grow on every monitor hop. wParam carries the
// new DPI in its low word.
//
// The transition is logged because a scaling regression is otherwise only visible
// as the user's window slowly drifting larger.
func (host *Host) applyDPIChangedRect(hwnd windowHandle, wParam, lParam uintptr) bool {
	next, ok := readRect(lParam)
	if !ok {
		return false
	}
	width, height, ok := dpiChangedTargetSize(next)
	if !ok {
		return false
	}
	host.logDPIChangedTransition(hwnd, wParam, next, width, height)
	result, _, _ := procSetWindowPos.Call(
		uintptr(hwnd),
		0,
		uintptr(next.Left),
		uintptr(next.Top),
		uintptr(width),
		uintptr(height),
		swpNoZOrder|swpNoActivate,
	)
	return result != 0
}

// dpiChangedTargetSize is the size to apply on WM_DPICHANGED: the extent of the
// rect Windows suggested, unchanged. It is split out as a pure function only so a
// test can pin that identity - if someone later "corrects" it with a `* dpi / 96`,
// the window double-scales, and this way a test fails instead of the user watching
// the window grow across monitors.
func dpiChangedTargetSize(suggested rect) (width, height int32, ok bool) {
	width = suggested.Right - suggested.Left
	height = suggested.Bottom - suggested.Top
	return width, height, width > 0 && height > 0
}

// dpiRescaleLength states the Per-Monitor-V2 model of how a length is expected to
// scale across a DPI change - the rule Windows itself follows when it computes the
// suggested rect. The window path deliberately does not use it: it trusts the OS
// rect. It exists so the tests can express the contract, in particular that a
// from->to->from round trip at clean ratios is lossless, i.e. no hysteresis.
func dpiRescaleLength(length int32, fromDPI, toDPI uint32) int32 {
	if fromDPI == 0 {
		fromDPI = defaultWindowDPI
	}
	return int32(int64(length) * int64(toDPI) / int64(fromDPI))
}

// logDPIChangedTransition records a DPI transition so a visible scaling regression
// can be diagnosed from a log alone. Metrics only - DPI values and rect extents,
// never paths or tokens - so the log stays safe to hand over.
func (host *Host) logDPIChangedTransition(hwnd windowHandle, wParam uintptr, suggested rect, width, height int32) {
	newDPI := uint32(wParam & 0xffff)
	oldDPI := dpiForWindow(hwnd)
	prev, hadPrev := getWindowRect(hwnd)
	prevWidth, prevHeight := int32(0), int32(0)
	if hadPrev {
		prevWidth = prev.Right - prev.Left
		prevHeight = prev.Bottom - prev.Top
	}
	host.log.Debug("mullion: dpi changed, old_dpi=" + strconv.FormatUint(uint64(oldDPI), 10) +
		", new_dpi=" + strconv.FormatUint(uint64(newDPI), 10) +
		", zoomed=" + strconv.FormatBool(isZoomed(hwnd)) +
		", prev_w=" + formatInt32(prevWidth) + ", prev_h=" + formatInt32(prevHeight) +
		", suggested_w=" + formatInt32(width) + ", suggested_h=" + formatInt32(height))
}
