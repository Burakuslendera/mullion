//go:build windows

package host

import "testing"

func TestPerMonitorV2DPIAwarenessContext(t *testing.T) {
	if dpiAwarenessContextPerMonitorAwareV2 != ^uintptr(3) {
		t.Fatalf("dpi context = %d, want per-monitor-v2 pseudo handle", dpiAwarenessContextPerMonitorAwareV2)
	}
}
