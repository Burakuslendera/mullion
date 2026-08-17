package logsafe

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// URLLimit bounds the URL payload/result budget. Optional truncation,
// query, and fragment markers may follow according to the established
// contract, so complete output can exceed URLLimit.
const URLLimit = 160

// truncationMarker says the path was cut. A reduced value carrying no marker is
// the whole value.
const truncationMarker = "..."

// URL reduces an http/https URL to the part that identifies a navigation -
// scheme, host and path - and bounds every fallback reduction.
// A URL is not a filesystem path, and the path sanitizer mangles one. isPathStart
// reads a Windows drive letter as <alpha> ':' <separator>, which is what
// "http://" and "https://" both contain - at the 'p' and at the 's' - and reads
// "//" as a UNC start, which every scheme://host URL contains. Either match
// makes the rest of the URL a path span, and FileName reduces a span to its last
// segment, so the host - the one field that says *where* a navigation went - is
// deleted (issue #78):
//
//	messagePlain("https://mullion.local/index.html") == "httpindex.html"
//
// Message no longer does that: it finds the http(s) runs inside a value and
// sends each one here (issue #80, decisions/0028). This function is still the
// right call for a field whose value *is* a URL, because it bounds the whole
// value and refuses to print a host it cannot print in full.
//
// The contract this owes its reader is narrow, and worth stating, because a
// diagnostic that is confidently wrong is worse than one that is obviously
// broken:
//
//   - if a host is printed, it is the WHOLE host, never a prefix of one. A
//     truncated host reads as a different and usually more trustworthy host:
//     cut "evil.example.attacker.test" at the right byte and the log says
//     "evil.example".
//   - if a host is printed, it is hostname-shaped - ASCII letters, digits and
//     ".-_:[]". Anything else sends the whole value to Message, where no host
//     survives at all. A "," or "=" would forge a second "key=value" field in
//     the log line; a zero-width or bidi character makes a foreign host render
//     exactly like the trusted one to whoever is reading.
//   - the query and fragment are dropped, because a token in a query string is
//     the real disclosure risk here. Their presence survives as a bare "?" or
//     "#", read off the raw value, so two navigations differing only there stay
//     distinguishable - which is what issue #77 needs from these lines.
//   - a value cut to fit ends in "..." so a reader can tell that it was.
//
// Everything else falls back to the bounded reduction: a value that is not
// http(s) to begin with, one that does not parse, an authority-less form
// ("http:evil.example", "http:/C:/Users/alice/x"), and any host failing the
// rules above. That fallback is load-bearing. A file: URL's path really is a
// local filesystem path and must still collapse to its file name; the empty
// source still has to reduce to "unknown" and decisions/0021's data: observation
// rests on the reduction it was verified with. All three still hold.
//
// The explicit blob: and filesystem: wrappers retain an inner http(s)
// origin whole while bounding the suffix. Generic fallbacks, including data:,
// are bounded before scanning and may therefore lose an http(s) run interrupted
// by that bound; file: values remain tail-first so their filename survives.
func URL(raw string) string {
	// Gate on the literal scheme, not the parsed one. url.Parse accepts forms
	// carrying no authority at all - "http:evil.example" (opaque) and
	// "http:/C:/Users/alice/x" (empty host, drive-letter path) - and rebuilding
	// those from Host and Path either erases the target entirely or prints a home
	// directory. Neither is a target a browser would hand back, so the old
	// reduction is the right answer for both.
	if !hasHTTPPrefix(raw) {
		return reduceURLFallback(raw, true, false, false)
	}

	head, hasQuery, hasFragment := splitURLMarks(raw)
	reduced, ok := reduceHTTPURL(head, hasQuery, hasFragment)
	if !ok {
		// A fallback still has to avoid cutting a host into a believable prefix,
		// preserve query/fragment presence, and mark any bounded result.
		return reduceURLFallback(head, false, hasQuery, hasFragment)
	}
	return reduced
}

func reduceURLFallback(raw string, scanURLs, hasQuery, hasFragment bool) string {
	if scanURLs {
		// Wrapped web URLs retain their inner origin. Reduce that inner URL
		// directly before bounding the outer wrapper, so a long suffix cannot
		// erase the origin.
		if reduced, ok := reduceOpaqueHTTPURL(raw); ok {
			return reduced
		}
	}

	bounded := raw
	if len(raw) > URLLimit {
		if scanURLs && hasFilePrefix(raw) {
			// File URLs are paths, not opaque web origins. Keep the final
			// component for FileName rather than feeding it a head cut that
			// can expose a partial directory.
			bounded = boundFileFallback(raw)
		} else {
			bounded = boundForScan(raw)
		}
	}
	inputTruncated := len(bounded) < len(raw)
	if scanURLs {
		raw = Message(bounded)
	} else {
		raw = messagePlain(bounded)
	}

	suffixLen := 0
	if hasQuery {
		suffixLen++
	}
	if hasFragment {
		suffixLen++
	}
	truncated := inputTruncated || len(raw) > URLLimit-suffixLen
	if truncated {
		suffixLen += len(truncationMarker)
		budget := URLLimit - suffixLen
		if budget < 0 {
			budget = 0
		}
		raw = strings.TrimSpace(boundForScanAt(raw, budget)) + truncationMarker
	}
	if hasQuery {
		raw += "?"
	}
	if hasFragment {
		raw += "#"
	}
	return raw
}

func reduceOpaqueHTTPURL(raw string) (string, bool) {
	for _, prefix := range []string{"blob:", "filesystem:"} {
		if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
			continue
		}
		inner := raw[len(prefix):]
		if !hasHTTPPrefix(inner) {
			return "", false
		}
		head, hasQuery, hasFragment := splitURLMarks(inner)
		reduced, ok := reduceHTTPURLWithLimit(head, hasQuery, hasFragment, URLLimit-len(prefix))
		if !ok {
			return "", false
		}
		return raw[:len(prefix)] + reduced, true
	}
	return "", false
}

func hasFilePrefix(raw string) bool {
	const prefix = "file:"
	return len(raw) >= len(prefix) && strings.EqualFold(raw[:len(prefix)], prefix)
}

func boundFileFallback(raw string) string {
	if len(raw) <= URLLimit {
		return raw
	}
	end := len(raw)
	for end > 0 && (raw[end-1] == '/' || raw[end-1] == '\\') {
		end--
	}
	if end > 0 {
		if index := strings.LastIndexAny(raw[:end], `/\`); index >= 0 && len(raw)-index <= URLLimit {
			return raw[index:]
		}
	}
	return raw[len(raw)-URLLimit:]
}

// reduceHTTPURL validates the entire identifying part of an http(s) URL and
// emits a bounded projection with the package URL limit.
func reduceHTTPURL(head string, hasQuery, hasFragment bool) (string, bool) {
	return reduceHTTPURLWithLimit(head, hasQuery, hasFragment, URLLimit)
}

// reduceHTTPURLWithLimit is the same projection with room reserved for an
// opaque wrapper such as "blob:" or "filesystem:".
func reduceHTTPURLWithLimit(head string, hasQuery, hasFragment bool, limit int) (string, bool) {
	schemeEnd := len("http://")
	scheme := "http"
	if len(head) >= len("https://") && strings.EqualFold(head[:len("https://")], "https://") {
		schemeEnd = len("https://")
		scheme = "https"
	}
	if len(head) < schemeEnd {
		return "", false
	}

	authorityEnd := len(head)
	if slash := strings.IndexByte(head[schemeEnd:], '/'); slash >= 0 {
		authorityEnd = schemeEnd + slash
	}
	authority := head[schemeEnd:authorityEnd]
	host := authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		if !isValidUserinfo(authority[:at]) {
			return "", false
		}
		host = authority[at+1:]
	}
	if !isHostnameShaped(host) {
		return "", false
	}
	if len(scheme)+len("://")+len(host) > limit {
		return "", false
	}

	path := head[authorityEnd:]
	if !validURLPath(path) {
		return "", false
	}

	// The parser sees only the bounded, credential-free origin. Userinfo and the
	// entire path were validated above and are intentionally never copied.
	// Parsing here locks an accepted authority to the production URL semantics
	// without making credentials or a long path part of an allocation.
	originText := scheme + "://" + host
	parsed, err := url.Parse(originText)
	if err != nil || parsed.User != nil || !isHostnameShaped(parsed.Host) {
		return "", false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if len(origin) > limit {
		return "", false
	}

	var reduced strings.Builder
	reduced.Grow(limit + len(truncationMarker) + 2)
	reduced.WriteString(origin)
	pathBudget := limit - len(origin)
	truncated := appendBoundedEscapedPath(&reduced, path, pathBudget)
	if truncated {
		reduced.WriteString(truncationMarker)
	}
	if hasQuery {
		reduced.WriteByte('?')
	}
	if hasFragment {
		reduced.WriteByte('#')
	}
	result := reduced.String()
	if !isPrintableASCII(result) {
		return "", false
	}
	return result, true
}

// isValidUserinfo mirrors the RFC 3986 userinfo alphabet accepted by net/url.
// Credentials are validated in place and then discarded; even a very large
// password therefore cannot become retained or allocated diagnostic state.
func isValidUserinfo(value string) bool {
	for index := 0; index < len(value); {
		c := value[index]
		if c == '%' {
			if index+2 >= len(value) || !isHexByte(value[index+1]) || !isHexByte(value[index+2]) {
				return false
			}
			index += 3
			continue
		}
		if c >= utf8.RuneSelf {
			return false
		}
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case strings.ContainsRune("-._~!$&'()*+,;=:@", rune(c)):
		default:
			return false
		}
		index++
	}
	return true
}

// validURLPath validates all of path, including bytes beyond the retained
// projection. A malformed late escape must reject the candidate rather than
// letting a valid-looking prefix displace a later complete URL.
func validURLPath(path string) bool {
	for index := 0; index < len(path); index++ {
		c := path[index]
		if c < ' ' || c == 0x7f {
			return false
		}
		if c == '%' {
			if index+2 >= len(path) || !isHexByte(path[index+1]) || !isHexByte(path[index+2]) {
				return false
			}
			index += 2
		}
	}
	return true
}

// appendBoundedEscapedPath writes at most budget escaped bytes and reports
// whether any path unit was omitted. A unit is one raw UTF-8 rune, one %XX
// escape, or a complete run of %XX escapes encoding one UTF-8 rune.
func appendBoundedEscapedPath(out *strings.Builder, path string, budget int) bool {
	for index := 0; index < len(path); {
		end := escapedPathUnitEnd(path, index)
		size := escapedPathUnitSize(path[index:end])
		if size > budget {
			return true
		}
		appendEscapedPathUnit(out, path[index:end])
		budget -= size
		index = end
	}
	return false
}

func escapedPathUnitEnd(path string, index int) int {
	if path[index] == '%' {
		first := unhex(path[index+1])<<4 | unhex(path[index+2])
		width := utf8SequenceLen(first)
		if width <= 1 || index+width*3 > len(path) {
			return index + 3
		}
		var encoded [utf8.UTFMax]byte
		encoded[0] = first
		for offset := 1; offset < width; offset++ {
			next := index + offset*3
			if path[next] != '%' {
				return index + 3
			}
			encoded[offset] = unhex(path[next+1])<<4 | unhex(path[next+2])
		}
		if utf8.Valid(encoded[:width]) {
			return index + width*3
		}
		return index + 3
	}
	_, size := utf8.DecodeRuneInString(path[index:])
	return index + size
}

func utf8SequenceLen(first byte) int {
	switch {
	case first < utf8.RuneSelf:
		return 1
	case first >= 0xc2 && first <= 0xdf:
		return 2
	case first >= 0xe0 && first <= 0xef:
		return 3
	case first >= 0xf0 && first <= 0xf4:
		return 4
	default:
		return 1
	}
}

func escapedPathUnitSize(unit string) int {
	if unit[0] == '%' {
		return len(unit)
	}
	size := 0
	for index := range len(unit) {
		if shouldEscapePathByte(unit[index]) {
			size += 3
		} else {
			size++
		}
	}
	return size
}

func appendEscapedPathUnit(out *strings.Builder, unit string) {
	if unit[0] == '%' {
		out.WriteString(unit)
		return
	}
	const hex = "0123456789ABCDEF"
	for index := range len(unit) {
		c := unit[index]
		if !shouldEscapePathByte(c) {
			out.WriteByte(c)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hex[c>>4])
		out.WriteByte(hex[c&15])
	}
}

func shouldEscapePathByte(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return false
	}
	switch c {
	case '-', '_', '.', '~', '$', '&', '\'', '+', ',', '/', ':', ';', '=', '@':
	default:
		return true
	}
	return false
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// boundInput is the generic bounded-input primitive for fallback scans and
// reducers. It caps oversized input at URLLimit before the caller applies
// its path- or URL-aware reduction.
func boundInput(raw string) string {
	if len(raw) <= URLLimit {
		return raw
	}
	return raw[:URLLimit]
}

// boundForScan bounds a value whose reduction will be scanned for URLs, which
// boundInput alone cannot do safely.
//
// Cut a value at a fixed byte and the cut can land inside a host; Message then
// reads what is left as a whole URL and prints a prefix of that host as an
// origin. Padding chosen by whoever wrote the value puts the cut on a label
// boundary, so "blob:https://cdn.<pad>.mullion.local.evil.example/x" logs as
// "blob:https://cdn.<pad>.mullion.local" - a well-formed lie naming the trusted
// origin, which is issue #78's rejected first attempt arriving through a
// different door (decisions/0025, 0028).
//
// So a cut that interrupts a run takes the whole run with it. What remains is
// shorter and says less, which is the direction this package fails in.
func boundForScan(raw string) string {
	bounded := boundInput(raw)
	if len(bounded) == len(raw) {
		return bounded
	}
	for index := len(bounded) - 1; index >= 0; index-- {
		if isASCIISpace(bounded[index]) {
			break
		}
		if hasHTTPPrefix(bounded[index:]) {
			return bounded[:index]
		}
	}
	return bounded
}

// hasHTTPPrefix reports whether raw literally begins with the http:// or https://
// scheme, matched case-insensitively as url.Parse would.
func hasHTTPPrefix(raw string) bool {
	const httpPrefix = "http://"
	const httpsPrefix = "https://"
	if len(raw) >= len(httpPrefix) && strings.EqualFold(raw[:len(httpPrefix)], httpPrefix) {
		return true
	}
	return len(raw) >= len(httpsPrefix) && strings.EqualFold(raw[:len(httpsPrefix)], httpsPrefix)
}

// splitURLMarks separates the part of raw naming the document from the query and
// fragment, and reports whether each was present.
//
// Presence is read off the raw string rather than the parsed URL for two
// reasons: url.URL has no ForceFragment, so "https://x/p#" is indistinguishable
// from "https://x/p" once parsed, and the flags stay right for a value the parse
// goes on to reject.
func splitURLMarks(raw string) (head string, hasQuery, hasFragment bool) {
	head = raw
	if index := strings.IndexByte(head, '#'); index >= 0 {
		hasFragment = true
		head = head[:index]
	}
	if index := strings.IndexByte(head, '?'); index >= 0 {
		hasQuery = true
		head = head[:index]
	}
	return head, hasQuery, hasFragment
}

// isHostnameShaped reports whether host is safe to print in a log line: a
// non-empty run of ASCII letters, digits, and the punctuation a host, a port or
// an IPv6 literal legitimately needs.
//
// The test is by byte, so every byte above 0x7f is refused, and that is the
// point. Go accepts far more in a host than a hostname can hold: "," and "="
// pass through unescaped, and a zero-width space or bidi override survives
// verbatim. A percent sign is refused too, so an IPv6 zone identifier takes the
// fallback rather than emitting a bare "%".
func isHostnameShaped(host string) bool {
	if host == "" {
		return false
	}
	for index := 0; index < len(host); index++ {
		switch value := host[index]; {
		case value >= 'a' && value <= 'z':
		case value >= 'A' && value <= 'Z':
		case value >= '0' && value <= '9':
		case value == '.' || value == '-' || value == '_':
		case value == ':' || value == '[' || value == ']':
		default:
			return false
		}
	}
	return true
}

// isPrintableASCII reports whether every byte of value is a printable, non-space
// ASCII character, so the value cannot carry a terminal escape, break the line,
// or split into a second whitespace-separated field.
func isPrintableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}
