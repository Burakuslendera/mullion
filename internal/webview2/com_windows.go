//go:build windows

// Package webview2 is a hand-written, CGo-free binding for the Microsoft
// WebView2 runtime.
//
// It exists so that mullion depends on nothing but the standard library and
// golang.org/x/sys/windows: no third-party WebView2 binding, and - crucially -
// no WebView2Loader.dll shipped beside the executable. The environment is
// created by calling CreateWebViewEnvironmentWithOptionsInternal directly out
// of the runtime's own EmbeddedBrowserWebView.dll (see loader_windows.go).
//
// This file is the outbound COM plumbing that everything else stands on - the
// call bridge, IUnknown, HRESULT handling, and the laundering rules for memory
// Windows owns. The inbound half - comServer, the shared IUnknown for COM
// objects implemented in Go - lives in comserver_windows.go. Two directions of
// traffic exist and they have different hazards:
//
//   - Outbound (we call COM): a COM object is a pointer to a pointer to a
//     vtable, so *IUnknown below mirrors that layout exactly. Calls go through
//     ComProc.Call.
//   - Inbound (COM calls us): WebView2's async APIs take completion handlers,
//     which are COM objects the *caller* must implement. comServer is the
//     shared implementation of IUnknown for those Go-side objects.
//
// Threading: WebView2 requires a single-threaded apartment and a running
// message loop. Callers must have called CoInitializeEx(COINIT_APARTMENTTHREADED)
// on the thread they use this package from, and that thread must be locked
// (runtime.LockOSThread) for the lifetime of the WebView.
package webview2

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HRESULT values. COM signals failure with the top bit, so any code with
// 0x80000000 set is an error and everything else - S_OK and S_FALSE alike - is
// a success. Do not compare against S_OK alone: a COM method that legitimately
// returns S_FALSE would be read as a failure.
const (
	sOK          uintptr = 0x00000000
	eNoInterface uintptr = 0x80004002
	ePointer     uintptr = 0x80004003
	eFail        uintptr = 0x80004005
	eOutOfMemory uintptr = 0x8007000E
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

// IIDIUnknown is the identity every COM object must answer QueryInterface for.
var IIDIUnknown = windows.GUID{
	Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
	Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
}

// ComProc is one slot of a COM vtable: the address of a method whose first
// argument is the `this` pointer.
type ComProc uintptr

// Call invokes the method. The first return value is the HRESULT; feed it to
// hres. The error return only reports a failure of the syscall machinery
// itself and is nil in practice - it is not the COM status.
//
// The //go:uintptrescapes directive is load-bearing, not decoration. Callers
// write `p.Call(uintptr(unsafe.Pointer(&out)))` to receive out-parameters. Go
// may grow (and therefore move) a goroutine stack at any call boundary, which
// would leave COM writing its result into an address that no longer belongs to
// `out`. The directive forces every pointer converted to uintptr at this call
// site onto the heap, where it cannot move, for the duration of the call. This
// is exactly why syscall.Proc.Call carries the same directive.
//
//go:uintptrescapes
func (p ComProc) Call(args ...uintptr) (uintptr, uintptr, error) {
	r1, r2, errno := syscall.SyscallN(uintptr(p), args...)
	if errno != 0 {
		// syscall.Errno(0) is a non-nil error value once boxed into the error
		// interface, so it has to be filtered out here rather than returned.
		return r1, r2, errno
	}
	return r1, r2, nil
}

// IUnknownVtbl is the head of every COM vtable. Any richer interface embeds it
// first, in declaration order, so that the method offsets line up with the ABI.
type IUnknownVtbl struct {
	QueryInterface ComProc
	AddRef         ComProc
	Release        ComProc
}

// IUnknown is a COM interface pointer: one machine word pointing at a vtable.
// It is the base for every WebView2 interface. Pointers of this type address
// memory owned by the WebView2 runtime, not the Go heap.
type IUnknown struct {
	Vtbl *IUnknownVtbl
}

// AddRef takes a reference and returns the new count.
func (u *IUnknown) AddRef() uint32 {
	if u == nil {
		return 0
	}
	r, _, _ := u.Vtbl.AddRef.Call(uintptr(unsafe.Pointer(u)))
	return uint32(r)
}

// Release drops a reference and returns the remaining count. Releasing more
// than you own frees the object under the runtime's feet, so pair every
// AddRef, QueryInterface and out-parameter with exactly one Release.
func (u *IUnknown) Release() uint32 {
	if u == nil {
		return 0
	}
	r, _, _ := u.Vtbl.Release.Call(uintptr(unsafe.Pointer(u)))
	return uint32(r)
}

// QueryInterface asks the object for another of its interfaces. The returned
// pointer carries a reference the caller owns and must Release.
//
// It is returned as unsafe.Pointer rather than *IUnknown because the caller
// knows the concrete interface and will reinterpret it; every WebView2
// interface begins with the IUnknown vtable, so the cast is layout-safe.
func (u *IUnknown) QueryInterface(iid *windows.GUID) (unsafe.Pointer, error) {
	if u == nil {
		return nil, errors.New("webview2: QueryInterface on nil interface")
	}
	if iid == nil {
		return nil, errors.New("webview2: QueryInterface with nil IID")
	}
	// `out` is declared as a typed pointer, so COM writes the interface pointer
	// straight into a Go pointer variable. Nothing here converts a uintptr into
	// an unsafe.Pointer, which keeps the code inside what `go vet` accepts.
	var out *IUnknown
	hr, _, _ := u.Vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(u)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&out)),
	)
	if err := hres(hr); err != nil {
		return nil, fmt.Errorf("QueryInterface(%s): %w", iid.String(), err)
	}
	if out == nil {
		return nil, fmt.Errorf("QueryInterface(%s): succeeded but returned nil", iid.String())
	}
	return unsafe.Pointer(out), nil
}

// HResultError is a failed COM status code.
type HResultError uint32

// HResult returns the raw status code, so callers can branch on a specific
// failure (E_NOINTERFACE from an older runtime, say) instead of on text.
func (e HResultError) HResult() uint32 { return uint32(e) }

func (e HResultError) Error() string {
	// Most WebView2 failures are FACILITY_WIN32 HRESULTs, which the system
	// message table can render. When it cannot, syscall.Errno falls back to a
	// "winapi error #N" placeholder that adds nothing to the hex code.
	if text := strings.TrimSpace(syscall.Errno(uint32(e)).Error()); text != "" &&
		!strings.HasPrefix(text, "winapi error") {
		return fmt.Sprintf("hresult 0x%08X: %s", uint32(e), text)
	}
	return fmt.Sprintf("hresult 0x%08X", uint32(e))
}

// hres converts an HRESULT into an error, treating every non-negative code
// (S_OK, S_FALSE, ...) as success.
func hres(hr uintptr) error {
	if uint32(hr)&0x80000000 == 0 {
		return nil
	}
	return HResultError(uint32(hr))
}

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
func writeAddress(dst uintptr, value uintptr) bool {
	if dst == 0 {
		return false
	}
	stored := value
	_, _, _ = procRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&stored)), unsafe.Sizeof(stored))
	return true
}

// writeBOOL stores a Win32 BOOL (4 bytes, not Go's 1-byte bool) into an
// out-parameter supplied by COM.
func writeBOOL(dst uintptr, value bool) bool {
	if dst == 0 {
		return false
	}
	var stored int32
	if value {
		stored = 1
	}
	_, _, _ = procRtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&stored)), unsafe.Sizeof(stored))
	return true
}

// unknownFromAddress reinterprets an interface pointer that COM passed to us as
// an integer (a completion handler's `result` argument, for example) as an
// *IUnknown.
//
// The bit pattern is copied into a typed pointer variable rather than cast,
// for the reason given on procRtlMoveMemory. The address points into the
// runtime's memory, never into the Go heap, so the GC will scan the resulting
// word, find it outside every heap span, and ignore it - which is precisely
// what we want.
func unknownFromAddress(addr uintptr) *IUnknown {
	if addr == 0 {
		return nil
	}
	var out *IUnknown
	source := addr
	_, _, _ = procRtlMoveMemory.Call(
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&source)),
		unsafe.Sizeof(source),
	)
	return out
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
