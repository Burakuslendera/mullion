package logsafe

import (
	"net/url"
	"strings"
)

// URL reduces an http/https URL to the part that identifies a navigation -
// scheme, host and path - and hands everything else to Message.
//
// A URL is not a filesystem path, and Message's path sanitizer mangles one.
// isPathStart reads a Windows drive letter as <alpha> ':' <separator>, which is
// exactly what "https://" contains at its 's', and it reads "//" as a UNC start,
// which every scheme://host URL contains. Either match makes the rest of the URL
// a path span, and FileName reduces a span to its last segment - so the host,
// the one field that says *where* a navigation went, is deleted (issue #78):
//
//	Message("https://mullion.local/index.html") == "httpindex.html"
//
// That fails safe - it removes more than intended, never less - but it removes
// the identifying half, and the live verifications for issues #6, #68 and #72
// were all read from lines that had already lost their host.
//
// What survives here is deliberate:
//
//   - scheme and host name the origin, which is what the navigation gate, the
//     message-source check and the error surface all reason about;
//   - the path names the document. An http(s) path is a resource on a server,
//     not a folder on this machine, so it carries none of the home directory
//     the path sanitizer exists to remove;
//   - userinfo is dropped, because url.URL keeps credentials out of Host: a
//     password someone put in a URL cannot reach a log line through here;
//   - the query and fragment are dropped, because a token in a query string is
//     the real disclosure risk on this path - and it is what today's sanitizer
//     *preserves* while deleting the host. Their presence is kept as a bare "?"
//     or "#": two navigations differing only in their query would otherwise log
//     identically, and telling those apart is what issue #77 needs.
//
// Anything that is not http/https falls back to Message unchanged: data:,
// blob:, file:, about:blank, "", and any value that does not parse. That keeps
// the reduction fail-safe in both directions. A file: URL's path really is a
// local filesystem path and must still collapse to its file name, and a data:
// URI keeps the verbatim reporting decisions/0021 was verified against.
//
// Callers bound the input first (Host.clampSourceForLog): a foreign data: or
// blob: URI is arbitrarily long, and the fallback would otherwise log all of it.
func URL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Message(raw)
	}

	var builder strings.Builder
	builder.WriteString(parsed.Scheme)
	builder.WriteString("://")
	builder.WriteString(parsed.Host)
	builder.WriteString(parsed.EscapedPath())
	// ForceQuery is the "https://x/p?" form, where the query is empty but present.
	if parsed.ForceQuery || parsed.RawQuery != "" {
		builder.WriteByte('?')
	}
	if parsed.Fragment != "" {
		builder.WriteByte('#')
	}
	return neutralizeForLog(builder.String())
}

// neutralizeForLog applies to a reassembled URL the two guards Message applies
// to everything else: no control character reaches a terminal, and the value
// stays on one line as a single run of non-space text.
//
// Neither is theoretical. url.Parse rejects C0 bytes outright and EscapedPath
// percent-encodes what it re-emits, but a C1 byte (U+0080 to U+009F) passes
// straight through host parsing into URL.Host - a NEL byte between two host
// labels survives verbatim - and StripControl then folds it to a space, which
// is why the whitespace collapse follows rather than precedes it.
func neutralizeForLog(value string) string {
	fields := strings.Fields(StripControl(value))
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.Join(fields, " ")
}
