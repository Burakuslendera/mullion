package logsafe

import (
	"strings"
	"unicode/utf8"
)

func Reason(err error) string {
	if err == nil {
		return "unknown"
	}
	return Message(err.Error())
}

// Message reduces an arbitrary diagnostic string: control bytes folded, Windows
// paths cut to their file name - and any http(s) URL inside it left readable,
// because that is the half of such a message worth having.
//
// The path sanitizer below reads "http://" as a drive letter at the 'p' and
// "//" as a UNC start, so before this every URL embedded in a sentence lost its
// host to FileName (issue #80):
//
//	in   Failed to fetch dynamically imported module: https://mullion.local/app/main.js
//	out  Failed to fetch dynamically imported module: httpmain.js
//
// Swapping the caller to URL does not fix that: url.Parse rejects the whole
// sentence, and URL hands it straight back here. So the scheme has to be known
// one level down, where the message is split around its URL runs.
//
// Two properties keep the widening honest. A message carrying no http(s) scheme
// is reduced as it was before - with one measured exception, that a message
// whose reduction collapsed to nothing now says "unknown" instead of being
// empty - which is what keeps this out of the ~90 call sites decisions/0025 did
// not want to audit. And only those two schemes are spared, so a file: URL's
// path is still a local filesystem path and still collapses to its file name.
func Message(message string) string {
	return reduceAroundURLs(message)
}

// DiagnosticLimit is the maximum number of bytes retained or logged for one
// frontend-controlled diagnostic value. Diagnostic applies the bound before the
// path and URL reducers run, then enforces it again on their output.
const DiagnosticLimit = 2000

const diagnosticInputLimit = DiagnosticLimit - 64

// Diagnostic reduces an untrusted diagnostic value with bounded work and
// bounded output. It keeps the first http(s) URL the production reducer can
// emit, even when a long plain-text prefix would otherwise push that URL beyond
// the input bound. An overlong or malformed candidate is rejected and scanning
// continues; a URL authority is never cut into a believable host prefix.
//
// The clone is intentional. Several diagnostic values live for the lifetime of
// a Host; a short reduction must not retain a large frontend-owned backing
// string.
func Diagnostic(message string) string {
	bounded := boundDiagnosticInput(message)
	reduced := Message(bounded)
	if len(reduced) > DiagnosticLimit {
		// Path reduction can expand a short token: "." becomes "unknown.".
		// Reserve the selected URL's output bytes after reduction, not merely
		// before it, or an expanding prefix can still displace the URL that
		// boundDiagnosticInput deliberately kept (decision 0035).
		if urlStart, urlValue := firstDiagnosticURL(message); urlValue != "" {
			reduced = diagnosticPrefixWithURL(message[:urlStart], urlValue)
		} else {
			reduced = strings.TrimSpace(boundForScanAt(reduced, DiagnosticLimit-len(truncationMarker))) + truncationMarker
		}
	}
	return strings.Clone(reduced)
}

func diagnosticPrefixWithURL(prefix, urlValue string) string {
	plainBudget := DiagnosticLimit - len(urlValue) - 1
	prefix = boundForScanAt(prefix, diagnosticInputLimit)
	plain := strings.TrimSpace(Message(prefix))
	if len(plain) > plainBudget {
		plain = strings.TrimSpace(boundForScanAt(plain, plainBudget-len(truncationMarker))) + truncationMarker
	}
	if plain == "" {
		return urlValue
	}
	return plain + " " + urlValue
}

// DiagnosticFileName bounds a path from the tail before reducing it, because
// the final component is the useful part. The marker makes a cut inside an
// exceptionally long final component visible, and the clone prevents the
// returned name from retaining the input's backing storage.
func DiagnosticFileName(path string) string {
	truncated := len(path) > diagnosticInputLimit
	if truncated {
		start := len(path) - diagnosticInputLimit
		for start < len(path) && !utf8.RuneStart(path[start]) {
			start++
		}
		path = path[start:]
	}
	name := FileName(path)
	if truncated && !strings.ContainsAny(path, `/\`) {
		name = truncationMarker + name
	}
	if len(name) > DiagnosticLimit {
		start := len(name) - (DiagnosticLimit - len(truncationMarker))
		for start < len(name) && !utf8.RuneStart(name[start]) {
			start++
		}
		name = truncationMarker + name[start:]
	}
	return strings.Clone(name)
}

// Field reduces an untrusted value that will occupy one value in Mullion's
// comma-separated key=value diagnostic grammar. It keeps Diagnostic's bound,
// path, URL, and control-byte guarantees, then folds the two grammar delimiters
// so one value cannot mint another field.
func Field(value string) string {
	return foldStructuredField(Diagnostic(value))
}

// FieldFileName is Field for a path-valued structured field. It preserves
// DiagnosticFileName's tail-first privacy and byte-bound behaviour before
// folding the diagnostic grammar delimiters.
func FieldFileName(path string) string {
	return foldStructuredField(DiagnosticFileName(path))
}

func foldStructuredField(value string) string {
	value = strings.ReplaceAll(value, ",", "_")
	value = strings.ReplaceAll(value, "=", "_")
	return strings.Clone(value)
}

// boundDiagnosticInput caps the bytes that reach Message while reserving the
// first meaningful URL. Screening and full-path validation are linear; the
// selected production reduction has fixed-size allocation and output.
func boundDiagnosticInput(message string) string {
	if len(message) <= diagnosticInputLimit {
		return message
	}
	urlStart, urlValue := firstDiagnosticURL(message)
	if urlValue == "" {
		return boundForScanAt(message, diagnosticInputLimit)
	}
	plainLimit := diagnosticInputLimit - len(urlValue)
	if plainLimit > 0 {
		plainLimit--
	}
	if plainLimit > urlStart {
		plainLimit = urlStart
	}
	plain := strings.TrimSpace(boundForScanAt(message[:urlStart], plainLimit))
	if plain == "" {
		return urlValue
	}
	return plain + " " + urlValue
}

// firstDiagnosticURL returns the first run that the production URL reduction
// accepts while keeping a complete host. Cheap malformed shapes allocate
// nothing; every parse and retained candidate is bounded independently of the
// input, and rejected runs cannot displace a later valid URL.
func firstDiagnosticURL(message string) (int, string) {
	for index := 0; index < len(message); {
		offset := strings.IndexAny(message[index:], "hH")
		if offset < 0 {
			return 0, ""
		}
		index += offset
		if !hasHTTPPrefix(message[index:]) {
			index++
			continue
		}
		end := urlRunEnd(message, index)
		candidate, ok := diagnosticURLCandidate(message[index:end], terminatorAt(message, end))
		if !ok {
			index = end
			continue
		}
		return index, candidate
	}
	return 0, ""
}

// diagnosticURLCandidate returns the bounded production reduction of one URL
// run. The complete raw run is validated before any candidate is accepted, so
// an over-budget authority, parser-invalid shape or malformed late path escape
// cannot displace a later meaningful URL. Userinfo is accepted but never copied
// into the returned value.
func diagnosticURLCandidate(run string, terminator byte) (string, bool) {
	if terminator != 0 && terminator != ' ' && !hasCompleteAuthority(run) {
		return "", false
	}
	head, hasQuery, hasFragment := splitURLMarks(run)
	host, ok := diagnosticURLHost(head)
	if !ok || !isDiagnosticAuthorityShaped(host) {
		return "", false
	}
	return reduceHTTPURL(head, hasQuery, hasFragment)
}

func diagnosticURLHost(head string) (string, bool) {
	schemeEnd := len("http://")
	if len(head) >= len("https://") && strings.EqualFold(head[:len("https://")], "https://") {
		schemeEnd = len("https://")
	}
	if len(head) < schemeEnd {
		return "", false
	}
	authorityEnd := len(head)
	if slash := strings.IndexByte(head[schemeEnd:], '/'); slash >= 0 {
		authorityEnd = schemeEnd + slash
	}
	authority := head[schemeEnd:authorityEnd]
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		if !isValidUserinfo(authority[:at]) {
			return "", false
		}
		authority = authority[at+1:]
	}
	return authority, true
}

func isDiagnosticAuthorityShaped(authority string) bool {
	if !isHostnameShaped(authority) {
		return false
	}
	if authority[0] == '[' {
		close := strings.IndexByte(authority, ']')
		if close < 0 || strings.ContainsAny(authority[1:close], "[]") {
			return false
		}
		remainder := authority[close+1:]
		if remainder == "" {
			return true
		}
		if remainder[0] != ':' {
			return false
		}
		return remainder[1:] == "" || isDecimal(remainder[1:])
	}
	if strings.ContainsAny(authority, "[]") || strings.Count(authority, ":") > 1 {
		return false
	}
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		return authority[colon+1:] == "" || isDecimal(authority[colon+1:])
	}
	return true
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func isHexByte(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'f' ||
		value >= 'A' && value <= 'F'
}

// boundForScanAt is the fixed-limit form of boundForScan. If the cut interrupts
// a URL run, it drops that run rather than manufacturing a shortened host.
func boundForScanAt(raw string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(raw) <= limit {
		return raw
	}
	bounded := prefixAtRuneBoundary(raw, limit)
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

func prefixAtRuneBoundary(raw string, limit int) string {
	if len(raw) <= limit {
		return raw
	}
	for limit > 0 && !utf8.RuneStart(raw[limit]) {
		limit--
	}
	return raw[:limit]
}

// messagePlain is Message without the URL protection: the reduction every value
// got before issue #80, and the one URL falls back to.
//
// The direction is one-way where it has to be. Message may call URL. URL's
// fallbacks that keep the http(s) prefix on the value they hand back must call
// this rather than Message - they keep it deliberately, so a URL that fails to
// parse is not reduced as a path, and a Message that scans for http(s) runs
// would send that value straight back to URL for ever. URL's non-http fallback
// may and does call Message, because whatever Message finds inside such a value
// does begin with the scheme and therefore ends here.
func messagePlain(message string) string {
	message = normaliseMessage(message)
	if message == "" {
		return "unknown"
	}
	return reducePlain(message)
}

func normaliseMessage(message string) string {
	message = StripControl(message)
	return strings.TrimSpace(message)
}

func reducePlain(message string) string {
	message = sanitizePathSpans(message)
	parts := strings.Fields(message)
	for index, part := range parts {
		parts[index] = sanitizeToken(part)
	}
	return strings.Join(parts, " ")
}

// reduceAroundURLs splits a message into http(s) runs and everything else, and
// reduces each part by its own rule.
//
// It runs on the raw message, before any control byte has been folded, and that
// order is the whole safety of this function. StripControl turns a control byte
// into a space; if runs were found afterwards, a host with one inside it would
// split, and the part before the fold would be printed as though it were the
// whole host - "https://mullion.local<U+0085>.evil.example/x" logged as
// "https://mullion.local". That is the forgery decisions/0025 spent a round
// eliminating, and folding a control byte is what would manufacture it here, the
// same way folding one to a space would have manufactured a field separator
// there. Finding the run first keeps the control byte inside it, where URL
// refuses the host and the value falls back to the old reduction with no host
// left at all.
//
// For the same reason a run ends only at ASCII whitespace, and reduceRun then
// decides whether the byte that ended it can be trusted. Everything else - C1
// bytes, DEL, anything above 0x7f, and every printable byte including the quotes
// a message wraps a URL in - stays inside the run for URL to adjudicate, and
// URL's own output is printable ASCII by construction.
//
// A run starts at the literal scheme wherever it appears, not only at a word
// boundary, because the messages this exists for quote their URLs: Chrome's CSP
// violation reads "Refused to load the script 'https://cdn.evil.example/x.js'".
// Advancing past a run rather than into it is load-bearing: a URL carrying
// another one in its query ("...?next=https://...") would otherwise have the
// inner scheme matched after plainStart had already passed it, and the slice
// below would panic.
//
// A URL run is separated from the part before it only where the source had a
// separator there. Separating unconditionally would put a space inside every
// quoted URL and inside "blob:https://...", rewriting one token into two; not
// separating where the source had a control byte would weld two URLs into one,
// which reads as the second being a path of the first. A plain part, in
// contrast, is always separated: a run ends only at whitespace, so the part
// after one always begins with it.
func reduceAroundURLs(message string) string {
	var out strings.Builder
	write := func(text string, spaced bool) {
		if text == "" {
			return
		}
		if out.Len() > 0 && spaced {
			out.WriteByte(' ')
		}
		out.WriteString(text)
	}
	plainStart := 0
	for index := 0; index < len(message); {
		// Look for the scheme's first byte rather than testing every position:
		// hasHTTPPrefix is two EqualFold calls and does not inline, which costs
		// an order of magnitude per byte on a message that has no URL in it.
		offset := strings.IndexAny(message[index:], "hH")
		if offset < 0 {
			break
		}
		index += offset
		if !hasHTTPPrefix(message[index:]) {
			index++
			continue
		}
		end := urlRunEnd(message, index)
		plain := message[plainStart:index]
		reduced := reducePlainSegment(plain)
		write(reduced, true)
		write(reduceRun(message[index:end], terminatorAt(message, end)),
			plain != "" && (endsWithASCIISpace(plain) || reduced == ""))
		plainStart = end
		index = end
	}
	write(reducePlainSegment(message[plainStart:]), true)
	if out.Len() == 0 {
		// Reached by an empty message, and by one whose whole reduction
		// collapses to nothing - "// /" does. A log field with nothing after
		// the "=" is worse than one saying it does not know.
		return "unknown"
	}
	return out.String()
}

// reduceRun reduces one http(s) run, unless the byte that ended it makes the
// host untrustworthy.
//
// TAB, LF and CR are deleted outright by a URL parser before it resolves the
// value, so to a browser "https://mullion.local<TAB>.evil.example/x" is one URL
// whose host is mullion.local.evil.example. Ending the run at the tab and
// printing what precedes it prints a prefix of that host as though it were the
// whole of it - decisions/0025's forgery, reached through the run boundary
// instead of through a truncation. The other control bytes in the whitespace
// set behave no better in a terminal, so all of them are refused.
//
// A space is a different thing and is trusted: a space really does end a URL, so
// what precedes it is the whole of the URL the message contained. So is the end
// of the message, where nothing was cut at all - provided the caller did not cut
// it first, which is what boundForScan is for.
//
// The refusal only applies while the authority is still open. Once a path, query
// or fragment has started the host is complete, and a cut after that shortens
// the path, not the host - which keeps a URL inside a multi-line stack trace
// readable, the shape this whole change exists for.
func reduceRun(run string, terminator byte) string {
	if terminator != 0 && terminator != ' ' && !hasCompleteAuthority(run) {
		return messagePlain(run)
	}
	return URL(run)
}

// terminatorAt reports the byte that ended a run, or 0 for the end of the
// message. No run can be ended by a NUL, so 0 is unambiguous.
func terminatorAt(message string, end int) byte {
	if end >= len(message) {
		return 0
	}
	return message[end]
}

// hasCompleteAuthority reports whether a run got past its authority - a path,
// query or fragment has started - so that whatever ended the run cannot have
// shortened the host.
func hasCompleteAuthority(run string) bool {
	const authority = "://"
	index := strings.Index(run, authority)
	if index < 0 {
		return false
	}
	return strings.ContainsAny(run[index+len(authority):], "/?#")
}

// reducePlainSegment is the plain reduction applied to one part of a message.
// Unlike messagePlain it returns "" for an empty part rather than "unknown": a
// message that is nothing but a URL has empty parts on both sides of it, and
// neither of them is a value anyone is missing.
func reducePlainSegment(part string) string {
	return reducePlain(normaliseMessage(part))
}

// urlRunEnd finds where a URL run stops: the next ASCII whitespace byte, or the
// end of the message. See reduceAroundURLs for why nothing else may end one.
func urlRunEnd(message string, start int) int {
	for index := start; index < len(message); index++ {
		if isASCIISpace(message[index]) {
			return index
		}
	}
	return len(message)
}

func isASCIISpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

func endsWithASCIISpace(part string) bool {
	return part != "" && isASCIISpace(part[len(part)-1])
}

// IsControl reports whether r is a C0 or C1 control character, or DEL. It is the
// one definition of "control byte" in the tree: the asset boundary rejects the
// same set at its own edge (host/assets_windows.go), and two copies of a
// character-class rule is how they drift apart.
func IsControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// IsRenderingControl reports issue #115's exact invisible rendering-control set:
// bidi overrides and isolate markers, zero-width format characters,
// word joiner/invisible operators, and BOM. This shared predicate keeps logs
// and asset names from carrying invisible control markers.
func IsRenderingControl(r rune) bool {
	switch {
	case r >= 0x200b && r <= 0x200f:
	case r >= 0x202a && r <= 0x202e:
	case r >= 0x2060 && r <= 0x2064:
	case r >= 0x2066 && r <= 0x2069:
	case r == 0xfeff:
	default:
		return false
	}
	return true
}

// StripControl folds every C0/C1 control character - including CR and LF, ESC,
// BEL, backspace, NUL and the C1 block - plus rendering controls to a space, so
// a string cannot smuggle an ANSI/OSC terminal escape, a title rewrite, an
// injected line or a visually misleading value through to a terminal. Invalid
// UTF-8 bytes use a one-byte marker rather than expanding to U+FFFD; a legitimate
// U+FFFD therefore remains distinguishable and bounded.
func StripControl(message string) string {
	if utf8.ValidString(message) {
		return strings.Map(func(r rune) rune {
			if IsControl(r) || IsRenderingControl(r) {
				return ' '
			}
			return r
		}, message)
	}

	var out strings.Builder
	out.Grow(len(message))
	for index := 0; index < len(message); {
		r, size := utf8.DecodeRuneInString(message[index:])
		if r == utf8.RuneError && size == 1 {
			out.WriteByte('?')
			index++
			continue
		}
		if IsControl(r) || IsRenderingControl(r) {
			out.WriteByte(' ')
		} else {
			out.WriteString(message[index : index+size])
		}
		index += size
	}
	return out.String()
}
