//go:build windows

package mullion

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
	info, ok := monitorInfoForWindow(hwnd)
	if !ok {
		return false
	}
	next, ok := nccalcClientRect(target, info.Work, true)
	if !ok {
		return false
	}
	writeRect(lParam, &next)
	return true
}

func nccalcClientRect(target, workArea rect, maximized bool) (rect, bool) {
	if !maximized {
		return applyRestoredClientFrameCompensation(target), true
	}
	return clampRectToArea(target, workArea)
}

func applyRestoredClientFrameCompensation(target rect) rect {
	target.Bottom += nativeRestoredFrameCompensationPX
	return target
}

func clampRectToArea(target, area rect) (rect, bool) {
	next := rect{
		Left:   maxInt32(target.Left, area.Left),
		Top:    maxInt32(target.Top, area.Top),
		Right:  minInt32(target.Right, area.Right),
		Bottom: minInt32(target.Bottom, area.Bottom),
	}
	if next.Right <= next.Left || next.Bottom <= next.Top {
		return rect{}, false
	}
	return next, true
}

func minInt32(left, right int32) int32 {
	if left < right {
		return left
	}
	return right
}

func maxInt32(left, right int32) int32 {
	if left > right {
		return left
	}
	return right
}
