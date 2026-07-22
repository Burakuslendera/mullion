//go:build windows

package host

import (
	"runtime"
	"testing"
	"unsafe"
)

// TestRtlMoveMemoryRoundTrips locks the raw-memory helpers to the shared
// System32 kernel32 handle (win32_windows.go): after folding the old private
// "ntdll" NewLazyDLL load into it, RtlMoveMemory must still resolve and copy
// struct bytes both ways. In production the window procedure is handed a bare
// pointer into Win32-owned memory on WM_GETMINMAXINFO and WM_DPICHANGED; here
// both ends are Go-owned so the copy is observable without a window. This is a
// wiring lock, not a behaviour change - the helpers behaved identically before
// the fold; a mistyped export or the wrong DLL handle would fail Call and this
// round-trip. dst is used through its real pointer after each call (and kept
// alive) so it is heap-allocated and its address stays valid across the copy.
func TestRtlMoveMemoryRoundTrips(t *testing.T) {
	t.Run("rect", func(t *testing.T) {
		src := rect{Left: 11, Top: 22, Right: 333, Bottom: 444}
		dst := &rect{}
		writeRect(uintptr(unsafe.Pointer(dst)), &src)
		if *dst != src {
			t.Fatalf("writeRect stored %+v, want %+v", *dst, src)
		}
		got, ok := readRect(uintptr(unsafe.Pointer(dst)))
		runtime.KeepAlive(dst)
		if !ok || got != src {
			t.Fatalf("readRect = %+v ok=%v, want %+v", got, ok, src)
		}
	})

	t.Run("minMaxInfo", func(t *testing.T) {
		src := minMaxInfo{
			MaxSize:      point{X: 1920, Y: 1080},
			MaxPosition:  point{X: -8, Y: -8},
			MinTrackSize: point{X: 200, Y: 120},
			MaxTrackSize: point{X: 3840, Y: 2160},
		}
		dst := &minMaxInfo{}
		writeMinMaxInfo(uintptr(unsafe.Pointer(dst)), &src)
		if *dst != src {
			t.Fatalf("writeMinMaxInfo stored %+v, want %+v", *dst, src)
		}
		got, ok := readMinMaxInfo(uintptr(unsafe.Pointer(dst)))
		runtime.KeepAlive(dst)
		if !ok || got != src {
			t.Fatalf("readMinMaxInfo = %+v ok=%v, want %+v", got, ok, src)
		}
	})

	t.Run("a zero address is refused, not dereferenced", func(t *testing.T) {
		if _, ok := readRect(0); ok {
			t.Error("readRect(0) reported ok")
		}
		if _, ok := readMinMaxInfo(0); ok {
			t.Error("readMinMaxInfo(0) reported ok")
		}
		// A guarded write is a no-op, not a crash.
		writeRect(0, &rect{Left: 1})
		writeMinMaxInfo(0, &minMaxInfo{})
		dst := &rect{}
		writeRect(uintptr(unsafe.Pointer(dst)), nil) // value == nil
		runtime.KeepAlive(dst)
		if (*dst != rect{}) {
			t.Errorf("writeRect with a nil value wrote %+v", *dst)
		}
	})
}
