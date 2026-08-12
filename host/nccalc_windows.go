//go:build windows

package host

import "unsafe"

const nativeRestoredFrameCompensationPX int32 = 1

type nativeNCCalcAction uint8

const (
	nativeNCCalcDelegate nativeNCCalcAction = iota
	nativeNCCalcClaim
)

func (action nativeNCCalcAction) String() string {
	switch action {
	case nativeNCCalcClaim:
		return "claim_unchanged"
	default:
		return "delegate"
	}
}

type nativeNCCalcResult struct {
	action nativeNCCalcAction
	reason string
}

func (host *Host) isNCCalcZoomed(hwnd windowHandle) bool {
	if host.nccalcIsZoomed != nil {
		return host.nccalcIsZoomed(hwnd)
	}
	return isZoomed(hwnd)
}

func (host *Host) nccalcMonitorInfo(hwnd windowHandle) (monitorInfo, bool) {
	if host.nccalcMaximizeMonitorInfo != nil {
		return host.nccalcMaximizeMonitorInfo(hwnd)
	}
	return maximizeMonitorInfo(hwnd)
}

// applyNativeNCCalcClientRect changes only the proposed client rect that the
// WM_NCCALCSIZE pointer form supplies: a direct RECT for FALSE, or rgrc[0] of
// NCCALCSIZE_PARAMS for TRUE. The latter must not rely on the current matching
// first-bytes layout; the named Win32 structure documents its ownership.
//
// A missing pointer delegates: Windows owns no usable geometry. Missing monitor
// data or a degenerate clamp instead claims the unchanged proposed rect. That is
// imperfect maximized geometry, but preserves the frameless client extension;
// delegation would restore a native caption. The result names either degradation
// so the caller can make it observable.
func (host *Host) applyNativeNCCalcClientRect(hwnd windowHandle, wParam, lParam uintptr) nativeNCCalcResult {
	targetPointer := ncCalcSizeTargetPointer(wParam, lParam)
	target, ok := readRect(targetPointer)
	if !ok {
		return nativeNCCalcResult{action: nativeNCCalcDelegate, reason: "invalid client rect"}
	}
	if !host.isNCCalcZoomed(hwnd) {
		next := applyRestoredClientFrameCompensation(target)
		writeRect(targetPointer, &next)
		return nativeNCCalcResult{action: nativeNCCalcClaim}
	}
	info, ok := host.nccalcMonitorInfo(hwnd)
	if !ok {
		return nativeNCCalcResult{action: nativeNCCalcClaim, reason: "monitor unavailable"}
	}
	next, ok := clampRectToArea(target, info.Work)
	if !ok {
		return nativeNCCalcResult{action: nativeNCCalcClaim, reason: "invalid monitor work area"}
	}
	writeRect(targetPointer, &next)
	return nativeNCCalcResult{action: nativeNCCalcClaim}
}

func ncCalcSizeTargetPointer(wParam, lParam uintptr) uintptr {
	if wParam == 0 {
		return lParam
	}
	return lParam + unsafe.Offsetof(ncCalcSizeParams{}.Rects)
}

func applyRestoredClientFrameCompensation(target rect) rect {
	// RECT coordinates are signed 32-bit. A legal bottom at MaxInt32 cannot grow
	// another pixel: wrapping it to MinInt32 turns a valid client rect inside out.
	// Saturating keeps the frameless claim without manufacturing bad geometry.
	if target.Bottom < 1<<31-1 {
		target.Bottom++
	}
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
