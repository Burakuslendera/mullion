//go:build windows

package backdrop

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procFindWindow                    = user32.NewProc("FindWindowW")
	procIsWindow                      = user32.NewProc("IsWindow")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procRegisterClassEx               = user32.NewProc("RegisterClassExW")
	procUnregisterClass               = user32.NewProc("UnregisterClassW")
	procCreateWindowEx                = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProc                 = user32.NewProc("DefWindowProcW")
	procGetMessage                    = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessage               = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procLoadCursor                    = user32.NewProc("LoadCursorW")
	procCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject                  = gdi32.NewProc("DeleteObject")
	procGetModuleHandle               = kernel32.NewProc("GetModuleHandleW")
)

const (
	dpiAwarenessPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmKeyDown = 0x0100
	wmTimer   = 0x0113
	vkEscape  = 0x1B

	// watchTimerID drives the target watch below: 200ms is far under what a
	// human reads as "instant" and costs three cheap USER calls a tick.
	watchTimerID     = 1
	watchTimerMillis = 200

	wsPopup       = 0x8000_0000
	wsVisible     = 0x1000_0000
	wsExAppWindow = 0x0004_0000 // a taskbar button, so the backdrop is always discoverable

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	idcArrow = 32512

	hwndTop                   = 0
	swpNoMoveNoSizeNoActivate = 0x0013 // SWP_NOSIZE | SWP_NOMOVE | SWP_NOACTIVATE
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type message struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   struct{ X, Y int32 }
}

// wndProcCallback is created once, at package init: windows.NewCallback
// allocates from a small fixed table that is never freed, so a callback per
// call would leak table slots (the same rule host/ and internal/webview2
// follow).
var wndProcCallback = windows.NewCallback(backdropWndProc)

// watchedTarget is the window the backdrop lifted at startup, or 0. The
// command runs exactly one backdrop per process, so a package variable is the
// whole state the window procedure needs.
var watchedTarget uintptr

// backdropWndProc closes on Esc and on WM_CLOSE (Alt+F4, the taskbar button's
// close) - and, when a target was lifted at startup, closes with that target:
// the watch timer fires every 200ms and the backdrop leaves as soon as the
// target is destroyed (its process ended, or the user closed it), hidden, or
// minimised. Everything else - paint included - is DefWindowProc's: the class
// background brush is the entire rendering.
func backdropWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmKeyDown:
		if wParam == vkEscape {
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmTimer:
		if wParam == watchTimerID && watchedTarget != 0 && !targetStillUp(watchedTarget) {
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmClose:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		_, _, _ = procKillTimer.Call(hwnd, watchTimerID)
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

// targetStillUp reports whether the lifted window still exists on screen,
// unminimised. Moving and resizing it changes none of these; closing it,
// ending its process, hiding it or sending it to the taskbar ends the watch.
func targetStillUp(target uintptr) bool {
	if alive, _, _ := procIsWindow.Call(target); alive == 0 {
		return false
	}
	if visible, _, _ := procIsWindowVisible.Call(target); visible == 0 {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(target); iconic != 0 {
		return false
	}
	return true
}

// Show covers the whole virtual screen - every monitor - with a flat colour
// and blocks until the user dismisses it: Esc on the window, Alt+F4, its
// taskbar button, or Ctrl+C on the terminal (which ends the process and the
// window with it). It is deliberately NOT topmost: any window the user raises
// sits above it, so the backdrop can never hold the desktop hostage.
//
// If a visible window of targetClass exists, Show slots itself directly
// underneath it and lifts it to the top of the z-order - without activating
// anything, so no foreground-steal restriction applies (the SetForegroundWindow
// trap in docs/lessons-and-dead-ends.md section 4). The window to capture is
// then already in front; everything else is behind the backdrop. The lifted
// window is watched from then on: close it, end its process, or minimise it,
// and the backdrop closes itself within a timer tick. An empty targetClass,
// or no such window, just covers the desktop until dismissed by hand.
func Show(colour Colour, targetClass string) error {
	// A Win32 window and its message loop are thread-affine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Per-monitor-v2 before the HWND exists (the rule every window in this
	// repository follows), so the virtual-screen metrics and the window
	// placement below are physical pixels on every monitor.
	_, _, _ = procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)

	instance, _, err := procGetModuleHandle.Call(0)
	if instance == 0 {
		return fmt.Errorf("GetModuleHandle: %w", err)
	}
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)

	// COLORREF is 0x00BBGGRR.
	brush, _, err := procCreateSolidBrush.Call(uintptr(colour.R) | uintptr(colour.G)<<8 | uintptr(colour.B)<<16)
	if brush == 0 {
		return fmt.Errorf("CreateSolidBrush: %w", err)
	}
	defer procDeleteObject.Call(brush)

	className, err := windows.UTF16PtrFromString("MullionBackdrop")
	if err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString("mullion backdrop")
	if err != nil {
		return err
	}

	class := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    wndProcCallback,
		Instance:   instance,
		Cursor:     cursor,
		Background: brush,
		ClassName:  className,
	}
	atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	defer procUnregisterClass.Call(uintptr(unsafe.Pointer(className)), instance)

	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	width, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	height, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if width == 0 || height == 0 {
		return fmt.Errorf("GetSystemMetrics reported a %dx%d virtual screen", width, height)
	}

	hwnd, _, err := procCreateWindowEx.Call(
		wsExAppWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible,
		x, y, width, height,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}

	if target := raiseTargetAbove(hwnd, targetClass); target != 0 {
		watchedTarget = target
		_, _, _ = procSetTimer.Call(hwnd, watchTimerID, watchTimerMillis, 0)
	}

	var msg message
	for {
		ret, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(ret) {
		case 0: // WM_QUIT
			return nil
		case -1:
			return fmt.Errorf("GetMessage: %w", err)
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// raiseTargetAbove finds the first visible top-level window of targetClass and
// arranges the sandwich a capture wants: target on top, backdrop directly
// under it, everything else behind the backdrop. Both moves are pure z-order
// changes with SWP_NOACTIVATE - focus stays where it is, which is what makes
// them reliable. It returns the lifted window, or 0. Failing to find or raise
// the target is not an error: the backdrop is still doing its job, and the
// user can Alt+Tab the window forward, exactly as the usage text says.
func raiseTargetAbove(backdrop uintptr, targetClass string) uintptr {
	if targetClass == "" {
		return 0
	}
	class, err := windows.UTF16PtrFromString(targetClass)
	if err != nil {
		return 0
	}
	target, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(class)), 0)
	if target == 0 {
		return 0
	}
	if visible, _, _ := procIsWindowVisible.Call(target); visible == 0 {
		return 0
	}
	_, _, _ = procSetWindowPos.Call(target, hwndTop, 0, 0, 0, 0, swpNoMoveNoSizeNoActivate)
	_, _, _ = procSetWindowPos.Call(backdrop, target, 0, 0, 0, 0, swpNoMoveNoSizeNoActivate)
	return target
}
