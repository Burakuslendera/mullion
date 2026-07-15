//go:build windows

package host

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowFromPoint(cursor point) windowHandle {
	result, _, _ := procWindowFromPoint.Call(pointToStructArg(cursor))
	return windowHandle(result)
}

func isChildWindow(parent, child windowHandle) bool {
	if parent == 0 || child == 0 {
		return false
	}
	result, _, _ := procIsChild.Call(uintptr(parent), uintptr(child))
	return result != 0
}

func classNameForWindow(hwnd windowHandle) string {
	if hwnd == 0 {
		return "unavailable"
	}
	var buffer [256]uint16
	result, _, _ := procGetClassName.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if result == 0 {
		return "unavailable"
	}
	return windows.UTF16ToString(buffer[:int(result)])
}

func pointToStructArg(value point) uintptr {
	return uintptr(uint64(uint32(value.X)) | (uint64(uint32(value.Y)) << 32))
}
