package logsafe

import (
	"strings"
	"testing"
)

// What may be printed as a host, and what that guarantees about the rest of the
// reduced value. Split from url_test.go, which owns the reduction contract
// itself: this file is one concern - the character rules the host must satisfy
// before any part of a URL is reassembled into a log line.

// If a host is printed it is hostname-shaped, and anything else takes the
// fallback where no host survives at all. Two distinct harms sit behind this:
//
//   - "," and "=" are legal in a Go host and would forge a second "key=value"
//     field in a log line whose format is exactly that;
//   - a zero-width or bidi character makes a foreign host render like the
//     trusted one to whoever is reading the log, which is the whole audience
//     for these lines.
//
// Folding those bytes to a space is not good enough: a space is the field
// separator, so the neutraliser would be manufacturing the injection it exists
// to prevent. They send the value to Message instead.
func TestURLRefusesHostsThatAreNotHostnameShaped(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
	}{
		{"comma and equals forge a field", "https://evil.example,user_initiated=true/p"},
		{"c1 nel inside host", "https://exa\u0085mple.com/p"},
		{"c1 non-space inside host", "https://exa\u0086mple.com/p"},
		{"zero width space", "https://mullion\u200b.local/index.html"},
		{"bidi override", "https://ev\u202eil.example/p"},
		{"soft hyphen", "https://mullion\u00ad.local/p"},
		{"cyrillic homograph", "https://\u0435vil.example/p"},
		{"space via percent escape", "https://evil.example,%C2%A0approved=yes/p"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := URL(c.in)
			if want := Message(c.in); got != want {
				t.Fatalf("URL(%q) = %q, want the Message() fallback %q", c.in, got, want)
			}
			if strings.HasPrefix(got, "http://") || strings.HasPrefix(got, "https://") {
				t.Fatalf("URL(%q) = %q, want no reassembled URL for a malformed host", c.in, got)
			}
		})
	}
}

// isHostnameShaped is tested directly, not only through URL, because two guards
// overlap on the way out: a non-ASCII host is refused here AND would be caught
// by the printable-ASCII check on the reassembled value. Going through URL alone
// therefore cannot tell which one is doing the work, and a mutant that widens
// this gate to admit every byte above 0x7f survives the behavioural test. That
// is exactly the shape of a proof that has quietly stopped proving anything, so
// the gate gets its own lock.
func TestIsHostnameShaped(t *testing.T) {
	for _, host := range []string{
		"mullion.local",
		"example.com",
		"example.com:8443",
		"[2001:db8::1]:8443",
		"203.0.113.7",        // a documentation-range literal; this package may not name a loopback host
		"xn--80ak6aa92e.com", // punycode is already ASCII by the time it is a host
		"host_with_underscore",
		"UPPER.Example.COM",
	} {
		if !isHostnameShaped(host) {
			t.Errorf("isHostnameShaped(%q) = false, want true", host)
		}
	}

	for _, host := range []string{
		"",                            // no host at all
		"evil.example,user_init=true", // forges a second log field
		"evil.example=1",              // ditto
		"mullion\u200b.local",         // zero width space: renders like the trusted origin
		"ev\u202eil.example",          // bidi override
		"exa\u0085mple.com",           // C1 that Go treats as whitespace
		"exa\u0086mple.com",           // C1 that Go does not
		"\u0435vil.example",           // cyrillic homograph
		"mullion.local\ufeff",         // BOM
		"host with space",             // would split the log field
		"[fe80::1%eth0]",              // percent: refused rather than emitting a bare %
		"evil.example\"onclick=\"x",   // quote
	} {
		if isHostnameShaped(host) {
			t.Errorf("isHostnameShaped(%q) = true, want false", host)
		}
	}
}

// No reduced value carries a control byte, on either branch, by two different
// mechanisms: the URL branch refuses a host that is not printable ASCII and
// percent-encodes the path, the fallback goes through Message, which strips.
//
// The stronger no-whitespace guarantee holds on the URL branch only. Message
// folds a control byte to a space and joins on spaces - long-standing behaviour
// this change deliberately does not touch, because the non-http(s) reduction has
// to stay byte-identical (issue #56's ":unknown", decisions/0021's data: form).
// So a value that reaches the fallback may contain a space; a reassembled URL
// may not, which is what keeps a host from forging a second log field.
func TestURLOutputCarriesNoControlBytes(t *testing.T) {
	for _, in := range []string{
		"https://example.com/a\x1b[2Jb",
		"https://example.com/a\nWARN forged",
		"https://example.com/\u0090payload",
		"https://example.com/a\x7fb",
		"https://exa\u0086mple.com/p",
		"https://example.com/p#\nWARN forged",
		"https://example.com/a b",
		"data:text/html,<b>\x1b[2J</b>",
		"https://evil.example,\u0085user_initiated=false/p",
	} {
		got := URL(in)
		for _, r := range got {
			if IsControl(r) {
				t.Errorf("URL(%q) = %q carries control rune %#x", in, got, r)
			}
		}
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("URL(%q) = %q spans more than one line", in, got)
		}
		// A reassembled URL is held to the stronger rule.
		if strings.HasPrefix(got, "http://") || strings.HasPrefix(got, "https://") {
			if strings.ContainsAny(got, " \t") {
				t.Errorf("URL(%q) = %q reassembled a URL containing whitespace, which can forge a log field", in, got)
			}
		}
	}
}
