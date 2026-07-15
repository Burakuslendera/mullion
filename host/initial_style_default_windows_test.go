//go:build windows

package host

import "testing"

func TestNativeInitialWindowStyleDefaultUsesProjectStyle(t *testing.T) {
	if got := nativeInitialWindowStyle(); got != uintptr(wsNativeWindow) {
		t.Fatalf("nativeInitialWindowStyle() = 0x%x, want 0x%x", got, uintptr(wsNativeWindow))
	}
	if got := nativeInitialWindowStyleName(); got != "native" {
		t.Fatalf("nativeInitialWindowStyleName() = %q", got)
	}
}
