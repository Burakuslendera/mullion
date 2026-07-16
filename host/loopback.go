package host

import (
	"errors"
	"net/url"
	"strings"
)

// Config.URL lets a caller point mullion at a URL they serve themselves instead of
// the in-process virtual host (see docs/decisions/0012). mullion never opens a
// socket - the caller runs the server, and mullion only navigates there.
//
// This file owns the two rules that make that safe, and it is the one place in the
// tree allowed to name the loopback hosts: leak_test.go exempts it, because here the
// names *restrict* Config.URL to the local machine, which is the opposite of opening
// a socket. Nothing here listens; net/url only parses.

// loopbackHosts are the only hosts Config.URL may name. mullion injects window.<ns>
// - the window controls and Config.Bridge, i.e. the application's own Go methods -
// into whatever it navigates to. A non-loopback origin could therefore drive the
// window and call into Go from across the network, so the URL is pinned to the
// local machine.
var loopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"::1":       true,
}

// validateURL checks Config.URL. Empty is valid, and is the default: assets are
// served from the in-process virtual host and mullion opens nothing. A non-empty URL
// must use http or https and must name a loopback host. This decides only where
// mullion navigates; it never opens the socket.
func validateURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("mullion: Config.URL is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("mullion: Config.URL must use http or https")
	}
	if !loopbackHosts[parsed.Hostname()] {
		return errors.New("mullion: Config.URL must name a loopback host (the local machine only)")
	}
	return nil
}

// urlOrigin returns scheme://host[:port] for a log line, dropping any path, query or
// fragment so a token a caller placed in the URL never reaches a log. It runs only
// after validateURL has accepted the URL; the error branch is belt and braces.
func urlOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	return parsed.Scheme + "://" + parsed.Host
}

// assetSourceSummary is the startup line that states, on every run, where the
// frontend is served from - so a bug report shows whether Config.URL was set without
// anyone having to ask. It is logged at INFO alongside the version line, and the URL
// is reduced to its origin first (urlOrigin) so no path is disclosed.
func assetSourceSummary(config Config) string {
	if config.URL == "" {
		return "mullion: asset source=embedded-fs, virtual_host=" + config.origin()
	}
	return "mullion: asset source=external-url, url=" + urlOrigin(config.URL)
}

// trustedOrigin is the single origin the injected bridge may be driven from: the
// virtual host that serves the embedded fs.FS (https://<VirtualHost>), or the
// loopback origin the caller serves when Config.URL is set. Scheme://host[:port],
// no path.
func (config Config) trustedOrigin() string {
	return urlOrigin(config.startURL())
}

// messageSourceAllowed decides whether a web message posted from source may reach
// the bridge. window.<ns> - the window controls and Config.Bridge, the application's
// own Go methods - is injected into every document the WebView loads (decisions/0014),
// so this is an allow-list, not a deny-list: only mullion's own surfaces pass. That
// is the trusted origin (the virtual host, or the Config.URL origin) and the data:
// error page (errorpage.go). Everything else is rejected - a foreign http/https
// origin, and also blob:/filesystem:/file: (which carry a real web origin a bare
// scheme check would wave through) and about:blank/"" (which a script-driven top
// navigation can reach while inheriting the previous document's origin). Admitting
// data: is safe: only mullion itself can put a data: document in the top frame,
// because browsers block a script-driven top navigation to a data: URL.
func (config Config) messageSourceAllowed(source string) bool {
	if strings.HasPrefix(source, "data:") {
		return true
	}
	return sameHTTPOrigin(source, config.trustedOrigin())
}

// messageSourceTrusted reports whether a message from source may drive the
// application's own Config.Bridge methods, not just the reserved window controls.
// Only the trusted origin qualifies. A data: source is allowed (messageSourceAllowed)
// so the error page's caption buttons work, but it is NOT trusted for Config.Bridge:
// a data: document may be a hostile iframe a script created, not mullion's own error
// surface (decisions/0014).
func (config Config) messageSourceTrusted(source string) bool {
	return sameHTTPOrigin(source, config.trustedOrigin())
}

// sameHTTPOrigin reports whether raw and trusted are the same http/https origin -
// scheme, host (case-insensitive) and port, with the default port normalised so that
// https://x and https://x:443 match. A non-http/https scheme (blob:, file:, ...) is
// never the same origin as the trusted http/https one, so it is rejected.
func sameHTTPOrigin(raw, trusted string) bool {
	a, err := url.Parse(raw)
	if err != nil {
		return false
	}
	b, err := url.Parse(trusted)
	if err != nil {
		return false
	}
	if a.Scheme != b.Scheme || (a.Scheme != "http" && a.Scheme != "https") {
		return false
	}
	return strings.EqualFold(a.Hostname(), b.Hostname()) && defaultedPort(a) == defaultedPort(b)
}

// defaultedPort returns the URL's port, or the scheme default (443 for https, 80 for
// http) when none is given, so an explicit default port compares equal to none.
func defaultedPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
