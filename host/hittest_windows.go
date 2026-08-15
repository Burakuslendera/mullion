//go:build windows

package host

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

func scaleLogicalPixels(px int32, dpi uint32) int64 {
	if px <= 0 {
		return 0
	}
	if dpi == 0 {
		dpi = defaultWindowDPI
	}
	scaled := int64(px) * int64(dpi)
	result := scaled / int64(defaultWindowDPI)
	if scaled%int64(defaultWindowDPI) != 0 {
		result++
	}
	return result
}

// hitTestGeometry is the single screen-space representation used by native
// frame hit-testing. Win32 supplies int32 coordinates, but a valid RECT can span
// the full signed range, so all measurements and interval endpoints are int64.
type hitTestGeometry struct {
	left, top, right, bottom                 int64
	cursorX, cursorY                         int64
	resizeLeftEnd, resizeRightStart          int64
	resizeTopEnd, resizeBottomStart          int64
	titlebarBottom, controlsLeft             int64
	minimizeButtonEnd, maximizeButtonEnd     int64
	resizeWidth, resizeHeight, controlsWidth int64
}

func newHitTestGeometry(metrics hitTestMetrics, windowRect rect, cursor point, dpi uint32) (hitTestGeometry, bool) {
	left := int64(windowRect.Left)
	top := int64(windowRect.Top)
	right := int64(windowRect.Right)
	bottom := int64(windowRect.Bottom)
	cursorX := int64(cursor.X)
	cursorY := int64(cursor.Y)
	if left >= right || top >= bottom ||
		cursorX < left || cursorX >= right || cursorY < top || cursorY >= bottom {
		return hitTestGeometry{}, false
	}

	width := right - left
	height := bottom - top
	resize := scaleLogicalPixels(metrics.ResizeBorder, dpi)
	resizeWidth := min(resize, width/2)
	resizeHeight := min(resize, height/2)
	titlebarHeight := min(scaleLogicalPixels(metrics.TitlebarHeight, dpi), height)
	controlsWidth := min(scaleLogicalPixels(metrics.ControlsWidth, dpi), width)
	buttonWidth := controlsWidth / 3
	controlsLeft := right - controlsWidth
	minimizeButtonEnd := controlsLeft + buttonWidth

	return hitTestGeometry{
		left:              left,
		top:               top,
		right:             right,
		bottom:            bottom,
		cursorX:           cursorX,
		cursorY:           cursorY,
		resizeLeftEnd:     left + resizeWidth,
		resizeRightStart:  right - resizeWidth,
		resizeTopEnd:      top + resizeHeight,
		resizeBottomStart: bottom - resizeHeight,
		titlebarBottom:    top + titlebarHeight,
		controlsLeft:      controlsLeft,
		minimizeButtonEnd: minimizeButtonEnd,
		maximizeButtonEnd: minimizeButtonEnd + buttonWidth,
		resizeWidth:       resizeWidth,
		resizeHeight:      resizeHeight,
		controlsWidth:     controlsWidth,
	}, true
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

	hit := nativeHitTestForRect(host.config.hitTestMetrics(), windowRect, cursor, dpi, zoomed)
	if diagnostic := formatNativeHitTestDiagnostic(nativeHitTestDiagnosticEnabled(), zoomed, cursor, windowRect, dpi, hit); diagnostic != "" {
		// Logger.Debug receives a preformatted string, and even NopLogger pays
		// eager formatting; keep fmt.Sprintf inside this latched diagnostics gate.
		host.log.Debug(diagnostic)
	}

	return uintptr(hit)
}

func formatNativeHitTestDiagnostic(enabled, zoomed bool, cursor point, windowRect rect, dpi uint32, hit int32) string {
	if !enabled {
		return ""
	}
	return fmt.Sprintf("mullion: hittest zoomed=%v cursor=(%d,%d) rect=(%d,%d,%d,%d) dpi=%d hit=%s",
		zoomed, cursor.X, cursor.Y, windowRect.Left, windowRect.Top, windowRect.Right, windowRect.Bottom, dpi, nativeHitTestName(hit))
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

// windowRectForMaximizedHitTest clamps a maximized window rect to the visible work
// area so the hit-test bands anchor to what is on screen, not to frame overhang.
//
// It reads monitorInfoForWindow - in-process - and must never route through
// maximizeMonitorInfo, whose SHAppBarMessage probe is synchronous IPC to Explorer:
// WM_NCHITTEST fires continuously while the pointer is over the caption band, and a
// busy shell would stall hit-testing, drag and the caption buttons with it (issue
// #36, decision 0019). The auto-hide reveal sliver is not lost by this: the window
// rect was already inset when the window was sized (WM_GETMINMAXINFO), and
// clampRectToArea is min/max, so clamping that rect to the un-inset work area
// returns the inset rect unchanged.
func windowRectForMaximizedHitTest(hwnd windowHandle, windowRect rect) rect {
	info, ok := monitorInfoForWindow(hwnd)
	if !ok {
		return windowRect
	}
	next, ok := clampRectToArea(windowRect, info.Work)
	if !ok {
		return windowRect
	}
	return next
}

func nativeHitTestForRect(metrics hitTestMetrics, windowRect rect, cursor point, dpi uint32, maximized bool) int32 {
	geometry, ok := newHitTestGeometry(metrics, windowRect, cursor, dpi)
	if !ok {
		return htClient
	}
	if !maximized {
		if hit := hitTestResizeBorder(geometry); hit != htClient {
			return hit
		}
	}
	inTitlebar := geometry.cursorY < geometry.titlebarBottom
	inControls := geometry.cursorX >= geometry.controlsLeft
	profile := activeNativeFrameProfile()
	if inTitlebar && inControls && nativeFrameProfileUsesCaptionButtonHitTest(profile) {
		return hitTestCaptionButtons(geometry)
	}
	if inTitlebar && inControls &&
		(nativeFrameProfileUsesMaximizeCaptionButtonHitTest(profile) ||
			(maximized && nativeFrameProfileUsesZoomedMaximizeCaptionButtonHitTest(profile))) {
		if hit := hitTestCaptionButtons(geometry); hit == htMaxButton {
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
	geometry, ok := newHitTestGeometry(metrics, windowRect, cursor, dpi)
	if !ok {
		return htClient
	}
	if !maximized {
		if hit := hitTestResizeBorder(geometry); hit != htClient {
			return htClient
		}
	}
	if geometry.cursorY >= geometry.titlebarBottom || geometry.cursorX < geometry.controlsLeft {
		return htClient
	}
	return hitTestCaptionButtons(geometry)
}

func hitTestCaptionButtons(geometry hitTestGeometry) int32 {
	if geometry.controlsWidth < 3 {
		return htClient
	}
	switch {
	case geometry.cursorX >= geometry.controlsLeft && geometry.cursorX < geometry.minimizeButtonEnd:
		return htMinButton
	case geometry.cursorX >= geometry.minimizeButtonEnd && geometry.cursorX < geometry.maximizeButtonEnd:
		return htMaxButton
	case geometry.cursorX >= geometry.maximizeButtonEnd && geometry.cursorX < geometry.right:
		return htClose
	default:
		return htClient
	}
}

func hitTestResizeBorder(geometry hitTestGeometry) int32 {
	onLeft := geometry.cursorX < geometry.resizeLeftEnd
	onRight := geometry.cursorX >= geometry.resizeRightStart
	onTop := geometry.cursorY < geometry.resizeTopEnd
	onBottom := geometry.cursorY >= geometry.resizeBottomStart
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
	geometry, ok := newHitTestGeometry(
		hitTestMetrics{},
		windowRect,
		point{X: windowRect.Left, Y: windowRect.Top},
		defaultWindowDPI,
	)
	if !ok {
		return point{}, false
	}

	centerX := geometry.left + (geometry.right-geometry.left)/2
	centerY := geometry.top + (geometry.bottom-geometry.top)/2
	left := geometry.left
	right := geometry.right - 1
	top := geometry.top
	bottom := geometry.bottom - 1
	switch hit {
	case htLeft:
		return point{X: int32(left), Y: int32(centerY)}, true
	case htRight:
		return point{X: int32(right), Y: int32(centerY)}, true
	case htTop:
		return point{X: int32(centerX), Y: int32(top)}, true
	case htBottom:
		return point{X: int32(centerX), Y: int32(bottom)}, true
	case htTopLeft:
		return point{X: int32(left), Y: int32(top)}, true
	case htTopRight:
		return point{X: int32(right), Y: int32(top)}, true
	case htBottomLeft:
		return point{X: int32(left), Y: int32(bottom)}, true
	case htBottomRight:
		return point{X: int32(right), Y: int32(bottom)}, true
	default:
		return point{}, false
	}
}
