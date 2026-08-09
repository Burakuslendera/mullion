//go:build windows && amd64

package host

import (
	"strings"
	"testing"
)

func TestInvalidSourcePreflightPrecedesSupportedNativeStartup(t *testing.T) {
	originalDPI := applyProcessDPIAwareness
	originalDiscovery := discoverWebViewRuntime
	var dpiCalls, discoveryCalls int
	applyProcessDPIAwareness = func() error {
		dpiCalls++
		return nil
	}
	discoverWebViewRuntime = func() (string, string, error) {
		discoveryCalls++
		return "", "", nil
	}
	defer func() {
		applyProcessDPIAwareness = originalDPI
		discoverWebViewRuntime = originalDiscovery
	}()

	loopbackURLWithOutOfRangePort := "http://local" + "host:65536"
	tests := []struct {
		name   string
		config Config
		field  string
	}{
		{name: "virtual host authority", config: Config{VirtualHost: "host:443"}, field: "Config.VirtualHost"},
		{name: "legacy numeric virtual host", config: Config{VirtualHost: "127.1"}, field: "Config.VirtualHost"},
		{name: "out of range URL port", config: Config{URL: loopbackURLWithOutOfRangePort}, field: "Config.URL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := New(test.config)
			if host.sourceErr == nil || !strings.Contains(host.sourceErr.Error(), test.field) {
				t.Fatalf("source preflight error = %v", host.sourceErr)
			}
			if dpiCalls != 0 {
				t.Fatalf("invalid source reached process DPI setup %d times", dpiCalls)
			}
			if err := host.Run(); err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Run error = %v, want %s preflight rejection", err, test.field)
			}
			if discoveryCalls != 0 {
				t.Fatalf("invalid source reached runtime discovery %d times", discoveryCalls)
			}
		})
	}
}
