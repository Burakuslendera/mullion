package logsafe

import (
	"strings"
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
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
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

// StripControl folds every C0/C1 control character - including CR and LF, ESC,
// BEL, backspace, NUL and the C1 block - to a space, so a string cannot smuggle
// an ANSI/OSC terminal escape, a title rewrite, an injected line or a
// provenance-erasing backspace through to a terminal. Message calls it after
// handling CR/LF itself; it is exported so another internal package that prints
// untrusted strings to a console - the registry and environment values in
// mullion doctor (issue #40) - can apply the same guard at its own boundary.
func StripControl(message string) string {
	return strings.Map(func(r rune) rune {
		if IsControl(r) {
			return ' '
		}
		return r
	}, message)
}
