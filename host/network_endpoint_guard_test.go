package host

import (
	"net/netip"
	"strings"
)

// These are shipped code/data formats, not policy prose. Markdown is deliberately
// excluded: scanning the decision records that document forbidden endpoints would
// make the guard depend on hiding its own specification. Publication prose has a
// separate, raw-text authority in scripts/leak-scan.ps1.
var shippedTextExtensions = map[string]bool{
	".cjs": true, ".cs": true, ".css": true, ".htm": true, ".html": true,
	".js": true, ".json": true, ".jsx": true, ".mjs": true, ".ps1": true,
	".svg": true, ".ts": true, ".tsx": true,
}

// stripExemptName removes the one intercepted virtual-host name from endpoint
// inspection, and only where it stands alone. A label before it or address
// syntax after it leaves the occurrence visible to the policy.
func stripExemptName(source, name string) string {
	label := func(c byte) bool {
		return c == '.' || c == '-' || c == '_' ||
			('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
	}
	address := func(c byte) bool { return label(c) || c == ':' || c == '@' || c == '%' }

	var kept strings.Builder
	rest, previous := source, byte(0)
	for {
		at := strings.Index(rest, name)
		if at < 0 {
			kept.WriteString(rest)
			return kept.String()
		}
		kept.WriteString(rest[:at])
		before := previous
		if at > 0 {
			before = rest[at-1]
		}
		end := at + len(name)
		var after byte
		if end < len(rest) {
			after = rest[end]
		}
		if address(before) || address(after) {
			kept.WriteString(name)
			previous = name[len(name)-1]
		} else {
			previous = before
		}
		rest = rest[end:]
	}
}

// endpointPolicyFinding checks literal endpoint families, not every IP-looking
// number. The exact virtual host is removed only where it stands alone because
// WebResourceRequested intercepts that origin in process; a port, subdomain,
// trailing dot, userinfo or escape turns it back into an address-shaped value.
// netip handles canonical IPv6 while the explicit legacy IPv4 shapes cover the
// spellings netip intentionally rejects.
func endpointPolicyFinding(value string) string {
	virtualHost := "mullion." + "local" + "host"
	lower := stripExemptName(strings.ToLower(value), virtualHost)
	if strings.Contains(lower, virtualHost) {
		return "virtual host outside its intercepted origin"
	}
	if localhostEndpoint(lower) {
		return "loopback host"
	}
	if legacyIPv4Endpoint(lower) {
		return "IPv4 loopback"
	}
	if wildcardIPv4Endpoint(lower) {
		return "wildcard IPv4 endpoint"
	}
	for start := range len(value) {
		if value[start] != '[' {
			continue
		}
		relativeEnd := strings.IndexByte(value[start+1:], ']')
		if relativeEnd < 0 {
			continue
		}
		end := start + relativeEnd + 2
		host := value[start+1 : end-1]
		// IPv4-mapped IPv6 is parsed successfully. IsLoopback already recognizes
		// mapped loopback, but IsUnspecified needs Unmap to expose mapped
		// 0.0.0.0. Unmap before both predicates so the explicit authority rule
		// does not depend on that asymmetric netip behavior.
		address, err := netip.ParseAddr(host)
		if err == nil {
			address = address.Unmap()
		}
		if err == nil && (address.IsLoopback() || address.IsUnspecified()) &&
			(inURLAuthority(value, start) || (!hasURLAuthority(value, start) && standaloneEndpoint(value, start, end))) {
			return "IPv6 loopback or wildcard endpoint"
		}
	}
	if address, err := netip.ParseAddr(value); err == nil && address.IsLoopback() {
		return "loopback or wildcard address"
	}
	return ""
}

func legacyIPv4Endpoint(value string) bool {
	return browserIPv4Endpoint(value, false)
}

func wildcardIPv4Endpoint(value string) bool {
	return browserIPv4Endpoint(value, true)
}

func browserIPv4Endpoint(value string, wildcard bool) bool {
	for start := range len(value) {
		if value[start] < '0' || value[start] > '9' ||
			(start > 0 && endpointTokenByte(value[start-1])) {
			continue
		}
		end, parts, address, ok := browserIPv4End(value, start)
		if !ok || !endpointTokenBoundary(value, start, end) {
			continue
		}
		if wildcard {
			if address != 0 {
				continue
			}
		} else if address>>24 != 127 {
			continue
		}
		if hostStart, hostEnd, url := urlHostBounds(value, start); url {
			if start == hostStart && end == hostEnd {
				return true
			}
			continue
		}
		if wildcard {
			if parts == 4 && standalonePortEndpoint(value, start, end) {
				return true
			}
			continue
		}
		// Preserve the existing prose control for short dotted forms:
		// outside a URL they need a port, while four-part and single-number
		// spellings can stand alone as unambiguous addresses.
		if (parts == 1 || parts == 4) && standaloneEndpoint(value, start, end) ||
			(parts == 2 || parts == 3) && standalonePortEndpoint(value, start, end) {
			return true
		}
	}
	return false
}

// urlAuthorityBounds is candidate-relative because shipped files are scanned as
// whole source strings rather than extracted literals. A scheme-relative marker
// must begin a source token; `//` inside a prior URL path is not a new authority.
func urlAuthorityBounds(value string, before int) (int, int, bool) {
	if before < 0 || before > len(value) {
		return 0, 0, false
	}
	if authorityStart, ok := browserSpecialAuthorityStart(value, before); ok {
		return authorityBounds(value, authorityStart)
	}
	if scheme := strings.LastIndex(value[:before], "://"); scheme >= 0 {
		authorityStart := scheme + 3
		if before < urlTokenEnd(value, authorityStart) {
			return authorityBounds(value, authorityStart)
		}
	}
	for search := before; search > 0; {
		relative := strings.LastIndex(value[:search], "//")
		if relative < 0 {
			break
		}
		if relative == 0 || endpointSourceBoundary(value[relative-1]) {
			authorityStart := relative + 2
			if before < urlTokenEnd(value, authorityStart) {
				return authorityBounds(value, authorityStart)
			}
		}
		search = relative
	}
	return 0, 0, false
}

// browserSpecialAuthorityStart closes the #94 surplus-separator bypass.
// WHATWG special schemes consume a run of slash or backslash separators before
// parsing the authority. Looking only for the first :// leaves http:////127.0.0.1
// positioned after an empty authority even though the browser treats 127.0.0.1
// as the host. Search backward because a password may contain a later colon;
// require an http(s) name at a source-token boundary; require at least two
// separators so unrelated `http:value` prose stays outside this rule; then return
// the first non-separator byte. Generic scheme and scheme-relative handling below
// remain independent and must not be replaced by whole-string URL parsing.
func browserSpecialAuthorityStart(value string, before int) (int, bool) {
	for search := before; search > 0; {
		colon := strings.LastIndexByte(value[:search], ':')
		if colon < 0 {
			return 0, false
		}
		for _, scheme := range []string{"https", "http"} {
			schemeStart := colon - len(scheme)
			if schemeStart < 0 || !strings.EqualFold(value[schemeStart:colon], scheme) ||
				schemeStart > 0 && !endpointSourceBoundary(value[schemeStart-1]) {
				continue
			}
			authorityStart := colon + 1
			for authorityStart < len(value) &&
				(value[authorityStart] == '/' || value[authorityStart] == '\\') {
				authorityStart++
			}
			if authorityStart >= colon+3 && authorityStart <= before &&
				before < urlTokenEnd(value, authorityStart) {
				return authorityStart, true
			}
		}
		search = colon
	}
	return 0, false
}

func authorityBounds(value string, authorityStart int) (int, int, bool) {
	authorityEnd := len(value)
	if relative := strings.IndexAny(value[authorityStart:], "/\\?#"); relative >= 0 {
		authorityEnd = authorityStart + relative
	}
	return authorityStart, authorityEnd, true
}

func urlTokenEnd(value string, start int) int {
	for position := start; position < len(value); position++ {
		if endpointSourceBoundary(value[position]) && value[position] != '[' && value[position] != ']' {
			return position
		}
	}
	return len(value)
}

func urlHostBounds(value string, before int) (int, int, bool) {
	authorityStart, authorityEnd, ok := urlAuthorityBounds(value, before)
	if !ok {
		return 0, 0, false
	}
	hostStart := authorityStart
	if relative := strings.LastIndex(value[authorityStart:authorityEnd], "@"); relative >= 0 {
		hostStart = authorityStart + relative + 1
	}
	hostEnd := authorityEnd
	if hostStart < authorityEnd && value[hostStart] == '[' {
		if relative := strings.IndexByte(value[hostStart:authorityEnd], ']'); relative >= 0 {
			hostEnd = hostStart + relative + 1
		}
	} else if relative := strings.IndexByte(value[hostStart:authorityEnd], ':'); relative >= 0 {
		hostEnd = hostStart + relative
	}
	return hostStart, hostEnd, true
}

// inURLAuthority accepts a numeric token only at the authority host's start,
// after optional userinfo. Scheme-relative network paths are authorities too.
func inURLAuthority(value string, start int) bool {
	hostStart, hostEnd, ok := urlHostBounds(value, start)
	return ok && start == hostStart && start < hostEnd
}

func hasURLAuthority(value string, start int) bool {
	_, _, ok := urlAuthorityBounds(value, start)
	return ok
}

func standaloneEndpoint(value string, start, end int) bool {
	before := start == 0 || endpointSourceBoundary(value[start-1])
	after := end == len(value) || endpointSourceBoundary(value[end]) || value[end] == ':'
	return before && after
}

func standalonePortEndpoint(value string, start, end int) bool {
	return (start == 0 || endpointSourceBoundary(value[start-1])) && end < len(value) && value[end] == ':'
}

func endpointSourceBoundary(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' ||
		value == '"' || value == '\'' || value == '`' || value == '=' ||
		value == '(' || value == ')' || value == '[' || value == ']' ||
		value == '{' || value == '}' || value == ',' || value == ';' ||
		value == '<' || value == '>'
}

func localhostEndpoint(value string) bool {
	const token = "local" + "host"
	for search := 0; search < len(value); {
		relative := strings.Index(value[search:], token)
		if relative < 0 {
			return false
		}
		start, end := search+relative, search+relative+len(token)
		if localhostLabelBoundary(value, start, end) {
			hostStart, hostEnd, url := urlHostBounds(value, start)
			if url {
				if start >= hostStart && (end == hostEnd || (end+1 == hostEnd && value[end] == '.')) {
					return true
				}
			} else {
				bareEnd := bareEndpointEnd(value, end)
				if (start == 0 || value[start-1] == '.' || endpointSourceBoundary(value[start-1])) &&
					(end == bareEnd || (end+1 == bareEnd && value[end] == '.')) {
					return true
				}
			}
		}
		search = start + 1
	}
	return false
}

// bareEndpointEnd is candidate-relative. Shipped files are whole source strings,
// so a colon in an earlier URL must not determine the end of a later quoted host.
func bareEndpointEnd(value string, start int) int {
	for position := start; position < len(value); position++ {
		if strings.ContainsRune(":/?#", rune(value[position])) || endpointSourceBoundary(value[position]) {
			return position
		}
	}
	return len(value)
}

func localhostLabelBoundary(value string, start, end int) bool {
	labelByte := func(char byte) bool {
		return char == '-' || char == '_' ||
			('0' <= char && char <= '9') || ('a' <= char && char <= 'z') || ('A' <= char && char <= 'Z')
	}
	return (start == 0 || !labelByte(value[start-1])) &&
		(end == len(value) || !labelByte(value[end]))
}

func browserIPv4End(value string, start int) (int, int, uint32, bool) {
	var numbers [4]uint64
	position, parts := start, 0
	for {
		if parts == len(numbers) {
			return 0, 0, 0, false
		}
		number, end, ok := guardIPv4Number(value, position)
		if !ok {
			return 0, 0, 0, false
		}
		numbers[parts] = number
		parts++
		position = end
		if position >= len(value) || value[position] != '.' {
			break
		}
		position++
		if position == len(value) || value[position] == ':' || value[position] == '/' ||
			value[position] == '?' || value[position] == '#' || endpointSourceBoundary(value[position]) {
			break
		}
	}
	for index := range parts - 1 {
		if numbers[index] > 255 {
			return 0, 0, 0, false
		}
	}
	lastLimit := uint64(1) << (8 * (5 - parts))
	if numbers[parts-1] >= lastLimit {
		return 0, 0, 0, false
	}
	address := numbers[parts-1]
	for index := range parts - 1 {
		address += numbers[index] << (8 * (3 - index))
	}
	return position, parts, uint32(address), true
}

func guardIPv4Number(value string, start int) (uint64, int, bool) {
	if start >= len(value) || value[start] < '0' || value[start] > '9' {
		return 0, 0, false
	}
	radix, position := uint64(10), start
	if value[position] == '0' && position+1 < len(value) {
		switch value[position+1] {
		case 'x', 'X':
			radix, position = 16, position+2
		default:
			if value[position+1] >= '0' && value[position+1] <= '9' {
				radix, position = 8, position+1
			}
		}
	}
	digits := position
	var number uint64
	for position < len(value) {
		digit, ok := browserIPv4Digit(value[position])
		if !ok {
			break
		}
		if digit >= radix || number > (^uint64(0)-digit)/radix {
			return 0, 0, false
		}
		number = number*radix + digit
		position++
	}
	if position == digits && radix == 16 {
		return 0, position, true
	}
	return number, position, position > digits
}

func browserIPv4Digit(value byte) (uint64, bool) {
	switch {
	case value >= '0' && value <= '9':
		return uint64(value - '0'), true
	case value >= 'a' && value <= 'f':
		return uint64(value-'a') + 10, true
	case value >= 'A' && value <= 'F':
		return uint64(value-'A') + 10, true
	default:
		return 0, false
	}
}

func containsDelimitedEndpointToken(value, token string) bool {
	for search := 0; search < len(value); {
		relative := strings.Index(value[search:], token)
		if relative < 0 {
			return false
		}
		start := search + relative
		if endpointTokenBoundary(value, start, start+len(token)) {
			return true
		}
		search = start + 1
	}
	return false
}

func endpointTokenBoundary(value string, start, end int) bool {
	return (start == 0 || !endpointTokenByte(value[start-1])) &&
		(end == len(value) || !endpointTokenByte(value[end]))
}

func endpointTokenByte(value byte) bool {
	return value == '.' || value == '_' || value == '-' ||
		('0' <= value && value <= '9') || ('a' <= value && value <= 'z') || ('A' <= value && value <= 'Z')
}

// endpointFindingAllowed is rule-specific: it never exempts the API tier. These
// exact files own loopback/source-plan parser fixtures or the error/browser URL
// cases that are meant to exercise a loopback input. A new location must add an
// actual-guard spoof negative, not a basename or all-tests exception.
func endpointFindingAllowed(rel, value string) bool {
	switch rel {
	case "host/loopback.go", "host/loopback_test.go", "host/source_plan_test.go", "host/source_plan_windows_test.go":
		return true
	case "host/architecture_gate_unsupported_windows_test.go":
		return value == "127"+".1"
	case "host/errorpage_test.go", "host/systembrowser_windows_test.go":
		const allowedEndpoint = "[::" + "1]"
		withoutAllowed := strings.ReplaceAll(value, allowedEndpoint, "")
		return withoutAllowed != value && endpointPolicyFinding(withoutAllowed) == ""
	default:
		return false
	}
}
