package host

import "testing"

// This file is exempt from the loopback-literal check in TestNoNetworkListener: it,
// like loopback.go, names the loopback hosts on purpose - to prove Config.URL is
// pinned to them. No socket is opened here; these are string checks.

// testExternalURL is the caller-served URL every other test in this package uses
// when it needs Config.URL set. It is spelled here because this file is one of
// the two allowed to name a loopback host, so the rest of the package can model
// external-URL mode without each file having to be exempted. Nothing dials it.
const testExternalURL = "http://127.0.0.1:8080"

func TestValidateURLAcceptsOnlyLoopbackHTTP(t *testing.T) {
	valid := []string{
		"",                               // the default: no external URL, virtual host serves
		"http://127.0.0.1:8080",          // the common case
		"http://localhost:3000",          // localhost by name
		"http://localhost",               // no port is fine
		"https://127.0.0.1:8443",         // https loopback
		"http://[::1]:8080",              // IPv6 loopback
		"http://127.0.0.1:8080/app.html", // a path is allowed (only rejected from logs)
	}
	for _, raw := range valid {
		if err := validateURL(raw); err != nil {
			t.Errorf("validateURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{
		"http://example.com",        // remote host - the whole point of the check
		"https://192.168.1.10:8080", // LAN address, not loopback
		"http://10.0.0.5",           // private but not loopback
		"ftp://127.0.0.1",           // wrong scheme
		"file:///c:/app/index.html", // wrong scheme
		"127.0.0.1:8080",            // no scheme -> not http/https
		"://nonsense",               // unparseable enough to have no loopback host
	}
	for _, raw := range invalid {
		if err := validateURL(raw); err == nil {
			t.Errorf("validateURL(%q) = nil, want a rejection", raw)
		}
	}
}

func TestStartURLPrefersConfigURL(t *testing.T) {
	// Empty URL -> the in-process virtual host index, unchanged from before Config.URL.
	base := Config{}.normalise()
	if got := base.startURL(); got != "https://mullion.localhost/index.html" {
		t.Fatalf("startURL() with no Config.URL = %q, want the virtual host index", got)
	}
	// A set URL is navigated to verbatim - the caller's server owns its own paths.
	ext := Config{URL: "http://127.0.0.1:8080"}.normalise()
	if got := ext.startURL(); got != "http://127.0.0.1:8080" {
		t.Fatalf("startURL() with Config.URL = %q, want the caller URL verbatim", got)
	}
}

func TestAssetSourceSummaryStatesTheSourceAndRedactsPath(t *testing.T) {
	base := Config{}.normalise()
	if got := assetSourceSummary(base); got != "mullion: asset source=embedded-fs, virtual_host=https://mullion.localhost" {
		t.Fatalf("assetSourceSummary (embedded) = %q", got)
	}
	// The path and query are dropped: only scheme://host:port reaches the log, so a
	// token a caller put in the URL is never disclosed.
	ext := Config{URL: "http://127.0.0.1:8080/private?token=secret"}.normalise()
	if got := assetSourceSummary(ext); got != "mullion: asset source=external-url, url=http://127.0.0.1:8080" {
		t.Fatalf("assetSourceSummary (external) = %q, want the path and query dropped", got)
	}
}

// TestMessageSourceAllowed locks the bridge-origin gate (decisions/0014). It is an
// allow-list: only the trusted origin and the data: error surface may drive the
// bridge. Everything else is rejected - a foreign http/https origin, a blob:/
// filesystem:/file: document that launders a foreign origin, and about:blank/"" that
// a script-driven top navigation can reach.
func TestMessageSourceAllowed(t *testing.T) {
	asset := Config{}.normalise()                            // virtual host https://mullion.localhost
	loop := Config{URL: "http://127.0.0.1:8080"}.normalise() // caller loopback origin

	allowed := []struct {
		name   string
		config Config
		source string
	}{
		{"trusted virtual host", asset, "https://mullion.localhost/index.html"},
		{"trusted virtual host root", asset, "https://mullion.localhost/"},
		{"trusted host explicit default port", asset, "https://mullion.localhost:443/x"},
		{"trusted host case-insensitive", asset, "https://MULLION.LOCALHOST/x"},
		{"data error page", asset, "data:text/html,%3Chtml%3E"},
		{"trusted loopback url", loop, "http://127.0.0.1:8080/app"},
	}
	for _, c := range allowed {
		if !c.config.messageSourceAllowed(c.source) {
			t.Errorf("%s: messageSourceAllowed(%q) = false, want allowed", c.name, c.source)
		}
	}

	rejected := []struct {
		name   string
		config Config
		source string
	}{
		{"foreign https origin", asset, "https://evil.example/x"},
		{"foreign http origin", asset, "http://evil.example"},
		{"different loopback port", loop, "http://127.0.0.1:9999/x"},
		{"remote in url mode", loop, "https://evil.example"},
		{"scheme downgrade of trusted host", asset, "http://mullion.localhost"},
		{"blob laundering a foreign origin", asset, "blob:https://evil.example/uuid"},
		{"filesystem laundering a foreign origin", asset, "filesystem:https://evil.example/temporary/x"},
		{"file scheme", asset, "file:///c:/x"},
		{"about blank inherits the previous origin", asset, "about:blank"},
		{"empty source", asset, ""},
		{"userinfo cannot spoof the trusted host", asset, "https://mullion.localhost@evil.example/x"},
	}
	for _, c := range rejected {
		if c.config.messageSourceAllowed(c.source) {
			t.Errorf("%s: messageSourceAllowed(%q) = true, want rejected", c.name, c.source)
		}
	}
}

// TestMessageSourceTrusted locks the second half of decisions/0014, the one
// TestMessageSourceAllowed does not reach: a data: source is allowed (so the error
// page's caption buttons work) but is NOT trusted for Config.Bridge, because a data:
// document may be a hostile iframe a script created rather than mullion's own error
// surface. Only the trusted origin drives the application's own Go methods. Without
// this test the difference between the two functions is unlocked: collapsing
// messageSourceTrusted to messageSourceAllowed (both call sameHTTPOrigin, so it reads
// like harmless dedup) would make a data: iframe trusted - the exact hole 0014 closes
// - and every other test would still pass.
func TestMessageSourceTrusted(t *testing.T) {
	asset := Config{}.normalise()                            // virtual host https://mullion.localhost
	loop := Config{URL: "http://127.0.0.1:8080"}.normalise() // caller loopback origin

	trusted := []struct {
		name   string
		config Config
		source string
	}{
		{"trusted virtual host", asset, "https://mullion.localhost/index.html"},
		{"trusted host explicit default port", asset, "https://mullion.localhost:443/x"},
		{"trusted host case-insensitive", asset, "https://MULLION.LOCALHOST/x"},
		{"trusted loopback url", loop, "http://127.0.0.1:8080/app"},
	}
	for _, c := range trusted {
		if !c.config.messageSourceTrusted(c.source) {
			t.Errorf("%s: messageSourceTrusted(%q) = false, want trusted", c.name, c.source)
		}
	}

	untrusted := []struct {
		name   string
		config Config
		source string
	}{
		// The load-bearing case: allowed for reserved controls, never for Config.Bridge.
		{"data error surface is allowed but not trusted", asset, "data:text/html,%3Chtml%3E"},
		{"foreign https origin", asset, "https://evil.example/x"},
		{"different loopback port", loop, "http://127.0.0.1:9999/x"},
		{"scheme downgrade of trusted host", asset, "http://mullion.localhost"},
		{"blob laundering a foreign origin", asset, "blob:https://evil.example/uuid"},
		{"file scheme", asset, "file:///c:/x"},
		{"about blank inherits the previous origin", asset, "about:blank"},
		{"empty source", asset, ""},
		{"userinfo cannot spoof the trusted host", asset, "https://mullion.localhost@evil.example/x"},
	}
	for _, c := range untrusted {
		if c.config.messageSourceTrusted(c.source) {
			t.Errorf("%s: messageSourceTrusted(%q) = true, want untrusted", c.name, c.source)
		}
	}
}

// TestNavigationOffOrigin locks the PinNavigationToOrigin gate's pure decision
// (issue #6, decisions/0023). Off by default it never reports off-origin, so the
// gate cancels nothing. On, it passes the trusted origin - any path on it - and
// mullion's own data: surface, and reports every foreign target off-origin,
// including the blob:/file:/about:blank forms a bare scheme check would admit.
func TestNavigationOffOrigin(t *testing.T) {
	off := Config{}.normalise()                           // gate off (default)
	on := Config{PinNavigationToOrigin: true}.normalise() // virtual host, gate on
	onURL := Config{URL: "http://127.0.0.1:8080", PinNavigationToOrigin: true}.normalise()

	// Gate off: nothing is off-origin, whatever the URI - existing behaviour.
	for _, uri := range []string{"https://evil.example/", "https://mullion.localhost/x", "about:blank", "data:text/html,x"} {
		if off.navigationOffOrigin(uri) {
			t.Errorf("gate off: navigationOffOrigin(%q) = true, want false (never cancels)", uri)
		}
	}

	onOrigin := []struct {
		name   string
		config Config
		uri    string
	}{
		{"trusted virtual host root", on, "https://mullion.localhost/"},
		{"trusted host any path", on, "https://mullion.localhost/app/route?q=1"},
		{"trusted host explicit default port", on, "https://mullion.localhost:443/x"},
		{"trusted host case-insensitive", on, "https://MULLION.LOCALHOST/x"},
		{"the error surface (data:)", on, "data:text/html,%3Chtml%3E"},
		{"trusted loopback url", onURL, "http://127.0.0.1:8080/app"},
	}
	for _, c := range onOrigin {
		if c.config.navigationOffOrigin(c.uri) {
			t.Errorf("%s: navigationOffOrigin(%q) = true, want false (on-origin/surface)", c.name, c.uri)
		}
	}

	offOrigin := []struct {
		name   string
		config Config
		uri    string
	}{
		{"foreign https origin", on, "https://evil.example/"},
		{"scheme downgrade of trusted host", on, "http://mullion.localhost/x"},
		{"userinfo cannot spoof the trusted host", on, "https://mullion.localhost@evil.example/x"},
		{"blob laundering a foreign origin", on, "blob:https://evil.example/uuid"},
		{"file scheme", on, "file:///c:/x"},
		{"about blank inherits the previous origin", on, "about:blank"},
		{"different loopback port in url mode", onURL, "http://127.0.0.1:9999/x"},
	}
	for _, c := range offOrigin {
		if !c.config.navigationOffOrigin(c.uri) {
			t.Errorf("%s: navigationOffOrigin(%q) = false, want true (off-origin)", c.name, c.uri)
		}
	}
}
