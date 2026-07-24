//go:build windows

package host

const nativeRestoredFrameCompensationPX int32 = 1

func (host *Host) applyNativeNCCalcClientRect(hwnd windowHandle, lParam uintptr) bool {
	target, ok := readRect(lParam)
	if !ok {
		return false
	}
	if !isZoomed(hwnd) {
		next := applyRestoredClientFrameCompensation(target)
		writeRect(lParam, &next)
		return true
	}
	info, ok := maximizeMonitorInfo(hwnd)
	if !ok {
		return false
	}
	next, ok := clampRectToArea(target, info.Work)
	if !ok {
		return false
	}
	writeRect(lParam, &next)
	return true
}

func applyRestoredClientFrameCompensation(target rect) rect {
	target.Bottom += nativeRestoredFrameCompensationPX
	return target
}

func clampRectToArea(target, area rect) (rect, bool) {
	next := rect{
		Left:   max(target.Left, area.Left),
		Top:    max(target.Top, area.Top),
		Right:  min(target.Right, area.Right),
		Bottom: min(target.Bottom, area.Bottom),
	}
	if next.Right <= next.Left || next.Bottom <= next.Top {
		return rect{}, false
	}
	return next, true
}
