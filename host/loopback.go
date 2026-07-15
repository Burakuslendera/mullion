package host

import (
	"errors"
	"net/url"
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
