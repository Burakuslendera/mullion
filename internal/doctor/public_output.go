package doctor

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// publicReportWriter is the only string-output boundary for a doctor report.
// Every value is control-folded, stripped of a known home path wherever it
// appears, and stripped of any remaining UNC host before it reaches the block
// intended for a public issue.
type publicReportWriter struct {
	out   strings.Builder
	homes []string
}

func (writer *publicReportWriter) field(label, value string) {
	fmt.Fprintf(&writer.out, "%-11s%s\n", label+":", sanitizePublicValue(value, writer.homes))
}

func (writer *publicReportWriter) note(value string) {
	fmt.Fprintf(&writer.out, "%-11s%s\n", "", sanitizePublicValue(value, writer.homes))
}

// redactHome retains the focused helper used by the path tests. Format does not
// call it field by field: publicReportWriter applies the same treatment to every
// printable string, including paths embedded in build or error text.
func redactHome(value string, homes []string) string {
	return sanitizePublicValue(value, homes)
}

// The order is privacy-significant. Control folding protects the terminal.
// Known-home replacement must precede general UNC collapse so a home located on
// a network profile becomes %USERPROFILE% rather than merely losing its server.
// Only after that may remaining UNC machine names become <host>.
func sanitizePublicValue(value string, homes []string) string {
	value = logsafe.StripControl(value)
	value = redactKnownHomes(value, homes)
	return collapseUNCHosts(value)
}

// redactKnownHomes scans the complete printable value, not only byte offset zero.
// Build replacement metadata and future path-bearing errors therefore receive the
// same treatment as fields whose current source is a path. Canonical homes are
// computed once; the output builder remains unallocated on the common no-match
// path and starts only when a replacement is actually required.
func redactKnownHomes(value string, homes []string) string {
	canonicalHomes := make([]string, 0, len(homes))
	for _, home := range homes {
		if canonical := canonicalHome(home); canonical != "" {
			canonicalHomes = append(canonicalHomes, canonical)
		}
	}
	if len(canonicalHomes) == 0 {
		return value
	}

	var out strings.Builder
	last := 0
	for start := 0; start < len(value); {
		matchEnd := -1
		for _, home := range canonicalHomes {
			if end, ok := matchHomeAt(value, start, home); ok && end > matchEnd {
				matchEnd = end
			}
		}
		if matchEnd < 0 {
			start++
			continue
		}
		if out.Cap() == 0 {
			out.Grow(len(value))
		}
		out.WriteString(value[last:start])
		out.WriteString("%USERPROFILE%")
		last = matchEnd
		start = matchEnd
	}
	if out.Cap() == 0 {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}

// canonicalHome changes only a comparison key. It never cleans the displayed
// value: filepath.Clean would be GOOS-dependent and would destroy the spelling
// that makes a diagnostic useful. Extended drive and UNC prefixes are mapped to
// their ordinary comparison forms while the original bytes remain untouched.
func canonicalHome(home string) string {
	home = strings.TrimRight(home, `\/`)
	if home == "" {
		return ""
	}
	home = strings.ReplaceAll(home, "/", `\`)
	switch {
	case hasPrefixFold(home, `\\?\UNC\`):
		return `\\` + home[len(`\\?\UNC\`):]
	case hasPrefixFold(home, `\\?\`) && len(home) >= len(`\\?\C:`) && home[len(`\\?\`)+1] == ':':
		return home[len(`\\?\`):]
	default:
		return home
	}
}

// matchHomeAt compares original bytes so its end offset supports lossless
// replacement. Slash variants and Unicode simple folds cover Windows spellings.
// The suffix check distinguishes exact-home prose from sibling path components
// such as "Alice 2" and "Alice.Backup"; extended prefixes are consumed as part
// of the replaced prefix instead of leaving a corrupted host/drive combination.
func matchHomeAt(value string, start int, home string) (int, bool) {
	valueAt, homeAt := start, 0
	switch {
	case extendedUNCAt(value, start):
		if !strings.HasPrefix(home, `\\`) {
			return 0, false
		}
		valueAt += len(`\\?\UNC\`)
		homeAt = 2
	case extendedPathAt(value, start):
		valueAt += len(`\\?\`)
	}

	for homeAt < len(home) {
		if valueAt >= len(value) {
			return 0, false
		}
		if isSep(value[valueAt]) || isSep(home[homeAt]) {
			if !isSep(value[valueAt]) || !isSep(home[homeAt]) {
				return 0, false
			}
			valueAt++
			homeAt++
			continue
		}
		valueRune, valueSize := utf8.DecodeRuneInString(value[valueAt:])
		homeRune, homeSize := utf8.DecodeRuneInString(home[homeAt:])
		if (valueRune == utf8.RuneError && valueSize == 1) || (homeRune == utf8.RuneError && homeSize == 1) {
			if value[valueAt] != home[homeAt] {
				return 0, false
			}
			valueSize, homeSize = 1, 1
		} else if !equalFoldRune(valueRune, homeRune) {
			return 0, false
		}
		valueAt += valueSize
		homeAt += homeSize
	}
	if valueAt < len(value) && !homeSuffixBoundary(value, valueAt) {
		return 0, false
	}
	return valueAt, true
}

func extendedUNCAt(value string, start int) bool {
	const prefixLength = len(`\\?\UNC\`)
	return start+prefixLength <= len(value) &&
		isSep(value[start]) && isSep(value[start+1]) && value[start+2] == '?' && isSep(value[start+3]) &&
		strings.EqualFold(value[start+4:start+7], "UNC") && isSep(value[start+7])
}

func extendedPathAt(value string, start int) bool {
	const prefixLength = len(`\\?\`)
	return start+prefixLength <= len(value) &&
		isSep(value[start]) && isSep(value[start+1]) && value[start+2] == '?' && isSep(value[start+3])
}

func equalFoldRune(left, right rune) bool {
	if left == right {
		return true
	}
	for folded := unicode.SimpleFold(left); folded != left; folded = unicode.SimpleFold(folded) {
		if folded == right {
			return true
		}
	}
	return false
}

func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

// collapseUNCHosts is a second lazy pass over home-redacted output. It preserves
// prefix/share spelling, terminal prose punctuation and complete HTTP(S) URL
// spans. URL spans are consumed once in the same forward pass, keeping the scan
// linear without allocating metadata. A pre-existing <host> remains idempotent.
func collapseUNCHosts(value string) string {
	var out strings.Builder
	last := 0
	for start := 0; start < len(value); {
		if urlEnd, ok := httpURLSpanAt(value, start); ok {
			start = urlEnd
			continue
		}
		hostStart, hostEnd, ok := uncHostBounds(value, start)
		if !ok {
			start++
			continue
		}
		if value[hostStart:hostEnd] == "<host>" {
			start = hostEnd
			continue
		}
		if out.Cap() == 0 {
			out.Grow(len(value))
		}
		out.WriteString(value[last:hostStart])
		out.WriteString("<host>")
		last = hostEnd
		start = hostEnd
	}
	if out.Cap() == 0 {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}

// uncHostBounds is the #107 paste-ready boundary for ordinary and extended UNC.
// It rejects URL separator runs, extended drive paths and device paths. The late
// bypass was `\\BUILD-NAS\share` wrapped in Markdown backticks: one form failed
// the opening-token check and a byte-zero form consumed its closing wrapper.
// Host tokenization therefore stops before path separators, whitespace and every
// display wrapper recognized below; only the machine is replaced, while prefix,
// share, suffix, punctuation and wrapper remain byte-for-byte.
func uncHostBounds(value string, start int) (int, int, bool) {
	if start+2 > len(value) || !isSep(value[start]) || !isSep(value[start+1]) {
		return 0, 0, false
	}
	if start > 0 && !uncAuthorityBoundary(value[start-1]) {
		return 0, 0, false
	}
	hostStart := start + 2
	if hostStart+2 <= len(value) && (value[hostStart] == '?' || value[hostStart] == '.') && isSep(value[hostStart+1]) {
		if value[hostStart] != '?' || hostStart+6 > len(value) ||
			!strings.EqualFold(value[hostStart+2:hostStart+5], "UNC") || !isSep(value[hostStart+5]) {
			return 0, 0, false
		}
		hostStart += 6
	}
	hostEnd := hostStart
	for hostEnd < len(value) && uncHostByte(value[hostEnd]) {
		if value[hostEnd] == '.' && uncHostDotTerminates(value, hostEnd+1) {
			break
		}
		hostEnd++
	}
	if hostEnd == hostStart {
		return 0, 0, false
	}
	return hostStart, hostEnd, true
}

// A UNC authority starts a source/display token. Merely seeing two separators
// after an ordinary path component is insufficient: caller-supplied paths can
// retain redundant separators even when the target does not exist. Backticks
// delimit paste-ready code just like quotes. Removing the backtick from this
// opening set makes the wrapped #107 fixture leak BUILD-NAS while all earlier
// angle/quote cases stay green, which is why it is named rather than generalized.
func uncAuthorityBoundary(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' ||
		value == '(' || value == '[' || value == '{' || value == '"' ||
		value == '\'' || value == '`' || value == '=' || value == ',' || value == ';' ||
		value == '<' || value == '>'
}

// httpURLSpanAt recognizes a URL at its scheme and returns its complete display
// token. In addition to prose boundaries, '=' admits key=value fields and '['
// preserves the prior bracket-wrapped spelling. Square brackets are deliberately
// not URL terminators: bracketed IPv6 authorities may be followed by paths and
// queries whose doubled separators must remain URL text.
func httpURLSpanAt(value string, start int) (int, bool) {
	if start >= len(value) || (value[start] != 'h' && value[start] != 'H') {
		return 0, false
	}
	if start > 0 && value[start-1] != '=' && value[start-1] != '[' &&
		!httpURLTextBoundary(value[start-1]) {
		return 0, false
	}

	schemeEnd := 0
	switch {
	case start+len("https://") <= len(value) && strings.EqualFold(value[start:start+len("https://")], "https://"):
		schemeEnd = start + len("https://")
	case start+len("http://") <= len(value) && strings.EqualFold(value[start:start+len("http://")], "http://"):
		schemeEnd = start + len("http://")
	default:
		return 0, false
	}
	end := schemeEnd
	for end < len(value) && !httpURLTextBoundary(value[end]) {
		end++
	}
	return end, true
}

func httpURLTextBoundary(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' ||
		value == '(' || value == ')' || value == '{' || value == '}' ||
		value == '"' || value == '\'' || value == '`' || value == '<' || value == '>'
}

func urlTextBoundary(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' ||
		value == '(' || value == ')' || value == '[' || value == ']' ||
		value == '{' || value == '}' || value == '"' || value == '\'' ||
		value == '`' || value == '<' || value == '>'
}

func uncHostDotTerminates(value string, after int) bool {
	return after == len(value) || urlTextBoundary(value[after]) ||
		value[after] == ',' || value[after] == ';' || value[after] == ':' ||
		value[after] == '!' || value[after] == '?'
}

// Keep this terminator set aligned with uncAuthorityBoundary and URL text
// boundaries. A wrapper admitted only at the start can otherwise be absorbed
// into the host at byte zero and disappear when the host becomes <host>.
func uncHostByte(value byte) bool {
	return !isSep(value) && value != ' ' && value != '\t' && value != '\n' && value != '\r' &&
		value != '(' && value != ')' && value != '[' && value != ']' && value != '{' && value != '}' &&
		value != '<' && value != '>' && value != '"' && value != '\'' && value != '`' &&
		value != ',' && value != ';' && value != ':' && value != '!' && value != '?'
}

func isSep(value byte) bool {
	return value == '\\' || value == '/'
}

// A punctuation byte alone is not enough to prove a prose boundary: Windows
// permits spaces and periods inside sibling component names. Scan the adjacent
// token for a separator; a terminal space-prefixed token stays a sibling, while
// later whitespace or an explicit drive token marks prose.
func homeSuffixBoundary(value string, start int) bool {
	if start >= len(value) || isSep(value[start]) {
		return true
	}
	if !homeProseBoundary(value[start]) {
		return false
	}
	if value[start] == '"' || value[start] == ':' || value[start] == '?' {
		return true
	}
	suffixStart := start
	for start < len(value) && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	hadLeadingSpace := start > suffixStart
	if hadLeadingSpace && start < len(value) &&
		(value[start] == '(' || value[start] == '[' || value[start] == '{' ||
			value[start] == '<' || value[start] == '"' || value[start] == '\'') {
		return true
	}
	dotSibling := !hadLeadingSpace && start+1 < len(value) && value[start] == '.' &&
		value[start+1] != ' ' && value[start+1] != '\t' &&
		value[start+1] != '\n' && value[start+1] != '\r'
	sawDriveColon := false
	for position := start; position < len(value); position++ {
		if isSep(value[position]) {
			return sawDriveColon
		}
		if value[position] == ':' {
			sawDriveColon = true
		}
		if value[position] == '\n' || value[position] == '\r' {
			return true
		}
	}
	return !dotSibling && !hadLeadingSpace
}

func homeProseBoundary(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'', ',', '.', ';', ':', '!', '?':
		return true
	default:
		return false
	}
}
