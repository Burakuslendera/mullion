//go:build windows

package host

import "unsafe"

// procRtlMoveMemory copies raw bytes to and from memory Win32 owns by pointer:
// the MINMAXINFO passed by reference on WM_GETMINMAXINFO and the suggested RECT
// on WM_DPICHANGED. RtlMoveMemory is a kernel32 export, so it resolves from the
// shared System32-only kernel32 handle in win32_windows.go - there is no second
// DLL to load here. Using that handle keeps every load site in this package on
// windows.NewLazySystemDLL, whose search path is narrower than the
// syscall.NewLazyDLL (misleadingly named "ntdll") this used to call.
var procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")

func readMinMaxInfo(src uintptr) (minMaxInfo, bool) {
	var value minMaxInfo
	if src == 0 {
		return value, false
	}
	copyFromWindowPointer(unsafe.Pointer(&value), src, unsafe.Sizeof(value))
	return value, true
}

func writeMinMaxInfo(dst uintptr, value *minMaxInfo) {
	if dst == 0 || value == nil {
		return
	}
	copyToWindowPointer(dst, unsafe.Pointer(value), unsafe.Sizeof(*value))
}

func readRect(src uintptr) (rect, bool) {
	var value rect
	if src == 0 {
		return value, false
	}
	copyFromWindowPointer(unsafe.Pointer(&value), src, unsafe.Sizeof(value))
	return value, true
}

func writeRect(dst uintptr, value *rect) {
	if dst == 0 || value == nil {
		return
	}
	copyToWindowPointer(dst, unsafe.Pointer(value), unsafe.Sizeof(*value))
}

func copyFromWindowPointer(dst unsafe.Pointer, src uintptr, size uintptr) {
	_, _, _ = procRtlMoveMemory.Call(uintptr(dst), src, size)
}

func copyToWindowPointer(dst uintptr, src unsafe.Pointer, size uintptr) {
	_, _, _ = procRtlMoveMemory.Call(dst, uintptr(src), size)
}
