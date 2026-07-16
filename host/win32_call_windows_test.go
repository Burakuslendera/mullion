//go:build windows

package host

import "testing"

// TestGuardedWindowProcContainsPanic locks the invariant that a Go panic in the
// window procedure is recovered rather than allowed to unwind into the native
// DispatchMessage frame (which aborts the process). guardedWindowProc must call
// the reporter with the panic value and the message, return the fallback's
// result, and never re-panic.
func TestGuardedWindowProcContainsPanic(t *testing.T) {
	const sentinel = uintptr(0xD1CE)
	var gotPanic any
	var gotMessage uint32

	guarded := guardedWindowProc(
		func(windowHandle, uint32, uintptr, uintptr) uintptr { panic("boom") },
		func(windowHandle, uint32, uintptr, uintptr) uintptr { return sentinel },
		func(recovered any, message uint32) { gotPanic, gotMessage = recovered, message },
	)

	result := guarded(0, wmClose, 0, 0)
	if result != sentinel {
		t.Fatalf("panic path returned %#x, want the fallback result %#x", result, sentinel)
	}
	if gotPanic != "boom" {
		t.Fatalf("onPanic recovered %v, want \"boom\"", gotPanic)
	}
	if gotMessage != wmClose {
		t.Fatalf("onPanic saw message %#x, want %#x (wmClose)", gotMessage, wmClose)
	}
}

// TestGuardedWindowProcPassesThrough proves the guard is transparent when the
// procedure does not panic: the real return value flows through untouched and
// the fallback is never consulted.
func TestGuardedWindowProcPassesThrough(t *testing.T) {
	guarded := guardedWindowProc(
		func(_ windowHandle, _ uint32, wParam, _ uintptr) uintptr { return wParam + 1 },
		func(windowHandle, uint32, uintptr, uintptr) uintptr {
			t.Fatal("fallback called for a non-panicking procedure")
			return 0
		},
		nil,
	)
	if got := guarded(0, wmSize, 41, 0); got != 42 {
		t.Fatalf("passthrough returned %d, want 42", got)
	}
}
