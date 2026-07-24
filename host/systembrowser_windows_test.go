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

// routeNewWindow must never reach ShellExecute (openInSystemBrowser) for a
// scheme isExternalBrowserSafe rejects: it logs the drop and returns. Only the
// drop path is exercised headless; the http/https path launches a real browser
// and is verified live.
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
