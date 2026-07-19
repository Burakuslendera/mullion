//go:build windows

package host

// Initial window placement: where and how large the first frame is, decided at
// creation time instead of left to the shell (issue #59, docs/decisions/0018).
//
// Before this existed, CreateWindowEx received CW_USEDEFAULT and the raw
// Config.Width/Height. That produced the shell's cascade position - the
// top-left quadrant of the primary monitor, drifting a step per launch - and a
// window whose physical size equalled the logical config values, i.e. a window
// that shrank by the scale factor on every monitor above 100%. The config has
// always documented Width and Height as logical pixels; this file is what makes
// that true at creation.

import (
	"strconv"
	"unsafe"
)

// initialPlacement is the physical rect handed to CreateWindowEx, plus the DPI
// it was computed for so the startup log can say why the numbers are what they
// are.
type initialPlacement struct {
	X, Y          int32
	Width, Height int32
	DPI           uint32
}

// initialWindowPlacement resolves the primary monitor and returns the centered,
// DPI-scaled creation rect. ok is false when the monitor cannot be resolved at
// all; the caller then falls back to the pre-#59 CW_USEDEFAULT behaviour, which
// degrades the position, never the window.
//
// The primary monitor - not the one under the cursor - is the deliberate
// default: it is deterministic across launches, and it is where Windows itself
// puts a first window. Decision 0018 records the alternative and what would
// change it.
func (host *Host) initialWindowPlacement() (initialPlacement, bool) {
	info, monitor, ok := primaryMonitorInfo()
	if !ok {
		return initialPlacement{}, false
	}
	return centeredPlacement(info.Work, monitorDPI(monitor), host.config.Width, host.config.Height)
}

// primaryMonitorInfo resolves the primary monitor. The point 0,0 is the primary
// monitor's origin by definition; MONITOR_DEFAULTTOPRIMARY is belt and braces
// for the same answer.
func primaryMonitorInfo() (monitorInfo, uintptr, bool) {
	monitor, _, _ := procMonitorFromPoint.Call(pointToStructArg(point{}), monitorDefaultToPrimary)
	if monitor == 0 {
		return monitorInfo{}, 0, false
	}
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	result, _, _ := procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return monitorInfo{}, 0, false
	}
	return info, monitor, true
}

// monitorDPI reads a monitor's effective DPI before any window exists on it -
// GetDpiForWindow needs an HWND, and placement runs to decide where that HWND
// goes. GetDpiForMonitor lives in shcore.dll; if it cannot be resolved or
// fails, the default DPI keeps the window centered at its unscaled size - the
// pre-#59 size - rather than failing the placement outright.
//
// The awareness contract matters here: New enables Per-Monitor-V2 before Run
// creates any window, and GetDpiForMonitor answers per that awareness, so the
// value is the monitor's real effective DPI, not a virtualised one.
func monitorDPI(monitor uintptr) uint32 {
	if err := procGetDpiForMonitor.Find(); err != nil {
		return defaultWindowDPI
	}
	var dpiX, dpiY uint32
	result, _, _ := procGetDpiForMonitor.Call(
		monitor,
		mdtEffectiveDPI,
		uintptr(unsafe.Pointer(&dpiX)),
		uintptr(unsafe.Pointer(&dpiY)),
	)
	if result != 0 || dpiX == 0 { // S_OK is zero
		return defaultWindowDPI
	}
	return dpiX
}

// centeredPlacement scales a logical size to physical pixels for dpi and
// centers it in work. Pure so the placement contract is testable headlessly
// (decision 0006), like dpiChangedTargetSize.
//
// The rect is centered in the monitor's *work area*, not the monitor rect:
// centering over the full monitor puts the bottom edge under the taskbar. A
// size larger than the work area is clamped to it - the window lands flush
// with the work-area origin instead of hanging off-screen. Scaling happens
// before centering, because the centered origin depends on the physical
// extent.
func centeredPlacement(work rect, dpi uint32, logicalWidth, logicalHeight int32) (initialPlacement, bool) {
	if dpi == 0 {
		dpi = defaultWindowDPI
	}
	workWidth := work.Right - work.Left
	workHeight := work.Bottom - work.Top
	if workWidth <= 0 || workHeight <= 0 || logicalWidth <= 0 || logicalHeight <= 0 {
		return initialPlacement{}, false
	}
	width := dpiRescaleLength(logicalWidth, defaultWindowDPI, dpi)
	height := dpiRescaleLength(logicalHeight, defaultWindowDPI, dpi)
	if width > workWidth {
		width = workWidth
	}
	if height > workHeight {
		height = workHeight
	}
	return initialPlacement{
		X:      work.Left + (workWidth-width)/2,
		Y:      work.Top + (workHeight-height)/2,
		Width:  width,
		Height: height,
		DPI:    dpi,
	}, true
}

// formatInitialPlacementLog is the startup log line for a resolved placement.
// Metrics only - coordinates, extent and DPI - so the log stays safe to hand
// over.
func formatInitialPlacementLog(place initialPlacement) string {
	return "mullion: initial placement, x=" + formatInt32(place.X) +
		", y=" + formatInt32(place.Y) +
		", width=" + formatInt32(place.Width) +
		", height=" + formatInt32(place.Height) +
		", dpi=" + strconv.FormatUint(uint64(place.DPI), 10)
}
