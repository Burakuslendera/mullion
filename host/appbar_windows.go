//go:build windows

package host

import "unsafe"

// autoHideRevealInsetPX is the sliver a maximized frameless window must leave on an
// edge that holds an auto-hide appbar, so the shell keeps its reveal-on-hover alive.
// An auto-hide taskbar reserves no work area, so a window sized to the raw work area
// covers the whole monitor and the shell's fullscreen detection suppresses the
// reveal (docs/decisions/0015). One pixel is the whole cost - it is what DefWindowProc
// and Chromium leave for the same reason.
const autoHideRevealInsetPX int32 = 1

// autoHideEdges records which edges of a monitor hold an auto-hide appbar.
type autoHideEdges struct {
	left   bool
	top    bool
	right  bool
	bottom bool
}

// insetForAutoHideEdges shrinks area by autoHideRevealInsetPX on each edge that
// holds an auto-hide appbar, and leaves every other edge untouched. With no such
// edge it is the identity, so a monitor with a visible taskbar or none at all
// maximizes exactly as before - the change is inert unless an auto-hide bar is
// actually present.
//
// It is pure so the 1px geometry can be locked headlessly. It never manufactures
// an inverted rect: if area is already degenerate, the input is returned unchanged.
// The NCCALCSIZE caller rejects an invalid clamp; WM_GETMINMAXINFO sizes the outer
// rect directly from the returned work area.
func insetForAutoHideEdges(area rect, edges autoHideEdges) rect {
	next := area
	if edges.left {
		next.Left += autoHideRevealInsetPX
	}
	if edges.top {
		next.Top += autoHideRevealInsetPX
	}
	if edges.right {
		next.Right -= autoHideRevealInsetPX
	}
	if edges.bottom {
		next.Bottom -= autoHideRevealInsetPX
	}
	if next.Right <= next.Left || next.Bottom <= next.Top {
		return area
	}
	return next
}

// maximizeMonitorInfo is monitorInfoForWindow with the work area inset on every edge
// of the window's monitor that holds an auto-hide appbar. WM_GETMINMAXINFO
// (applyMonitorWorkArea) sizes the outer rect from this work area. WM_NCCALCSIZE
// (applyNativeNCCalcClientRect) independently clamps its proposed client rect to the
// same work area.
//
// The maximized hit-test does NOT read it, deliberately: autoHideEdgesForMonitor is
// synchronous shell IPC, and WM_NCHITTEST is the hottest input path (issue #36,
// decision 0019). The hit-test clamps the actual window rect - already inset when
// WM_GETMINMAXINFO sized the window - to the un-inset work area, and because
// clampRectToArea is min/max that clamp cannot undo the inset.
//
// Monitor is left untouched: applyMonitorWorkArea needs it to make MaxPosition
// monitor-relative, and only the work area drives the maximized extent.
func maximizeMonitorInfo(hwnd windowHandle) (monitorInfo, bool) {
	info, ok := monitorInfoForWindow(hwnd)
	if !ok {
		return monitorInfo{}, false
	}
	info.Work = insetForAutoHideEdges(info.Work, autoHideEdgesForMonitor(info.Monitor))
	return info, true
}

// autoHideEdgesForMonitor is the SHAppBarMessage probe behind maximizeMonitorInfo.
// It is a variable only so the headless routing test can count shell calls and prove
// the maximized hit-test path never makes one (issue #36, decision 0019); production
// code never reassigns it.
var autoHideEdgesForMonitor = queryAutoHideEdgesForMonitor

// queryAutoHideEdgesForMonitor asks the shell which edges of monitor hold an auto-hide
// appbar. ABM_GETSTATE is a cheap global check first: if no auto-hide bar exists
// anywhere, the per-edge queries are skipped and no edge is reported. The monitor
// rect is taken from the same monitorInfo the rest of the frame code uses, per the
// MonitorFromWindow warning in docs/frame-and-dpi.md section 5.
func queryAutoHideEdgesForMonitor(monitor rect) autoHideEdges {
	var probe appBarData
	probe.Size = uint32(unsafe.Sizeof(probe))
	state, _, _ := procSHAppBarMessage.Call(abmGetState, uintptr(unsafe.Pointer(&probe)))
	if state&absAutoHide == 0 {
		return autoHideEdges{}
	}
	return autoHideEdges{
		left:   autoHideBarOnEdge(monitor, abeLeft),
		top:    autoHideBarOnEdge(monitor, abeTop),
		right:  autoHideBarOnEdge(monitor, abeRight),
		bottom: autoHideBarOnEdge(monitor, abeBottom),
	}
}

// autoHideBarOnEdge reports whether an auto-hide appbar sits on edge of monitor.
// ABM_GETAUTOHIDEBAREX returns the bar's window handle, or zero when there is none.
func autoHideBarOnEdge(monitor rect, edge uint32) bool {
	data := appBarData{Edge: edge, Rect: monitor}
	data.Size = uint32(unsafe.Sizeof(data))
	bar, _, _ := procSHAppBarMessage.Call(abmGetAutoHideBarEx, uintptr(unsafe.Pointer(&data)))
	return bar != 0
}
