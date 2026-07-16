//go:build windows

package host

import "unsafe"

// Auto-hide taskbar detection.
//
// A taskbar set to auto-hide reserves no work-area space, so GetMonitorInfo
// reports the work area as the full monitor. A maximized window sized to that
// work area therefore covers the monitor edge the taskbar hides against, and the
// shell then suppresses the taskbar's reveal-on-hover for as long as the window
// stays maximized. DefWindowProc's own maximized WM_NCCALCSIZE leaves a one-pixel
// sliver on the auto-hide edge so the reveal still fires; because mullion answers
// WM_NCCALCSIZE itself (docs/decisions/0003) it must reimplement that inset. This
// file finds which monitor edges hold an auto-hide taskbar; nccalc_windows.go
// applies the one-pixel inset (docs/decisions/0015).

// SHAppBarMessage commands and flags (shellapi.h).
const (
	abmGetState         = 0x00000004
	abmGetAutoHideBarEx = 0x0000000b
	absAutoHide         = 0x00000001

	abeLeft   = 0
	abeTop    = 1
	abeRight  = 2
	abeBottom = 3
)

// appBarData mirrors APPBARDATA. hWnd and lParam are pointer-sized, so
// unsafe.Sizeof matches the C struct on every supported architecture and can be
// used verbatim as cbSize.
type appBarData struct {
	Size            uint32
	Hwnd            windowHandle
	CallbackMessage uint32
	Edge            uint32
	Rect            rect
	LParam          uintptr
}

// autoHideEdges records which edges of a monitor hold an auto-hide taskbar.
type autoHideEdges struct {
	Left, Top, Right, Bottom bool
}

// autoHideTaskbarEdges reports which edges of the given monitor hold an auto-hide
// taskbar. It asks the shell (SHAppBarMessage), so it cannot be exercised in a
// headless test; the pure inset it feeds (insetForAutoHideEdges) is what the tests
// lock instead.
func autoHideTaskbarEdges(monitor rect) autoHideEdges {
	if !anyAutoHideTaskbar() {
		// The common case: no taskbar is auto-hide, so skip the four per-edge probes.
		return autoHideEdges{}
	}
	return autoHideEdges{
		Left:   hasAutoHideTaskbar(abeLeft, monitor),
		Top:    hasAutoHideTaskbar(abeTop, monitor),
		Right:  hasAutoHideTaskbar(abeRight, monitor),
		Bottom: hasAutoHideTaskbar(abeBottom, monitor),
	}
}

// anyAutoHideTaskbar reports whether any taskbar is in auto-hide mode. ABM_GETSTATE
// is a cheap global query; it gates the per-edge, per-monitor ABM_GETAUTOHIDEBAREX
// probes so the common case (no auto-hide taskbar) costs a single call.
func anyAutoHideTaskbar() bool {
	data := appBarData{}
	data.Size = uint32(unsafe.Sizeof(data))
	state, _, _ := procSHAppBarMessage.Call(abmGetState, uintptr(unsafe.Pointer(&data)))
	return state&absAutoHide != 0
}

// hasAutoHideTaskbar reports whether the given edge of the given monitor holds an
// auto-hide taskbar. ABM_GETAUTOHIDEBAREX takes the monitor rect in rc and returns
// the appbar's window handle (non-zero) when one is present on that edge of that
// monitor - the per-monitor form, so a secondary display is answered correctly
// rather than always against the primary.
func hasAutoHideTaskbar(edge uint32, monitor rect) bool {
	data := appBarData{Edge: edge, Rect: monitor}
	data.Size = uint32(unsafe.Sizeof(data))
	result, _, _ := procSHAppBarMessage.Call(abmGetAutoHideBarEx, uintptr(unsafe.Pointer(&data)))
	return result != 0
}
