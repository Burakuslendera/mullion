//go:build windows

package webview2

// Reading and writing memory that Windows owns: the RtlMoveMemory laundering
// rules and the CoTaskMemAlloc allocator. Split from com_windows.go, which
// keeps the outbound call bridge.
//
// Every helper here serves a COM method this package *implements* - the
// QueryInterface in comserver_windows.go, the environment-options getters in
// loader_options_windows.go, the completion callbacks that receive a result.
// host/memory_windows.go applies the same technique to WM_NCCALCSIZE payloads.

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	// RtlMoveMemory is how we read and write memory that Windows owns.
	//
	// The obvious alternative - casting the uintptr Windows hands us straight
	// to an unsafe.Pointer - is rejected by `go vet`'s unsafeptr check, and the
	// check is right to be suspicious: such a uintptr is not tracked by the GC.
	// Copying through RtlMoveMemory keeps every Go-side value a real Go pointer
	// and every Windows-side value a plain integer address, so the two never
	// masquerade as each other. memory_windows.go in the root package uses the
	// same technique for WM_NCCALCSIZE payloads.
	procRtlMoveMemory  = kernel32.NewProc("RtlMoveMemory")
	procCoTaskMemAlloc = ole32.NewProc("CoTaskMemAlloc")
)

// --- Reading and writing memory that Windows owns -------------------------
//
// Everything below launders values between Go memory and a bare address. The
// rule enforced here: a uintptr that came from Windows is never converted to a
// Go pointer, and a Go pointer is never handed out as a bare address except as
// an argument to a syscall (where //go:uintptrescapes keeps it pinned).

// readGUID copies a GUID out of memory owned by the caller of a COM method,
// e.g. the riid argument of QueryInterface.
func readGUID(src uintptr) (windows.GUID, bool) {
	var value windows.GUID
	if src == 0 {
		return value, false
	}
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&value)), src, unsafe.Sizeof(value))
	return value, true
}

// writeAddress stores a machine word (an interface pointer, a string pointer, a
// nil) into an out-parameter supplied by COM.
func writeAddress(dst uintptr, value uintptr) {
	if dst == 0 {
		return
	}
	stored := value
	_, _, _ = procRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&stored)), unsafe.Sizeof(stored))
}

// writeBOOL stores a Win32 BOOL (4 bytes, not Go's 1-byte bool) into an
// out-parameter supplied by COM.
func writeBOOL(dst uintptr, value bool) {
	if dst == 0 {
		return
	}
	var stored int32
	if value {
		stored = 1
	}
	_, _, _ = procRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&stored)), unsafe.Sizeof(stored))
}

// coTaskMemString copies s into memory allocated with CoTaskMemAlloc and
// returns its address, because COM string out-parameters must be freeable by
// the caller with CoTaskMemFree. An empty string yields a null pointer, which
// is what the WebView2 SDK's own options object returns for an unset property.
func coTaskMemString(s string) (uintptr, error) {
	if s == "" {
		return 0, nil
	}
	encoded, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	size := uintptr(len(encoded)) * unsafe.Sizeof(encoded[0])
	mem, _, _ := procCoTaskMemAlloc.Call(size)
	if mem == 0 {
		return 0, errors.New("webview2: CoTaskMemAlloc failed")
	}
	_, _, _ = procRtlMoveMemory.Call(mem, uintptr(unsafe.Pointer(&encoded[0])), size)
	return mem, nil
}
