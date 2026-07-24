package logsafe

import (
	"net/url"
	"strings"
)

// URLLimit bounds a reduced URL. It is applied to the *result*, never to the
// input: bounding a URL before parsing it yields a shorter URL that is still
// well formed, and a well-formed lie is worse than visible garbage. See URL.
const URLLimit = 160

// truncationMarker says the path was cut. A reduced value carrying no marker is
// the whole value.
const truncationMarker = "..."

// URL reduces an http/https URL to the part that identifies a navigation -
// scheme, host and path - and hands everything else to Message.
//
// A URL is not a filesystem path, and Message's path sanitizer mangles one.
// isPathStart reads a Windows drive letter as <alpha> ':' <separator>, which is
// what "http://" and "https://" both contain - at the 'p' and at the 's' - and
// reads "//" as a UNC start, which every scheme://host URL contains. Either
// match makes the rest of the URL a path span, and FileName reduces a span to
// its last segment, so the host - the one field that says *where* a navigation
// went - is deleted (issue #78):
//
//	Message("https://mullion.local/index.html") == "httpindex.html"
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
// Everything else falls back to Message: a value that is not http(s) to begin
// with, one that does not parse, an authority-less form ("http:evil.example",
// "http:/C:/Users/alice/x"), and any host failing the rules above. That
// fallback is load-bearing in both directions. A file: URL's path really is a
// local filesystem path and must still collapse to its file name, and the
// non-http(s) reduction must not move at all: the empty source still has to
// reduce to ":unknown" through urlOrigin - the value issue #56's live probe was
// read against - and decisions/0021's data: observation rests on the reduction
// it was verified with.
func URL(raw string) string {
	// Gate on the literal scheme, not the parsed one. url.Parse accepts forms
	// carrying no authority at all - "http:evil.example" (opaque) and
	// "http:/C:/Users/alice/x" (empty host, drive-letter path) - and rebuilding
	// those from Host and Path either erases the target entirely or prints a home
	// directory. Neither is a target a browser would hand back, so the old
	// reduction is the right answer for both. The prefix test also skips
	// url.Parse for the common non-URL case, which is every fallback caller.
	if !hasHTTPPrefix(raw) {
		return Message(boundInput(raw))
	}

	// From here the fallback reduces head, never raw. An http(s) value that fails
	// to parse is still an http(s) value: handing the whole thing back to Message
	// reproduces issue #78 in its worst form - the host deleted and the query,
	// where a token would be, kept intact. A lone "%" is enough to get here
	// ("https://host/50%off/p?token=..."), and so is the runtime handing back
	// anything url.Parse rejects, so this path is not exotic.
	head, hasQuery, hasFragment := splitURLMarks(raw)
	parsed, err := url.Parse(head)
	if err != nil || !isHostnameShaped(parsed.Host) {
		return Message(boundInput(head))
	}

	origin := parsed.Scheme + "://" + parsed.Host
	// A host that cannot fit the budget is not printed at all rather than cut,
	// because cutting it is exactly the failure this bound exists to prevent.
	if len(origin) > URLLimit {
		return Message(boundInput(head))
	}

	reduced := origin + parsed.EscapedPath()
	if len(reduced) > URLLimit {
		reduced = reduced[:URLLimit] + truncationMarker
	}
	if hasQuery {
		reduced += "?"
	}
	if hasFragment {
		reduced += "#"
	}

	// Belt and braces, and unreachable today: the scheme is one of two literals,
	// isHostnameShaped has already held the host to printable ASCII, and
	// EscapedPath percent-encodes every control, space and non-ASCII byte it
	// re-emits. Kept because that last one is a standard library behaviour this
	// package does not control, and a control byte reaching a terminal is the one
	// failure worth a redundant check. Nothing reaches this branch, so do not go
	// hunting for a test that covers it - the property is locked at
	// isHostnameShaped, which is where it can actually be exercised.
	if !isPrintableASCII(reduced) {
		return Message(boundInput(head))
	}
	return reduced
}

// boundInput bounds the fallback only, where the value goes to Message as-is: a
// foreign data: or blob: URI is arbitrarily long and Message would otherwise
// reduce, and log, all of it. Cutting the input is safe here in the way it is
// not on the URL branch, because Message deletes the identifying part of the
// value anyway - there is no host left for a reader to misread.
func boundInput(raw string) string {
	if len(raw) <= URLLimit {
		return raw
	}
	return raw[:URLLimit]
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
