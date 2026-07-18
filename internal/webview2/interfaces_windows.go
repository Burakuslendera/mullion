//go:build windows

// Package webview2 contains hand-written COM bindings for the WebView2 Win32 API.
//
// This file carries the ABI contract and the shared value types and helpers;
// the interface declarations themselves live in the interfaces_* family, one
// file per interface group:
//
//	interfaces_environment_windows.go  IStream, ICoreWebView2Environment
//	interfaces_controller_windows.go   the ICoreWebView2Controller chain
//	interfaces_core_windows.go         ICoreWebView2 itself
//	interfaces_settings_windows.go     the ICoreWebView2Settings chain
//	interfaces_webresource_windows.go  request, requested-event args, response
//	interfaces_events_windows.go       message/navigation/process-failed args
//
// Every layout in this family is derived from Microsoft's official WebView2 SDK
// (Microsoft.Web.WebView2, build/native/include/WebView2.h and WebView2.idl),
// which is the MIDL-generated C ABI. Nothing is copied from a third-party Go
// binding.
//
// # ABI contract (read before touching any vtable struct in this family)
//
// COM dispatches by vtable *offset*, not by name. Adding, removing, reordering
// or misspelling-into-the-wrong-slot a single ComProc field silently retargets
// every method after it. The failure mode is not an error return: it is a call
// through a wrong function pointer, i.e. memory corruption or a hard crash
// inside the runtime, at the point of first use.
//
// Two rules follow, and both are load-bearing:
//
//  1. Vtables list EVERY method of the interface, in IDL order, including the
//     ones this package never calls. Unused slots are still declared, because
//     the slots after them depend on their presence. Do not "clean up" a field
//     just because nothing references it.
//  2. Each interface embeds its base interface's vtable, mirroring the C++
//     inheritance chain (ICoreWebView2Settings9 : ...8 : ...7 : ... : IUnknown).
//     Go lays embedded structs out inline and in order, so the embedding chain
//     reproduces exactly the flattened vtable MIDL emits.
//
// interfaces_windows_test.go pins every slot offset and every IID with
// unsafe.Offsetof, and runs without a WebView2 runtime. Re-run it after any
// edit here; a passing build proves nothing about a vtable.
//
// # Win64 argument-passing rules that matter here
//
// From the x64 calling convention (learn.microsoft.com/cpp/build/x64-calling-convention):
// "Structs and unions of size 8, 16, 32, or 64 bits ... are passed as if they
// were integers of the same size. Structs or unions of other sizes are passed
// as a pointer to memory allocated by the caller."
//
//   - COREWEBVIEW2_COLOR is 4 bytes (32 bits) -> passed BY VALUE, packed into a
//     register. See Color.pack.
//   - RECT is 16 bytes (128 bits) -> passed BY POINTER, even though the C
//     signature says `put_Bounds(RECT bounds)`. See ICoreWebView2Controller.PutBounds.
//   - `double` lands in XMM1 for the second argument. Go's syscall bridge copies
//     the first four integer-register arguments into X0-X3 precisely so that
//     floating-point arguments work (see Go's
//     internal/runtime/syscall/windows/asm_windows_amd64.s: "Floating point
//     arguments are passed in the XMM registers. Set them here in case any of
//     the arguments are floating point values."). So passing
//     math.Float64bits(v) as a uintptr reaches the callee correctly. See
//     ICoreWebView2Controller3.PutRasterizationScale.
package webview2

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

// Rect is the Win32 RECT. Layout is ABI-critical: four 32-bit fields, 16 bytes.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// Color is COREWEBVIEW2_COLOR.
//
// Field order is A,R,G,B - NOT R,G,B,A. This is a real trap: the struct is
// declared `{ BYTE A; BYTE R; BYTE G; BYTE B; }` in WebView2.idl, so a binding
// that reorders the fields compiles fine and silently renders the wrong colour
// (and, worse, the wrong alpha - swapping A and B turns an opaque background
// transparent).
type Color struct {
	A byte
	R byte
	G byte
	B byte
}

// pack flattens Color into the single register the Win64 ABI passes it in.
//
// The struct is 32 bits, so it travels by value "as if it were an integer of
// the same size". Little-endian: A is the lowest-addressed byte and therefore
// the least significant. TestColorPackMatchesMemoryLayout pins this against an
// unsafe reinterpretation of the struct, so the two can never drift.
func (c Color) pack() uintptr {
	return uintptr(uint32(c.A) | uint32(c.R)<<8 | uint32(c.G)<<16 | uint32(c.B)<<24)
}

// EventRegistrationToken is the token an add_* method writes back. The C type is
// a struct wrapping a single __int64; an 8-byte scalar is layout-identical and
// is always passed by pointer, so nothing is lost by flattening it.
type EventRegistrationToken int64

// BoundsMode is COREWEBVIEW2_BOUNDS_MODE.
type BoundsMode int32

const (
	// BoundsModeUseRawPixels makes Bounds mean physical (device) pixels.
	//
	// This is the mode this host wants: it computes bounds from the window's
	// client rect, which is already in physical pixels, and manages DPI itself.
	// The alternative would have WebView2 rescale bounds by RasterizationScale
	// behind our back, which double-applies DPI.
	BoundsModeUseRawPixels BoundsMode = 0
	// BoundsModeUseRasterizationScale makes Bounds be interpreted in logical
	// pixels, scaled by RasterizationScale.
	BoundsModeUseRasterizationScale BoundsMode = 1
)

// WebResourceContext is COREWEBVIEW2_WEB_RESOURCE_CONTEXT.
type WebResourceContext int32

// Only the values this package uses are named. The full enum runs to
// COREWEBVIEW2_WEB_RESOURCE_CONTEXT_OTHER = 16.
const (
	// WebResourceContextAll matches every request kind. Verified = 0 in
	// WebView2.h; it is the first enumerator, not a bitmask of the others, so
	// it must not be OR-ed with anything.
	WebResourceContextAll      WebResourceContext = 0
	WebResourceContextDocument WebResourceContext = 1
)

// ProcessFailedKind is COREWEBVIEW2_PROCESS_FAILED_KIND.
type ProcessFailedKind int32

const (
	ProcessFailedKindBrowserProcessExited      ProcessFailedKind = 0
	ProcessFailedKindRenderProcessExited       ProcessFailedKind = 1
	ProcessFailedKindRenderProcessUnresponsive ProcessFailedKind = 2
)

// WebErrorStatus is COREWEBVIEW2_WEB_ERROR_STATUS. Only carried through, never
// interpreted here, so the enumerators are not mirrored.
type WebErrorStatus int32

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// errNilInterface guards the case where the runtime hands back S_OK with a null
// out-pointer. Dereferencing that would call through a nil vtable.
var errNilInterface = errors.New("webview2: COM call succeeded but returned a nil interface")

// wstr converts to a NUL-terminated UTF-16 string for an LPCWSTR parameter.
func wstr(s string) (*uint16, error) {
	return windows.UTF16PtrFromString(s)
}

// takeWstr converts an LPWSTR *output* to a Go string and frees it.
//
// Every WebView2 get_ method that returns LPWSTR transfers ownership of a
// CoTaskMemAlloc'd buffer to the caller. Not calling CoTaskMemFree leaks it on
// every single call - and these sit on hot paths (get_Uri fires for every web
// resource request), so the leak is unbounded, not a one-off.
func takeWstr(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// boolToBOOL widens a Go bool to a Win32 BOOL argument.
func boolToBOOL(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// boolFromBOOL narrows a Win32 BOOL out-parameter. BOOL is a 32-bit int, and
// TRUE is "non-zero", not "== 1".
func boolFromBOOL(v int32) bool { return v != 0 }
