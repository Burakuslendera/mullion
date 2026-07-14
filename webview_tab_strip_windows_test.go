//go:build windows

package mullion

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

// A nil chromium must warn and return, not panic: the caller reaches this path
// when the WebView2 runtime is missing.
func TestApplyTabStripStartupNilChromium(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.applyTabStripStartup(nil)

	if !strings.Contains(logger.String(), "mullion: tab strip startup skipped") {
		t.Fatalf("nil chromium was not reported:\n%s", logger.String())
	}
}
