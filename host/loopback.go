package host

import (
	"errors"
	"strings"
)

// Config.URL points the WebView at a caller-served loopback URL. Mullion never
// opens a socket; this allow-list only restricts where the injected bridge may
// run. Source-plan construction is the sole production parser of Config.URL.
var loopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"::1":       true,
}

func buildExternalSourcePlan(raw string) (sourcePlan, error) {
	origin, parsed, err := parseCanonicalHTTPOrigin(raw)
	if err != nil {
		return sourcePlan{}, errors.New("mullion: Config.URL " + err.Error())
	}
	if !loopbackHosts[origin.host] {
		return sourcePlan{}, errors.New("mullion: Config.URL must name a loopback host (the local machine only)")
	}

	// Keep the caller-owned path, query, fragment and optional userinfo, but
	// canonicalise the scheme, host and default port once for every consumer.
	parsed.Scheme = origin.scheme
	parsed.Host = strings.TrimPrefix(origin.text, origin.scheme+"://")
	startURL := parsed.String()
	return sourcePlan{
		startURL:    startURL,
		origin:      origin,
		retryTarget: origin.text,
		summary:     "mullion: asset source=external-url, url=" + origin.text,
	}, nil
}
