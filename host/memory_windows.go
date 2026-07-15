//go:build windows

package host

import (
	"syscall"
	"unsafe"
)

var (
	ntdll             = syscall.NewLazyDLL("kernel32.dll")
	procRtlMoveMemory = ntdll.NewProc("RtlMoveMemory")
)

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
