//go:build windows

package host

import "golang.org/x/sys/windows"

type windowHandle = windows.Handle

const (
	cwUseDefault = 0x80000000

	htClient      = 1
	htCaption     = 2
	htMinButton   = 8
	htMaxButton   = 9
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
	htClose       = 20

	monitorDefaultToNearest = 0x00000002
	monitorDefaultToPrimary = 0x00000001

	// MDT_EFFECTIVE_DPI for GetDpiForMonitor: the DPI the monitor is actually
	// scaled to, which is the one layout must use.
	mdtEffectiveDPI = 0

	// SHAppBarMessage (shell32) commands and state, used to keep an auto-hide
	// taskbar reachable while a frameless window is maximized (docs/decisions/0015).
	// ABM_GETAUTOHIDEBAREX is the monitor-aware query: it takes the monitor rect and
	// an edge and reports whether an auto-hide appbar sits on that edge of it.
	abmGetState         = 0x00000004
	abmGetAutoHideBarEx = 0x0000000b
	absAutoHide         = 0x00000001
	abeLeft             = 0
	abeTop              = 1
	abeRight            = 2
	abeBottom           = 3

	swHide = 0
	swShow = 5

	swpNoZOrder        = 0x0004
	swpNoMove          = 0x0002
	swpNoSize          = 0x0001
	swpNoActivate      = 0x0010
	swpFrameChanged    = 0x0020
	gwlStyle           = -16
	gwlExStyle         = -20
	wsOverlapped       = 0x00000000
	wsVisible          = 0x10000000
	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsThickFrame       = 0x00040000
	wsMinimizeBox      = 0x00020000
	wsMaximizeBox      = 0x00010000
	wsOverlappedWindow = wsOverlapped | wsCaption | wsSysMenu |
		wsThickFrame | wsMinimizeBox | wsMaximizeBox
	wsNativeWindow = wsOverlapped | wsThickFrame | wsMinimizeBox | wsMaximizeBox

	wmDestroy           = 0x0002
	wmMove              = 0x0003
	wmSize              = 0x0005
	wmQuit              = 0x0012
	wmEraseBkgnd        = 0x0014
	wmClose             = 0x0010
	wmSetCursor         = 0x0020
	wmGetMinMaxInfo     = 0x0024
	wmWindowPosChanging = 0x0046
	wmWindowPosChanged  = 0x0047
	wmNCCalcSize        = 0x0083
	wmNCHitTest         = 0x0084
	wmNCPaint           = 0x0085
	wmNCActivate        = 0x0086
	wmNCMouseMove       = 0x00A0
	wmNCLButtonDown     = 0x00A1
	wmSysCommand        = 0x0112
	wmInitMenu          = 0x0116
	wmMoving            = 0x0216
	wmEnterSizeMove     = 0x0231
	wmExitSizeMove      = 0x0232
	wmNCMouseHover      = 0x02A0
	wmNCMouseLeave      = 0x02A2
	wmDPIChanged        = 0x02E0
	wmApp               = 0x8000
	wmNativeShow        = wmApp + 21
	wmNativeHide        = wmApp + 22
	wmNativeQuit        = wmApp + 23
	wmNativeMinimize    = wmApp + 24
	wmNativeMaxToggle   = wmApp + 25
	wmNativeStartDrag   = wmApp + 26
	wmNativeStartResize = wmApp + 27
	wmNativeSyncBounds  = wmApp + 28

	scMinimize = 0xF020
	scMaximize = 0xF030
	scRestore  = 0xF120

	pmRemove = 0x0001

	dwmwaExtendedFrameBounds     = 9
	dwmwaWindowCornerPreference  = 33
	dwmWindowCornerPreferenceDef = 0
	dwmWindowCornerPreferenceRnd = 2

	defaultWindowDPI = 96
)

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type point struct {
	X int32
	Y int32
}

type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// appBarData mirrors the Win32 APPBARDATA passed to SHAppBarMessage. Size is set
// from unsafe.Sizeof so the trailing pad after Hwnd (uintptr is 8-aligned) is
// counted exactly as the C struct's, on both 32- and 64-bit. Only Edge and Rect
// are set for the ABM_GETAUTOHIDEBAREX query; the rest are zero.
type appBarData struct {
	Size            uint32
	Hwnd            windowHandle
	CallbackMessage uint32
	Edge            uint32
	Rect            rect
	LParam          uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windowHandle
	Icon       windowHandle
	Cursor     windowHandle
	Background windowHandle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windowHandle
}

type msg struct {
	Window  windowHandle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
}
