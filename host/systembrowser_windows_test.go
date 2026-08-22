//go:build windows

package host

import (
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// isExternalBrowserSafe is the gate on what a foreign document's window.open can
// make the host launch through ShellExecute. Literal double quotes, ASCII spaces,
// and C0, DEL, or C1 controls are refused at the ShellExecute argument boundary;
// this is not URI normalization or proof of browser behavior. Only http/https
// may pass; every other scheme - file:, a custom protocol handler, javascript:,
// data:, about:, or a URL that does not parse - must be refused, so off-origin
// content cannot name a dangerous handler and have the host open it (issue #6).
func TestIsExternalBrowserSafe(t *testing.T) {
	for _, uri := range []string{
		"http://example.com/",
		"https://example.com/path?q=1",
		"HTTPS://EXAMPLE.COM",  // scheme is matched case-insensitively
		"https://[::1]:8443/x", // an ipv6 loopback target is still http(s)
		"https://example.com/%22",
		"https://example.com/%20",
		"https://example.com/%1F",
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
		`https://example.com/"`,
		"https://example.com/a b",
		"https://example.com/\x1f",
		"https://example.com/\x7f",
		"https://example.com/\u0085", // url.Parse alone accepts this C1 control
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

func TestProductionNewWindowRoutesNonGestureURIExactlyAndReducesDiagnostics(t *testing.T) {
	const target = "https://alice:hunter2@popup.example/opened.html?token=s3cr3t#private-fragment"
	host, logger := newTestHost(t, Config{StartHidden: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	host.newWebViewBrowser().NewWindowRequestedCallback(webview2.NewWindowRequestedObservation{
		URI:             target,
		IsUserInitiated: false,
	})

	if len(opened) != 1 || opened[0] != target {
		t.Fatalf("system-browser hand-off = %v, want exact observed URI %q once", opened, target)
	}
	logged := logger.String()
	if !strings.Contains(logged,
		"new window routed to system browser, user_initiated=false, uri=https://popup.example/opened.html?#\n") {
		t.Fatalf("non-gesture route diagnostic was not reduced as expected:\n%s", logged)
	}
	for _, forbidden := range []string{"alice", "hunter2", "token", "s3cr3t", "private-fragment"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("route diagnostic leaked %q from userinfo, query, or fragment:\n%s", forbidden, logged)
		}
	}
}

func TestProductionNewWindowRejectsMalformedUserinfoWithoutDiagnosticDisclosure(t *testing.T) {
	const target = "https://alice:bad%zz@evil.example?token=s3cr3t#private-fragment"
	host, logger := newTestHost(t, Config{StartHidden: true})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	host.newWebViewBrowser().NewWindowRequestedCallback(webview2.NewWindowRequestedObservation{
		URI:             target,
		IsUserInitiated: true,
	})

	if len(opened) != 0 {
		t.Fatalf("malformed target reached the system-browser hand-off: %v", opened)
	}
	logged := logger.String()
	if !strings.Contains(logged, "new window dropped, target not admitted, uri=unknown?#\n") {
		t.Fatalf("malformed target diagnostic was not reduced as expected:\n%s", logged)
	}
	for _, forbidden := range []string{"alice", "bad", "evil.example", "token", "s3cr3t", "private-fragment"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("malformed target diagnostic leaked %q:\n%s", forbidden, logged)
		}
	}
}

func TestProductionCancelRouteRejectsMalformedUserinfoWithoutDiagnosticDisclosure(t *testing.T) {
	const target = "https://alice:bad%zz@evil.example?token=s3cr3t#private-fragment"
	host, logger := newTestHost(t, Config{
		StartHidden:           true,
		PinNavigationToOrigin: true,
	})
	var opened []string
	host.openExternal = func(uri string) { opened = append(opened, uri) }

	if !cancelNavigation(host, target, 75, true) {
		t.Fatal("pin route did not accept cancellation for the off-origin malformed target")
	}
	if len(opened) != 0 {
		t.Fatalf("malformed cancelled target reached the system-browser hand-off: %v", opened)
	}
	logged := logger.String()
	if !strings.Contains(logged,
		"navigation cancelled off origin, target not admitted, uri=unknown?#\n") {
		t.Fatalf("malformed cancelled-target diagnostic was not reduced as expected:\n%s", logged)
	}
	for _, forbidden := range []string{"alice", "bad", "evil.example", "token", "s3cr3t", "private-fragment"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("malformed cancelled-target diagnostic leaked %q:\n%s", forbidden, logged)
		}
	}
}

// cancelNavigation drives the pair of callbacks the runtime drives for one
// NavigationStarting the gate wants cancelled: the decision, and - only if the
// runtime's put_Cancel succeeded - the commit. Between them sits the call this
// suite cannot make, which is the point of the split (issue #73,
// decisions/0027): a test that wants the failed-cancel path simply does not call
// the second half.
//
// It enters at noteAndGateNavigation, not at shouldCancelNavigation, because
// that is what the runtime's callback calls (host/webview_windows.go) - so the
// navigation target is recorded and the surface claim runs first, exactly as
// they do live. Entering one level lower left a blind spot: a commit added to
// noteAndGateNavigation, which is the pre-issue-73 fail-open one level up, went
// unnoticed by every test that used this helper.
func cancelNavigation(host *Host, uri string, navigationID uint64, isUserInitiated bool) bool {
	if !host.noteAndGateNavigation(uri, navigationID) {
		return false
	}
	host.noteNavigationCancelled(uri, navigationID, isUserInitiated)
	return true
}

// shouldCancelNavigation is the PinNavigationToOrigin gate at NavigationStarting.
// Off (default) it never cancels; on, an off-origin navigation is cancelled, and
// a non-http(s) target is dropped rather than routed. The trusted origin is
// never cancelled (issue #6, decisions/0023). What an http/https target is
// handed to is locked separately below, through the openExternal seam; only the
// ShellExecute call itself is live-only now (issue #76).
func TestShouldCancelNavigation(t *testing.T) {
	off, _ := newTestHost(t, Config{StartHidden: true})
	if cancelNavigation(off, "https://evil.example/", 0, true) {
		t.Error("gate off: shouldCancelNavigation cancelled a navigation")
	}

	on, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	// The trusted origin passes - the surface, SPA routing and in-origin links
	// must never be cancelled.
	if cancelNavigation(on, on.source.origin.text+"/app", 1, true) {
		t.Error("gate on: cancelled an on-origin navigation")
	}
	// An off-origin target rejected by the shared gate is cancelled and dropped,
	// never routed.
	if !cancelNavigation(on, "blob:https://evil.example/uuid", 2, false) {
		t.Error("gate on: did not cancel an off-origin blob: navigation")
	}
	if !strings.Contains(logger.String(), "navigation cancelled off origin, target not admitted") {
		t.Fatalf("off-origin blob: was not dropped as not admitted:\n%s", logger.String())
	}
	if strings.Contains(logger.String(), "routed to system browser") {
		t.Fatalf("a non-http(s) navigation reached the system-browser route:\n%s", logger.String())
	}
}

// The log lines these two paths emit must name the host the navigation went to.
// They did not: logsafe.Message treats "https://" as a Windows drive letter and
// "//" as a UNC start, so the whole URL collapsed to its last path segment and
// "https://evil.example/x?q=1" reached the log as "httpx?q=1" (issue #78).
//
// The query goes the other way: it is dropped, because a token in a query string
// is the disclosure risk this reduction exists for. Only a bare "?" survives, so
// two navigations differing only in their query stay distinguishable.
//
// Asserted against the end of the line, not with a bare Contains. "uri=" is the
// last field on both lines, so anchoring on the newline is what makes the
// dropped query provable: a Contains on ".../x?" alone is equally happy with
// ".../x?token=s3cr3t", and would keep passing if the query came back.
func TestRoutingLogsNameTheHost(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true, PinNavigationToOrigin: true})

	if !cancelNavigation(host, "https://evil.example/x?token=s3cr3t", 7, true) {
		t.Fatal("gate on: an off-origin navigation must be cancelled")
	}
	host.routeNewWindow("http://popup.example/opened.html", true)

	logged := logger.String()
	for _, want := range []string{
		"uri=https://evil.example/x?\n",
		"uri=http://popup.example/opened.html\n",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log does not end the line with %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "s3cr3t") || strings.Contains(logged, "token") {
		t.Errorf("log leaked the query string:\n%s", logged)
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
	if !cancelNavigation(host, "https://evil.example/x?q=1", 3, true) {
		t.Fatal("gate on: an off-origin navigation must be cancelled")
	}
	// A new window is routed without any gate involved (decisions/0022).
	host.routeNewWindow("http://evil.example/popup", true)
	// Neither path may hand over a scheme ShellExecute would resolve elsewhere.
	if !cancelNavigation(host, "blob:https://evil.example/uuid", 4, false) {
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
