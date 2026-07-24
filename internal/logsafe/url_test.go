package logsafe

import (
	"strings"
	"testing"
)

// The bug URL exists to fix (issue #78). These are the two lines read verbatim
// off a live examples/basic run, and Message's path sanitizer deletes the host
// from both. The assertions are pinned to Message, not to URL: they document the
// behaviour that made URL necessary, and they fail the day someone "fixes"
// Message itself, at which point this file and its call sites want revisiting
// rather than silently keeping a redundant wrapper.
func TestMessageStillManglesURLsWhichIsWhyURLExists(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://mullion.local/index.html?in=1", "httpindex.html?in=1"},
		{"https://example.com/", "httpexample.com"},
	} {
		if got := Message(c.in); got != c.want {
			t.Fatalf("Message(%q) = %q, want %q - if this changed, re-check whether URL is still needed", c.in, got, c.want)
		}
	}
}

// The heart of the fix: an http/https URL keeps the scheme, host and path that
// say where a navigation went, and loses the query and fragment that are where a
// token would sit. Presence of a query or fragment survives as a bare marker so
// two navigations differing only there stay distinguishable in a live log.
func TestURLKeepsOriginAndPath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://mullion.local/index.html?in=1", "https://mullion.local/index.html?"},
		{"https://mullion.local/index.html", "https://mullion.local/index.html"},
		{"https://example.com/", "https://example.com/"},
		{"http://example.com/", "http://example.com/"},
		{"https://example.com", "https://example.com"},
		{"https://example.com:8443/deep/path/page.html", "https://example.com:8443/deep/path/page.html"},
		{"https://[2001:db8::1]:8443/x", "https://[2001:db8::1]:8443/x"},
		{"https://example.com/p#section", "https://example.com/p#"},
		{"https://example.com/p?", "https://example.com/p?"},
		// url.Parse lower-cases the scheme; the host is logged as the runtime
		// reported it, because that is what a live reader is comparing against.
		{"HTTPS://EXAMPLE.COM", "https://EXAMPLE.COM"},
	} {
		if got := URL(c.in); got != c.want {
			t.Errorf("URL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The query is dropped, not reduced: a token a caller put in a query string is
// the real disclosure risk on this path, and Message currently *preserves* it
// while deleting the host. Credentials in userinfo go the same way - url.URL
// keeps them out of Host, so reassembling from Host cannot re-emit them.
func TestURLDropsQueryAndCredentials(t *testing.T) {
	got := URL("https://user:hunter2@example.com/a/b?token=s3cr3t&next=%2Fadmin#tail")
	for _, forbidden := range []string{"hunter2", "user", "s3cr3t", "token", "next", "admin", "tail"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("URL() leaked %q in %q", forbidden, got)
		}
	}
	if got != "https://example.com/a/b?#" {
		t.Fatalf("URL() = %q, want %q", got, "https://example.com/a/b?#")
	}
}

// Everything that is not http/https falls back to Message verbatim. Three of
// these carry a contract of their own:
//
//   - file: - its path IS a local filesystem path, so it must still collapse to
//     a file name. Routing it through the URL branch would leak a home directory,
//     which is the regression this fallback exists to prevent.
//   - data: - decisions/0021 was verified against Message reporting a data: URI
//     in full. That observation must keep holding.
//   - a value that does not parse, and "" - no worse than before.
func TestURLFallsBackToMessageForNonHTTP(t *testing.T) {
	for _, in := range []string{
		`file:///C:/Users/Alice O'Brien/AppData/secret.html`,
		"data:text/html,<b>hi</b>",
		"blob:https://evil.example/uuid",
		"about:blank",
		"://noscheme",
		"",
		"   ",
		`\\server\share\O'Brien\rollout.jsonl`,
	} {
		if got, want := URL(in), Message(in); got != want {
			t.Errorf("URL(%q) = %q, want the Message() reduction %q", in, got, want)
		}
	}

	// Spelled out for the one that matters most: no user or folder segment of a
	// file: path may survive.
	got := URL(`file:///C:/Users/Alice O'Brien/AppData/secret.html`)
	if !strings.Contains(got, "secret.html") {
		t.Fatalf("URL(file:) = %q, want the file name retained", got)
	}
	for _, forbidden := range []string{"Alice", "O'Brien", "AppData", "Users"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("URL(file:) leaked %q in %q", forbidden, got)
		}
	}
}

// The log-safety contract does not weaken on the new branch. A C0 byte makes
// url.Parse fail, so that input reaches Message and is stripped there; a C1 byte
// survives host parsing verbatim and is stripped by URL itself. Both must come
// out inert, on one line, either way.
func TestURLStripsControlBytesAndStaysOnOneLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"c0 in path fails to parse", "https://example.com/a\x1b[2Jb"},
		{"c0 newline forges a line", "https://example.com/a\nWARN forged"},
		{"c1 nel inside host", "https://exa\u0085mple.com/p"},
		{"c1 block inside path", "https://example.com/\u0090payload"},
		{"del byte", "https://example.com/a\x7fb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := URL(c.in)
			for _, r := range got {
				if IsControl(r) {
					t.Fatalf("URL(%q) = %q still carries control rune %#x", c.in, got, r)
				}
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Fatalf("URL(%q) = %q spans more than one line", c.in, got)
			}
		})
	}
}

// A URL that survives the http/https branch must not be able to inject a run of
// whitespace into the log line it is embedded in. Host parsing lets a C1 byte
// through, StripControl folds each one to a space, and the collapse is what
// keeps the result readable as a single field.
//
// The fixture uses two ADJACENT C1 bytes on purpose. One is not enough to test
// anything: StripControl turns a single byte into a single space, which is
// exactly what the collapse would leave behind, so a one-byte fixture passes
// whether the collapse runs or not. Only a run tells the two apart.
func TestURLCollapsesWhitespaceIntroducedByStripping(t *testing.T) {
	const want = "https://exa mple.com/p"
	if got := URL("https://exa\u0085\u0086mple.com/p"); got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}
