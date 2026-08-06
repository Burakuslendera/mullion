//go:build windows && !amd64

package doctor

import (
	"runtime"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// Executed as a real Windows/386 process under WOW64 in CI. A UNC pin makes the
// ordering load-bearing: Probe must report the architecture without expanding
// or otherwise surfacing the caller-selected path, and without reaching monitor
// callback or machine-discovery work.
func TestUnsupportedArchitectureDoctorProbeStopsBeforeMachineAndPathProbes(t *testing.T) {
	const pinned = `\\server\must-not-be-read\runtime`
	t.Setenv(webview2.BrowserExecutableFolderEnv, pinned)

	originalCallback := newMonitorCallback
	callbackAllocations := 0
	newMonitorCallback = func(callback any) uintptr {
		callbackAllocations++
		return originalCallback(callback)
	}
	t.Cleanup(func() { newMonitorCallback = originalCallback })

	report := Probe("architecture-gate-test")
	if callbackAllocations != 0 {
		t.Fatalf("unsupported Probe allocated %d monitor callbacks, want 0", callbackAllocations)
	}
	if report.Usable() {
		t.Fatal("unsupported architecture report is usable")
	}
	for _, want := range []string{"GOARCH=" + runtime.GOARCH, "windows/amd64"} {
		if !strings.Contains(report.WebView2.Problem, want) {
			t.Errorf("problem = %q, want %q", report.WebView2.Problem, want)
		}
	}
	if report.WebView2.PinnedEnv != "" || report.WebView2.Folder != "" {
		t.Fatalf("unsupported Probe read or reported the pinned path: %+v", report.WebView2)
	}
	if report.OS != "" || len(report.GPUs) != 0 || len(report.Monitors) != 0 || len(report.Homes) != 0 {
		t.Fatalf("unsupported Probe reached machine probes: OS=%q GPUs=%v monitors=%v homes=%v", report.OS, report.GPUs, report.Monitors, report.Homes)
	}
}
