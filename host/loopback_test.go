package host

import "testing"

// This file is exempt from the loopback-literal check in TestNoNetworkListener: it,
// like loopback.go, names the loopback hosts on purpose - to prove Config.URL is
// pinned to them. No socket is opened here; these are string checks.

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
	if got := base.startURL(); got != "https://mullion.local/index.html" {
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
	if got := assetSourceSummary(base); got != "mullion: asset source=embedded-fs, virtual_host=https://mullion.local" {
		t.Fatalf("assetSourceSummary (embedded) = %q", got)
	}
	// The path and query are dropped: only scheme://host:port reaches the log, so a
	// token a caller put in the URL is never disclosed.
	ext := Config{URL: "http://127.0.0.1:8080/private?token=secret"}.normalise()
	if got := assetSourceSummary(ext); got != "mullion: asset source=external-url, url=http://127.0.0.1:8080" {
		t.Fatalf("assetSourceSummary (external) = %q, want the path and query dropped", got)
	}
}

// TestMessageSourceAllowed locks the bridge-origin gate (decisions/0014): a web
// message is dropped when its source is a concrete http/https origin other than
// the trusted one, while the frontend, the data: error surface and about:blank all
// pass so no first-party surface is broken.
func TestMessageSourceAllowed(t *testing.T) {
	asset := Config{}.normalise()                            // virtual host https://mullion.local
	loop := Config{URL: "http://127.0.0.1:8080"}.normalise() // caller loopback origin

	allowed := []struct {
		name   string
		config Config
		source string
	}{
		{"trusted virtual host", asset, "https://mullion.local/index.html"},
		{"trusted virtual host root", asset, "https://mullion.local/"},
		{"data error page", asset, "data:text/html,%3Chtml%3E"},
		{"about blank", asset, "about:blank"},
		{"empty source", asset, ""},
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
		{"scheme downgrade of trusted host", asset, "http://mullion.local"},
	}
	for _, c := range rejected {
		if c.config.messageSourceAllowed(c.source) {
			t.Errorf("%s: messageSourceAllowed(%q) = true, want rejected", c.name, c.source)
		}
	}
}
