//go:build windows

package host

import (
	"strings"
	"testing"
)

// The flag script is a contract with the frontend: the page reads
// window.<ns>.tabTitlebar to decide whether it may own the title bar. Renaming
// either side without the other silently leaves the window undraggable.
func TestTabStripFlagMatchesFrontendContract(t *testing.T) {
	host := New(Config{})
	if host.js.tabFlag != "window.mullion.tabTitlebar = true;" {
		t.Fatalf("tab strip flag script changed: %q", host.js.tabFlag)
	}

	custom := New(Config{JSNamespace: "acme"})
	if custom.js.tabFlag != "window.acme.tabTitlebar = true;" {
		t.Fatalf("tab strip flag ignored the namespace: %q", custom.js.tabFlag)
	}
	if strings.Contains(custom.js.tabFlag, "mullion") {
		t.Fatalf("custom namespace still carries the default: %q", custom.js.tabFlag)
	}
}

// A missing Browser is a lifecycle failure, not an optional capability miss:
// treating it as advisory would let createWebView emit readiness/navigation
// logs after teardown.
func TestApplyTabStripStartupRejectsUnavailableBrowser(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	if err := host.applyTabStripStartup(nil); err == nil {
		t.Fatal("nil browser returned nil")
	}
	if strings.Contains(logger.String(), "tab strip startup registration requested") {
		t.Fatalf("unavailable browser logged startup success:\n%s", logger.String())
	}
}
