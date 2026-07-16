//go:build windows

package host

const nativeRestoredFrameCompensationPX int32 = 1

// autoHideTaskbarInsetPX is the sliver a maximized custom-frame window must leave
// on an auto-hide taskbar's edge so the shell still reveals the taskbar on hover.
// One pixel is what DefWindowProc reserves (and what Chromium/Electron reserve for
// the same reason); it is invisible in practice. See docs/decisions/0015.
const autoHideTaskbarInsetPX int32 = 1

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
	// Reserve a one-pixel sliver on any monitor edge that holds an auto-hide
	// taskbar. Without it the maximized client covers the edge the taskbar hides
	// against and the shell stops revealing it on hover (docs/decisions/0015).
	next = insetForAutoHideEdges(next, autoHideTaskbarEdges(info.Monitor))
	writeRect(lParam, &next)
	return true
}

// insetForAutoHideEdges shrinks a maximized client rect by one pixel on each edge
// that holds an auto-hide taskbar, leaving the sliver the shell needs to reveal it
// on hover. It never shrinks a rect past emptiness; a maximized rect is always far
// larger than a one-pixel inset, so the guard only matters for a degenerate input.
func insetForAutoHideEdges(r rect, edges autoHideEdges) rect {
	if edges.Left && r.Right-r.Left > autoHideTaskbarInsetPX {
		r.Left += autoHideTaskbarInsetPX
	}
	if edges.Right && r.Right-r.Left > autoHideTaskbarInsetPX {
		r.Right -= autoHideTaskbarInsetPX
	}
	if edges.Top && r.Bottom-r.Top > autoHideTaskbarInsetPX {
		r.Top += autoHideTaskbarInsetPX
	}
	if edges.Bottom && r.Bottom-r.Top > autoHideTaskbarInsetPX {
		r.Bottom -= autoHideTaskbarInsetPX
	}
	return r
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
