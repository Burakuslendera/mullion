//go:build windows

package mullion

import (
	"fmt"
	"unsafe"
)

func pointFromLParam(lParam uintptr) point {
	return point{
		X: int32(int16(lParam & 0xffff)),
		Y: int32(int16((lParam >> 16) & 0xffff)),
	}
}

func pointToLParam(value point) uintptr {
	x := uint32(uint16(value.X))
	y := uint32(uint16(value.Y))
	return uintptr(x | (y << 16))
}

func getWindowRect(hwnd windowHandle) (rect, bool) {
	var value rect
	result, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&value)))
	return value, result != 0
}

func isZoomed(hwnd windowHandle) bool {
	result, _, _ := procIsZoomed.Call(uintptr(hwnd))
	return result != 0
}

func isIconic(hwnd windowHandle) bool {
	result, _, _ := procIsIconic.Call(uintptr(hwnd))
	return result != 0
}

func isWindowVisible(hwnd windowHandle) bool {
	result, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return result != 0
}

func scaleLogicalPixels(px int32, dpi uint32) int32 {
	if dpi == 0 {
		dpi = defaultWindowDPI
	}
	return int32((int64(px)*int64(dpi) + defaultWindowDPI - 1) / defaultWindowDPI)
}

func dpiForWindow(hwnd windowHandle) uint32 {
	dpi, _, _ := procGetDpiForWindow.Call(uintptr(hwnd))
	if dpi == 0 {
		return defaultWindowDPI
	}
	return uint32(dpi)
}

func (host *Host) nativeHitTest(hwnd windowHandle, lParam uintptr) uintptr {
	windowRect, ok := getWindowRect(hwnd)
	if !ok {
		return htClient
	}
	zoomed := isZoomed(hwnd)
	dpi := dpiForWindow(hwnd)
	cursor := pointFromLParam(lParam)

	if zoomed {
		windowRect = windowRectForMaximizedHitTest(hwnd, windowRect)
	}

	host.log.Debug(fmt.Sprintf("mullion: hittest zoomed=%v cursor=(%d,%d) rect=(%d,%d,%d,%d) dpi=%d",
		zoomed, cursor.X, cursor.Y, windowRect.Left, windowRect.Top, windowRect.Right, windowRect.Bottom, dpi))

	return uintptr(nativeHitTestForRect(host.config.hitTestMetrics(), windowRect, cursor, dpi, zoomed))
}

func (host *Host) nativeCaptionButtonHit(hwnd windowHandle, lParam uintptr) uintptr {
	windowRect, ok := getWindowRect(hwnd)
	if !ok {
		return htClient
	}
	zoomed := isZoomed(hwnd)
	dpi := dpiForWindow(hwnd)
	cursor := pointFromLParam(lParam)
	if zoomed {
		windowRect = windowRectForMaximizedHitTest(hwnd, windowRect)
	}
	return uintptr(nativeCaptionButtonHitForRect(host.config.hitTestMetrics(), windowRect, cursor, dpi, zoomed))
}

func windowRectForMaximizedHitTest(hwnd windowHandle, windowRect rect) rect {
	info, ok := monitorInfoForWindow(hwnd)
	if !ok {
		return windowRect
	}
	next, ok := maximizedHitTestRectForWorkArea(windowRect, info.Work)
	if !ok {
		return windowRect
	}
	return next
}

func maximizedHitTestRectForWorkArea(windowRect, workArea rect) (rect, bool) {
	return clampRectToArea(windowRect, workArea)
}

func nativeHitTestForRect(metrics hitTestMetrics, windowRect rect, cursor point, dpi uint32, maximized bool) int32 {
	if !maximized {
		border := scaleLogicalPixels(metrics.ResizeBorder, dpi)
		if hit := hitTestResizeBorder(windowRect, cursor, border); hit != htClient {
			return hit
		}
	}
	titlebarHeight := scaleLogicalPixels(metrics.TitlebarHeight, dpi)
	controlsWidth := scaleLogicalPixels(metrics.ControlsWidth, dpi)
	inTitlebar := cursor.Y >= windowRect.Top && cursor.Y < windowRect.Top+titlebarHeight
	inControls := cursor.X >= windowRect.Right-controlsWidth && cursor.X < windowRect.Right
	if inTitlebar && inControls && nativeFrameProfileUsesCaptionButtonHitTest(activeNativeFrameProfile()) {
		return hitTestCaptionButtons(windowRect, cursor, controlsWidth)
	}
	profile := activeNativeFrameProfile()
	if inTitlebar && inControls &&
		(nativeFrameProfileUsesMaximizeCaptionButtonHitTest(profile) ||
			(maximized && nativeFrameProfileUsesZoomedMaximizeCaptionButtonHitTest(profile))) {
		if hit := hitTestCaptionButtons(windowRect, cursor, controlsWidth); hit == htMaxButton {
			return htMaxButton
		}
		return htClient
	}
	if inTitlebar && !inControls {
		return htCaption
	}
	return htClient
}

func nativeCaptionButtonHitForRect(metrics hitTestMetrics, windowRect rect, cursor point, dpi uint32, maximized bool) int32 {
	if !maximized {
		border := scaleLogicalPixels(metrics.ResizeBorder, dpi)
		if hit := hitTestResizeBorder(windowRect, cursor, border); hit != htClient {
			return htClient
		}
	}
	titlebarHeight := scaleLogicalPixels(metrics.TitlebarHeight, dpi)
	controlsWidth := scaleLogicalPixels(metrics.ControlsWidth, dpi)
	inTitlebar := cursor.Y >= windowRect.Top && cursor.Y < windowRect.Top+titlebarHeight
	inControls := cursor.X >= windowRect.Right-controlsWidth && cursor.X < windowRect.Right
	if !inTitlebar || !inControls {
		return htClient
	}
	return hitTestCaptionButtons(windowRect, cursor, controlsWidth)
}

func hitTestCaptionButtons(windowRect rect, cursor point, controlsWidth int32) int32 {
	if controlsWidth <= 0 {
		return htClient
	}
	buttonWidth := controlsWidth / 3
	if buttonWidth <= 0 {
		return htClient
	}
	left := windowRect.Right - controlsWidth
	switch {
	case cursor.X >= left && cursor.X < left+buttonWidth:
		return htMinButton
	case cursor.X >= left+buttonWidth && cursor.X < left+2*buttonWidth:
		return htMaxButton
	case cursor.X >= left+2*buttonWidth && cursor.X < windowRect.Right:
		return htClose
	default:
		return htClient
	}
}

func hitTestResizeBorder(windowRect rect, cursor point, border int32) int32 {
	if border <= 0 {
		return htClient
	}
	withinX := cursor.X >= windowRect.Left && cursor.X < windowRect.Right
	withinY := cursor.Y >= windowRect.Top && cursor.Y < windowRect.Bottom
	onLeft := withinY && cursor.X >= windowRect.Left && cursor.X < windowRect.Left+border
	onRight := withinY && cursor.X < windowRect.Right && cursor.X >= windowRect.Right-border
	onTop := withinX && cursor.Y >= windowRect.Top && cursor.Y < windowRect.Top+border
	onBottom := withinX && cursor.Y < windowRect.Bottom && cursor.Y >= windowRect.Bottom-border
	switch {
	case onTop && onLeft:
		return htTopLeft
	case onTop && onRight:
		return htTopRight
	case onBottom && onLeft:
		return htBottomLeft
	case onBottom && onRight:
		return htBottomRight
	case onLeft:
		return htLeft
	case onRight:
		return htRight
	case onTop:
		return htTop
	case onBottom:
		return htBottom
	default:
		return htClient
	}
}

func resizeFallbackPoint(windowRect rect, hit int32) (point, bool) {
	centerX := windowRect.Left + (windowRect.Right-windowRect.Left)/2
	centerY := windowRect.Top + (windowRect.Bottom-windowRect.Top)/2
	left := windowRect.Left
	right := windowRect.Right - 1
	top := windowRect.Top
	bottom := windowRect.Bottom - 1
	switch hit {
	case htLeft:
		return point{X: left, Y: centerY}, true
	case htRight:
		return point{X: right, Y: centerY}, true
	case htTop:
		return point{X: centerX, Y: top}, true
	case htBottom:
		return point{X: centerX, Y: bottom}, true
	case htTopLeft:
		return point{X: left, Y: top}, true
	case htTopRight:
		return point{X: right, Y: top}, true
	case htBottomLeft:
		return point{X: left, Y: bottom}, true
	case htBottomRight:
		return point{X: right, Y: bottom}, true
	default:
		return point{}, false
	}
}
