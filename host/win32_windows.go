//go:build windows

package host

import "golang.org/x/sys/windows"

var (
	user32   = windows.NewLazySystemDLL("User32.dll")
	kernel32 = windows.NewLazySystemDLL("Kernel32.dll")

	procRegisterClassEx     = user32.NewProc("RegisterClassExW")
	procUnregisterClass     = user32.NewProc("UnregisterClassW")
	procCreateWindowEx      = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procDefWindowProc       = user32.NewProc("DefWindowProcW")
	procGetWindowLongPtr    = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessage         = user32.NewProc("PostMessageW")
	procSendMessage         = user32.NewProc("SendMessageW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procWindowFromPoint     = user32.NewProc("WindowFromPoint")
	procGetClassName        = user32.NewProc("GetClassNameW")
	procIsChild             = user32.NewProc("IsChild")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	procIsZoomed            = user32.NewProc("IsZoomed")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procMonitorFromWindow   = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	procLoadCursor          = user32.NewProc("LoadCursorW")
	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")

	procSetWindowText = user32.NewProc("SetWindowTextW")
)
