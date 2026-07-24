//go:build windows

package webview2

// Value-level ABI for the shared types in interfaces_windows.go: struct size and
// field order for Rect and Color, the width of an EventRegistrationToken the
// runtime writes through a pointer, the enum values passed straight to Chromium,
// and the BOOL/LPWSTR conversions.
//
// None of it is a vtable slot. These are the bytes that cross the boundary
// rather than the pointers that are jumped through, which is why they are not in
// the layout table next door - that file answers one question ("is every slot
// pinned?") and stays whole so the answer stays greppable.

import (
	"testing"
	"unsafe"
)

// Rect must be layout-identical to a Win32 RECT: four 32-bit fields, 16 bytes.
// The size is load-bearing beyond the field access - it is what makes the Win64
// ABI pass the struct by pointer rather than in a register, which is what
// PutBounds relies on.
func TestRectLayout(t *testing.T) {
	var r Rect
	if got := unsafe.Sizeof(r); got != 16 {
		t.Fatalf("Rect size = %d, want 16 (Win32 RECT)", got)
	}
	for _, tc := range []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"Left", unsafe.Offsetof(r.Left), 0},
		{"Top", unsafe.Offsetof(r.Top), 4},
		{"Right", unsafe.Offsetof(r.Right), 8},
		{"Bottom", unsafe.Offsetof(r.Bottom), 12},
	} {
		if tc.off != tc.want {
			t.Errorf("Rect.%s offset = %d, want %d", tc.name, tc.off, tc.want)
		}
	}
}

// TestColorPackMatchesMemoryLayout pins both halves of the COREWEBVIEW2_COLOR
// contract at once: the A,R,G,B field order, and the packing that PutDefaultBackgroundColor
// passes by value.
//
// Reinterpreting the struct as a uint32 is exactly what the Win64 ABI does with
// a 4-byte aggregate, so pack() agreeing with the raw memory is the definition
// of correct. If someone "fixes" the struct to R,G,B,A, this fails - whereas
// the call itself would keep working and just paint the wrong colour with the
// wrong opacity.
func TestColorPackMatchesMemoryLayout(t *testing.T) {
	if got := unsafe.Sizeof(Color{}); got != 4 {
		t.Fatalf("Color size = %d, want 4 (COREWEBVIEW2_COLOR is 4 BYTEs)", got)
	}
	for _, c := range []Color{
		{A: 255, R: 0, G: 0, B: 0},
		{A: 0, R: 255, G: 0, B: 0},
		{A: 0, R: 0, G: 255, B: 0},
		{A: 0, R: 0, G: 0, B: 255},
		{A: 0x12, R: 0x34, G: 0x56, B: 0x78},
	} {
		want := uintptr(*(*uint32)(unsafe.Pointer(&c)))
		if got := c.pack(); got != want {
			t.Errorf("Color%+v: pack() = %#x, want %#x (raw struct bytes)", c, got, want)
		}
	}
	// Spell out the field order independently of pack(), so that a matching
	// bug in both would still be caught.
	opaqueRed := Color{A: 0xFF, R: 0xFF, G: 0x00, B: 0x00}
	if got, want := opaqueRed.pack(), uintptr(0x0000FFFF); got != want {
		t.Errorf("opaque red packs to %#x, want %#x (A=0xFF lowest byte, R=0xFF next)", got, want)
	}
}

// EventRegistrationToken is written by the runtime through a pointer; if it
// were not 8 bytes the add_ methods would scribble past it.
func TestEventRegistrationTokenSize(t *testing.T) {
	if got := unsafe.Sizeof(EventRegistrationToken(0)); got != 8 {
		t.Fatalf("EventRegistrationToken size = %d, want 8 (__int64)", got)
	}
}

// The enum values are ABI, not policy: they are passed straight through to the
// runtime. BoundsModeUseRawPixels being 0 is what lets this host feed WebView2
// physical pixels, and WebResourceContextAll being 0 is what makes the resource
// filter match every request rather than only documents.
func TestEnumValues(t *testing.T) {
	if BoundsModeUseRawPixels != 0 {
		t.Errorf("COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS = %d, want 0", BoundsModeUseRawPixels)
	}
	if BoundsModeUseRasterizationScale != 1 {
		t.Errorf("COREWEBVIEW2_BOUNDS_MODE_USE_RASTERIZATION_SCALE = %d, want 1", BoundsModeUseRasterizationScale)
	}
	if WebResourceContextAll != 0 {
		t.Errorf("COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL = %d, want 0", WebResourceContextAll)
	}
	if ProcessFailedKindBrowserProcessExited != 0 {
		t.Errorf("COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED = %d, want 0", ProcessFailedKindBrowserProcessExited)
	}
	if ProcessFailedKindRenderProcessExited != 1 {
		t.Errorf("COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED = %d, want 1", ProcessFailedKindRenderProcessExited)
	}
}

func TestBoolConversions(t *testing.T) {
	if got := boolToBOOL(true); got != 1 {
		t.Errorf("boolToBOOL(true) = %d, want 1", got)
	}
	if got := boolToBOOL(false); got != 0 {
		t.Errorf("boolToBOOL(false) = %d, want 0", got)
	}
	// Win32 TRUE is "non-zero", not "== 1": a runtime that reports 2 or -1 must
	// still read as true.
	for _, v := range []int32{1, 2, -1} {
		if !boolFromBOOL(v) {
			t.Errorf("boolFromBOOL(%d) = false, want true (BOOL is non-zero, not ==1)", v)
		}
	}
	if boolFromBOOL(0) {
		t.Error("boolFromBOOL(0) = true, want false")
	}
}

// takeWstr must tolerate a nil LPWSTR. The runtime is allowed to hand back S_OK
// with a null string (an empty header, say), and a nil deref here would crash
// inside an event handler.
func TestTakeWstrNil(t *testing.T) {
	if got := takeWstr(nil); got != "" {
		t.Errorf("takeWstr(nil) = %q, want empty string", got)
	}
}
