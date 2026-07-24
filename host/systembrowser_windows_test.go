//go:build windows

package host

import (
	"strings"
	"testing"
)

// isExternalBrowserSafe is the gate on what a foreign document's window.open can
// make the host launch through ShellExecute. Only http/https may pass; every
// other scheme - file:, a custom protocol handler, javascript:, data:, about:,
// or a URL that does not parse - must be refused, so off-origin content cannot
// name a dangerous handler and have the host open it (issue #6).
func TestIsExternalBrowserSafe(t *testing.T) {
	for _, uri := range []string{
		"http://example.com/",
		"https://example.com/path?q=1",
		"HTTPS://EXAMPLE.COM",  // scheme is matched case-insensitively
		"https://[::1]:8443/x", // an ipv6 loopback target is still http(s)
	} {
		if !isExternalBrowserSafe(uri) {
			t.Errorf("isExternalBrowserSafe(%q) = false, want true", uri)
		}
	}

	for _, uri := range []string{
		"file:///etc/shadow",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"ms-settings:privacy",
		"steam://run/123",
		"vbscript:msgbox",
		`\\attacker\share\payload`, // a UNC path, not an http(s) URL
		"about:blank",
		"://noscheme", // does not parse
		"ftp://host/file",
		"",
	} {
		if isExternalBrowserSafe(uri) {
			t.Errorf("isExternalBrowserSafe(%q) = true, want false", uri)
		}
	}
}

// routeNewWindow must never reach the system-browser hand-off for a scheme
// isExternalBrowserSafe rejects: it logs the drop and returns.
func TestRouteNewWindowDropsUnsafeSchemes(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.routeNewWindow("javascript:alert(1)", true)

	if !strings.Contains(logger.String(), "new window dropped") {
		t.Fatalf("an unsafe scheme was not dropped:\n%s", logger.String())
	}
	if strings.Contains(logger.String(), "routed to system browser") {
		t.Fatalf("an unsafe scheme reached the system-browser route:\n%s", logger.String())
	}
}

// shouldCancelNavigation is the PinNavigationToOrigin gate at NavigationStarting.
// Off (default) it never cancels; on, an off-origin navigation is cancelled, and
// a non-http(s) target is dropped rather than routed. The trusted origin is
// never cancelled (issue #6, decisions/0023). What an http/https target is
// handed to is locked separately below, through the openExternal seam; only the
// ShellExecute call itself is live-only now (issue #76).
func TestShouldCancelNavigation(t *testing.T) {
	off, _ := newTestHost(t, Config{StartHidden: true})
	if off.shouldCancelNavigation("https://evil.example/", 0, true) {
		t.Error("gate off: shouldCancelNavigation cancelled a navigation")
	}

	on, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	// The trusted origin passes - the surface, SPA routing and in-origin links
	// must never be cancelled.
	if on.shouldCancelNavigation(on.config.trustedOrigin()+"/app", 1, true) {
		t.Error("gate on: cancelled an on-origin navigation")
	}
	// An off-origin non-http(s) target is cancelled and dropped, never routed.
	if !on.shouldCancelNavigation("blob:https://evil.example/uuid", 2, false) {
		t.Error("gate on: did not cancel an off-origin blob: navigation")
	}
	if !strings.Contains(logger.String(), "navigation cancelled off origin, unsupported scheme") {
		t.Fatalf("off-origin blob: was not dropped as unsupported:\n%s", logger.String())
	}
	if strings.Contains(logger.String(), "routed to system browser") {
		t.Fatalf("a non-http(s) navigation reached the system-browser route:\n%s", logger.String())
	}
}

// The routing half of both paths, which used to be live-only: the seam records
// what would have been handed to the system browser, so the exact target - and
// the fact that only a safe scheme reaches it at all - is pinned headless
// (issue #76). The ShellExecute call itself stays live-only; this locks
// everything up to it.
func TestSafeTargetsAreHandedToTheSystemBrowser(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	// The gate cancels an off-origin http(s) navigation and routes it verbatim -
	// query and all, since the user was going there.
	if !host.shouldCancelNavigation("https://evil.example/x?q=1", 3, true) {
		t.Fatal("gate on: an off-origin navigation must be cancelled")
	}
	// A new window is routed without any gate involved (decisions/0022).
	host.routeNewWindow("http://evil.example/popup", true)
	// Neither path may hand over a scheme ShellExecute would resolve elsewhere.
	if !host.shouldCancelNavigation("blob:https://evil.example/uuid", 4, false) {
		t.Fatal("gate on: an off-origin blob: navigation must be cancelled")
	}
	host.routeNewWindow("javascript:alert(1)", true)

	want := []string{"https://evil.example/x?q=1", "http://evil.example/popup"}
	if len(opened) != len(want) {
		t.Fatalf("handed over %v, want exactly %v", opened, want)
	}
	for i := range want {
		if opened[i] != want[i] {
			t.Fatalf("handed over %v, want %v", opened, want)
		}
	}
}
