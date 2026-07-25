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
// Two properties keep the widening honest. A message carrying no literal
// http:// or https:// is reduced byte for byte as it was before - which is what
// keeps this out of the ~90 call sites decisions/0025 did not want to audit -
// and only those two schemes are spared, so a file: URL's path is still a local
// filesystem path and still collapses to its file name.
func Message(message string) string {
	return reduceAroundURLs(message)
}

// messagePlain is Message without the URL protection: the reduction every value
// got before issue #80, and the one URL falls back to.
//
// The direction is one-way and load-bearing: Message may call URL, URL must
// never call Message. URL's fallbacks keep the http(s) prefix on the value they
// hand back - deliberately, so a URL that fails to parse is not reduced as a
// path - and handing that to a Message that scans for http(s) runs would send it
// straight back to URL for ever. A caller holding a sentence rather than a URL
// should call Message, which is where the scanning lives.
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
// For the same reason a run ends only at ASCII whitespace. Everything else -
// C1 bytes, DEL, anything above 0x7f - stays inside the run for URL to
// adjudicate, and URL's own output is printable ASCII by construction.
//
// A run starts at the literal scheme wherever it appears, not only at a word
// boundary, because the messages this exists for quote their URLs: Chrome's CSP
// violation reads "Refused to load the script 'https://cdn.evil.example/x.js'".
//
// A part is separated from the one before it only where the source had
// whitespace there. Separating unconditionally would put a space inside every
// quoted URL and inside "blob:https://..." - rewriting one token into two, which
// is a worse misreading than the one this is fixing.
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
	for index := 0; index < len(message); index++ {
		if !hasHTTPPrefix(message[index:]) {
			continue
		}
		end := urlRunEnd(message, index)
		plain := message[plainStart:index]
		write(reducePlainSegment(plain), startsWithASCIISpace(plain))
		write(URL(message[index:end]), endsWithASCIISpace(plain))
		plainStart = end
		index = end - 1
	}
	write(reducePlainSegment(message[plainStart:]), startsWithASCIISpace(message[plainStart:]))
	if out.Len() == 0 {
		return "unknown"
	}
	return out.String()
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

func startsWithASCIISpace(part string) bool {
	return part != "" && isASCIISpace(part[0])
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
