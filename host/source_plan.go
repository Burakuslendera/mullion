package host

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// sourcePlan is the immutable, pre-native description of the frontend source.
// New builds it once, then every normal source-plan trust and routing decision
// consumes one of its projections: origin is its sole admission identity;
// startURL is the caller-authorized initial navigation capability; retryTarget
// is derived from the origin; filterPattern is the embedded request boundary;
// summary is its redacted diagnostic form. The fallback surface uses a separate,
// generation-bound capability in errorSurfaceMessageAllowed. No consumer may
// re-read Config.URL or Config.VirtualHost and reconstruct a competing identity.
type sourcePlan struct {
	embedded      bool
	startURL      string
	origin        canonicalOrigin
	filterPattern string
	retryTarget   string
	summary       string
}

type canonicalOrigin struct {
	scheme string
	host   string
	port   string
	text   string
}

func buildSourcePlan(config Config) (sourcePlan, error) {
	if config.URL != "" {
		return buildExternalSourcePlan(config.URL)
	}

	host, err := canonicalVirtualHost(config.VirtualHost)
	if err != nil {
		return sourcePlan{}, errors.New("mullion: Config.VirtualHost is invalid: " + err.Error())
	}
	origin := newCanonicalOrigin("https", host, "")
	return sourcePlan{
		embedded:      true,
		startURL:      origin.text + "/index.html",
		origin:        origin,
		filterPattern: origin.text + "/*",
		retryTarget:   origin.text,
		summary:       "mullion: asset source=embedded-fs, virtual_host=" + origin.text,
	}, nil
}

func canonicalVirtualHost(raw string) (string, error) {
	if raw == "" {
		raw = defaultVirtualHost
	}
	if strings.Contains(raw, "%") {
		return "", errors.New("percent escapes are not allowed")
	}

	literal := raw
	if strings.HasPrefix(raw, "[") || strings.HasSuffix(raw, "]") {
		if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
			return "", errors.New("malformed IPv6 literal")
		}
		literal = raw[1 : len(raw)-1]
		if strings.Contains(literal, "%") {
			return "", errors.New("IPv6 zones are not allowed")
		}
		ip, err := netip.ParseAddr(literal)
		if err != nil || !ip.Is6() {
			return "", errors.New("malformed IPv6 literal")
		}
		return ip.String(), nil
	}

	if ip, err := netip.ParseAddr(raw); err == nil {
		return ip.String(), nil
	}
	if strings.ContainsAny(raw, ":@/\\?#[]") {
		return "", errors.New("authority, port, scheme, and path syntax are not allowed")
	}
	if strings.HasSuffix(raw, ".") {
		return "", errors.New("a trailing dot is not allowed")
	}
	if browserIPv4Number(raw) {
		return "", errors.New("legacy numeric IPv4 spellings are not allowed")
	}
	if len(raw) > 253 {
		return "", errors.New("host name is too long")
	}

	canonical := make([]byte, len(raw))
	for i := range len(raw) {
		c := raw[i]
		switch {
		case c >= 'A' && c <= 'Z':
			canonical[i] = c + ('a' - 'A')
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			canonical[i] = c
		default:
			return "", errors.New("only ASCII host labels are allowed")
		}
	}
	for _, label := range strings.Split(string(canonical), ".") {
		if label == "" {
			return "", errors.New("empty host labels are not allowed")
		}
		if len(label) > 63 {
			return "", errors.New("host label is too long")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("host labels cannot begin or end with a hyphen")
		}
	}
	return string(canonical), nil
}

func parseCanonicalHTTPOrigin(raw string) (canonicalOrigin, *url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" {
		return canonicalOrigin{}, nil, errors.New("not a valid absolute HTTP URL")
	}
	scheme, ok := lowerASCII(parsed.Scheme)
	if !ok || (scheme != "http" && scheme != "https") {
		return canonicalOrigin{}, nil, errors.New("must use http or https")
	}
	host, ok := lowerASCII(parsed.Hostname())
	if !ok || host == "" {
		return canonicalOrigin{}, nil, errors.New("host must be ASCII")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	}
	port, err := canonicalHTTPPort(parsed, scheme)
	if err != nil {
		return canonicalOrigin{}, nil, err
	}
	origin := newCanonicalOrigin(scheme, host, port)
	return origin, parsed, nil
}

func newCanonicalOrigin(scheme, host, port string) canonicalOrigin {
	if port == "" {
		port = defaultPort(scheme)
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != defaultPort(scheme) {
		authority += ":" + port
	}
	return canonicalOrigin{scheme: scheme, host: host, port: port, text: scheme + "://" + authority}
}

// matches parses an event/request candidate independently, but compares it to
// the plan's already-canonical identity. Keep the User check explicit: url.URL
// stores userinfo outside Host, so scheme/host/port equality alone would silently
// let a credential-bearing spelling prove canonical origin identity. An external
// startURL may retain userinfo only as an exact navigation capability.
func (origin canonicalOrigin) matches(raw string) bool {
	candidate, parsed, err := parseCanonicalHTTPOrigin(raw)
	return err == nil && parsed.User == nil &&
		candidate.scheme == origin.scheme && candidate.host == origin.host && candidate.port == origin.port
}

// messageSourceAllowed is source-only admission. The fallback surface is not a
// source-plan capability: Host.errorSurfaceMessageAllowed grants it only to the
// successfully observed source of the currently claimed surface generation.
func (plan sourcePlan) messageSourceAllowed(source string) bool {
	return plan.origin.matches(source)
}

func (plan sourcePlan) messageSourceTrusted(source string) bool {
	return plan.origin.matches(source)
}

// navigationOffOrigin treats the exact immutable start URL as a navigation-only
// capability. Equality authorizes the caller-selected initial navigation; it is
// not proof of origin, so every other candidate still has to pass the
// credential-free canonical origin check used by admission.
func (plan sourcePlan) navigationOffOrigin(uri string, enabled bool) bool {
	return enabled && uri != plan.startURL && !plan.origin.matches(uri)
}

func sourceOriginSummary(raw string) string {
	origin, _, err := parseCanonicalHTTPOrigin(raw)
	if err == nil {
		return origin.text
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque == "" {
		return ":unknown"
	}
	// blob: and filesystem: keep their wrapped HTTP source in Opaque; reducing
	// that inner origin preserves the rejection identity without its handle path.
	scheme, ok := lowerASCII(parsed.Scheme)
	if !ok || (scheme != "blob" && scheme != "filesystem") {
		return ":unknown"
	}
	inner, _, err := parseCanonicalHTTPOrigin(parsed.Opaque)
	if err != nil {
		return ":unknown"
	}
	return scheme + ":" + inner.text
}

// canonicalHTTPPort distinguishes an omitted port from an explicitly empty one.
// URL.Port returns "" for both; consulting the original Host first keeps
// "https://host" canonical while rejecting "https://host:" instead of silently
// laundering malformed authority syntax into the default port.
func canonicalHTTPPort(parsed *url.URL, scheme string) (string, error) {
	explicit := false
	if strings.HasPrefix(parsed.Host, "[") {
		if bracket := strings.LastIndexByte(parsed.Host, ']'); bracket >= 0 {
			explicit = bracket+1 < len(parsed.Host)
		}
	} else {
		explicit = strings.Contains(parsed.Host, ":")
	}
	if !explicit {
		return defaultPort(scheme), nil
	}

	port := parsed.Port()
	if port == "" {
		return "", errors.New("port must be a decimal uint16")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return "", errors.New("port must be a decimal uint16")
	}
	return strconv.FormatUint(number, 10), nil
}

// browserIPv4Number reports the WHATWG host-parser condition that makes a
// browser interpret a domain as an IPv4 address. Strict IPv4 literals have
// already returned above; reaching this condition means the browser and the
// source plan would otherwise disagree about the host's identity.
func browserIPv4Number(host string) bool {
	last := host
	if dot := strings.LastIndexByte(host, '.'); dot >= 0 {
		last = host[dot+1:]
	}
	if last == "" {
		return false
	}
	if len(last) > 2 && last[0] == '0' && (last[1] == 'x' || last[1] == 'X') {
		for i := 2; i < len(last); i++ {
			c := last[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	for i := range len(last) {
		if last[i] < '0' || last[i] > '9' {
			return false
		}
	}
	return true
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func lowerASCII(value string) (string, bool) {
	needsLower := false
	for i := range len(value) {
		c := value[i]
		if c >= 0x80 {
			return "", false
		}
		needsLower = needsLower || (c >= 'A' && c <= 'Z')
	}
	if !needsLower {
		return value, true
	}
	lower := []byte(value)
	for i := range len(lower) {
		if lower[i] >= 'A' && lower[i] <= 'Z' {
			lower[i] += 'a' - 'A'
		}
	}
	return string(lower), true
}
