//go:build windows && !amd64

package host

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The Windows/386 CI job executes this through WOW64. It drives New and Run,
// not a duplicate architecture helper, and therefore fails if the public host
// bypasses the production gate before DPI, COM, class, callback, or HWND work.
func TestUnsupportedArchitectureHostRunReturnsPublicSentinelBeforeNativeStartup(t *testing.T) {
	originalDPI := applyProcessDPIAwareness
	originalDiscovery := discoverWebViewRuntime
	var dpiCalls, discoveryCalls int
	applyProcessDPIAwareness = func() error {
		dpiCalls++
		return nil
	}
	discoverWebViewRuntime = func() (string, string, error) {
		discoveryCalls++
		return "", "", webview2.ErrUnsupportedArchitecture
	}
	defer func() {
		applyProcessDPIAwareness = originalDPI
		discoverWebViewRuntime = originalDiscovery
	}()
	host := New(Config{})
	if host.architectureErr == nil {
		t.Fatalf("New did not retain the unsupported %s architecture result", runtime.GOARCH)
	}
	if host.dpiAwarenessErr != nil {
		t.Fatalf("New attempted or reported DPI setup after architecture rejection: %v", host.dpiAwarenessErr)
	}
	if dpiCalls != 0 {
		t.Fatalf("unsupported New crossed the architecture gate into DPI setup %d times", dpiCalls)
	}

	err := host.Run()
	if !errors.Is(err, ErrUnsupportedArchitecture) {
		t.Fatalf("Run error = %v, want public ErrUnsupportedArchitecture", err)
	}
	if !errors.Is(err, webview2.ErrUnsupportedArchitecture) {
		t.Fatalf("Run error = %v, want wrapped internal architecture cause", err)
	}
	for _, want := range []string{"GOARCH=" + runtime.GOARCH, "windows/amd64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Run error = %q, want %q", err, want)
		}
	}
	if discoveryCalls != 0 {
		t.Fatalf("unsupported Run crossed the architecture gate into runtime discovery %d times", discoveryCalls)
	}
	if host.running || host.hwnd != 0 || host.wndProc != 0 || host.instance != 0 {
		t.Fatalf("unsupported Run reached native startup state: running=%t hwnd=%#x wndProc=%#x instance=%#x", host.running, host.hwnd, host.wndProc, host.instance)
	}

	invalidSourceHost := New(Config{VirtualHost: "127.1"})
	if invalidSourceHost.sourceErr == nil {
		t.Fatal("invalid source did not retain its source-plan error")
	}
	err = invalidSourceHost.Run()
	if !errors.Is(err, ErrUnsupportedArchitecture) || strings.Contains(err.Error(), "Config.VirtualHost") {
		t.Fatalf("invalid-source Run error = %v, want architecture sentinel before source error", err)
	}
	if dpiCalls != 0 || discoveryCalls != 0 {
		t.Fatalf("invalid-source unsupported host reached native startup: DPI %d, discovery %d", dpiCalls, discoveryCalls)
	}
}
