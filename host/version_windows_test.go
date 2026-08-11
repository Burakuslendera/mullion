//go:build windows && amd64

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// TestRunStartupSummaryUsesPublicOutputBoundary must stay in a windows/amd64
// file: the three seams below are implemented by host_windows.go, while portable
// and unsupported-architecture lanes cannot drive this startup path. The test
// still drives the real Host.Run dispatcher. Replacing DPI/runtime discovery
// prevents native work; returning the
// architecture sentinel after discovery guarantees Run logs its startup summary
// and exits before COM, class registration, callbacks, HWND creation or a message
// pump. The seam counters make that headless boundary part of the assertion rather
// than an assumption hidden in the injected error.
func TestRunStartupSummaryUsesPublicOutputBoundary(t *testing.T) {
	originalDPI, originalDiscovery, originalVersion := applyProcessDPIAwareness, discoverWebViewRuntime, runtimeSummaryVersion
	t.Cleanup(func() {
		applyProcessDPIAwareness = originalDPI
		discoverWebViewRuntime = originalDiscovery
		runtimeSummaryVersion = originalVersion
	})

	dpiCalls, discoveryCalls := 0, 0
	applyProcessDPIAwareness = func() error {
		dpiCalls++
		return nil
	}
	discoverWebViewRuntime = func() (string, string, error) {
		discoveryCalls++
		return "", "150.0.4078.65", webview2.ErrUnsupportedArchitecture
	}
	version := "v0.1.0 (replaced by C:" + `\` + "Users" + `\` + "Alice" + `\dev\mullion` + ")"
	runtimeSummaryVersion = func() string { return version }
	logger := &captureLogger{}
	host := New(Config{Logger: logger})
	if err := host.Run(); err == nil {
		t.Fatal("Run continued past the injected architecture stop")
	}
	if discoveryCalls != 1 || dpiCalls != 1 {
		t.Fatalf("Run seams: discovery calls = %d, DPI calls = %d; want 1, 1", discoveryCalls, dpiCalls)
	}

	got := logger.String()
	if strings.Contains(got, "Alice") {
		t.Fatalf("Run startup dispatch disclosed the replacement path: %q", got)
	}
	for _, want := range []string{"v0.1.0", "mullion", "webview2=150.0.4078.65"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Run startup dispatch discarded %q: %q", want, got)
		}
	}
}
