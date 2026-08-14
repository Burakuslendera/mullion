package logsafe

import (
	"net/url"
	"strings"
	"testing"
)

// This was TestMessageStillManglesURLsWhichIsWhyURLExists, the trip-wire
// decisions/0025 set for the day Message learned about schemes. That day came
// (issue #80): a URL sitting *inside* a message - a JS error, a recovered panic
// - was losing its host for the same reason a bare one did, and no call-site
// swap could reach it, because url.Parse rejects the sentence and hands it back.
//
// So the wire fired and the question it asked was answered rather than silenced:
// Message now protects http(s) runs by delegating each one to URL, and the two
// agree on a value that is nothing but a URL. URL is still the right call for a
// field whose value *is* a URL - it bounds the whole value, and 0025's rule that
// every such field goes through it stands - but it is no longer the only way to
// keep a host in the log.
func TestMessageAndURLAgreeOnABareURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://mullion.local/index.html?in=1", "https://mullion.local/index.html?"},
		{"https://example.com/", "https://example.com/"},
		{"https://evil.example", "https://evil.example"},
	} {
		if got := Message(c.in); got != c.want {
			t.Fatalf("Message(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := URL(c.in); got != c.want {
			t.Fatalf("URL(%q) = %q, want %q - Message and URL must not disagree about a bare URL", c.in, got, c.want)
		}
	}
}

// The heart of the fix: an http/https URL keeps the scheme, host and path that
// say where a navigation went, and loses the query and fragment that are where a
// token would sit. Presence of either survives as a bare marker so two
// navigations differing only there stay distinguishable in a live log.
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
		// A bare "#" is a real navigation, distinct from no fragment at all. It has
		// no ForceQuery equivalent in url.URL, which is why presence is read off
		// the raw string instead of the parsed one.
		{"https://example.com/p#", "https://example.com/p#"},
		{"https://example.com/p?#", "https://example.com/p?#"},
		// A "?" inside the fragment is part of the fragment, not a query.
		{"https://example.com/p#a?b", "https://example.com/p#"},
		// url.Parse lower-cases the scheme; the host is logged as the runtime
		// reported it, because that is what a live reader compares against.
		{"HTTPS://EXAMPLE.COM", "https://EXAMPLE.COM"},
	} {
		if got := URL(c.in); got != c.want {
			t.Errorf("URL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The query is dropped, not reduced: a token a caller put in a query string is
// the real disclosure risk on this path, and Message currently *preserves* it
// while deleting the host. Credentials go the same way - url.URL keeps userinfo
// out of Host, so rebuilding from Host cannot re-emit them.
func TestURLDropsQueryAndCredentials(t *testing.T) {
	got := URL("https://user:hunter2@example.com/a/b?token=s3cr3t&next=%2Fadmin#tail")
	for _, forbidden := range []string{"hunter2", "user", "s3cr3t", "token", "next", "admin", "tail"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("URL() leaked %q in %q", forbidden, got)
		}
	}
	if want := "https://example.com/a/b?#"; got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}

	got = URL("https://user:p@ssword@example.com/a")
	if got != "https://example.com/a" {
		t.Fatalf("URL() did not reduce valid @ userinfo to the host alone: %q", got)
	}
}

// Everything without an http(s) authority falls back to Message verbatim.
//
// Four of these carry a contract of their own:
//   - file: - its path IS a local filesystem path, so it must still collapse to
//     a file name. Routing it through the URL branch leaks a home directory.
//   - data: - decisions/0021 was verified against the reduction Message gives a
//     data: URI. That must not move.
//   - an authority-less http form - "http:evil.example" and
//     "http:/C:/Users/..." both parse with scheme "http" and no host. Rebuilding
//     them from Host and Path erases the target or prints a home directory,
//     which is why the gate is the literal scheme prefix, not the parsed scheme.
//   - a value that does not parse, and "" - no worse than before.
func TestURLFallsBackToMessageForNonHTTP(t *testing.T) {
	for _, in := range []string{
		`file:///C:/Users/Alice O'Brien/AppData/secret.html`,
		"data:text/html,<b>hi</b>",
		"blob:https://evil.example/uuid",
		"filesystem:https://evil.example/temporary/x",
		"about:blank",
		"://noscheme",
		"",
		`\\server\share\O'Brien\rollout.jsonl`,
		"http:evil.example",
		"http:x/y",
		`http:/C:/Users/alice/AppData/Roaming/creds.json`,
		" http://example.com/leading-space",
	} {
		if got, want := URL(in), Message(in); got != want {
			t.Errorf("URL(%q) = %q, want the Message() reduction %q", in, got, want)
		}
	}

	// Spelled out for the two that matter most: no user or folder segment may
	// survive, whichever scheme carried the path.
	for _, in := range []string{
		`file:///C:/Users/Alice O'Brien/AppData/secret.html`,
		`http:/C:/Users/Alice O'Brien/AppData/secret.html`,
	} {
		got := URL(in)
		if !strings.Contains(got, "secret.html") {
			t.Errorf("URL(%q) = %q, want the file name retained", in, got)
		}
		for _, forbidden := range []string{"Alice", "O'Brien", "AppData", "Users"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("URL(%q) leaked %q in %q", in, forbidden, got)
			}
		}
	}
}

// A host is never truncated: it is printed whole or not at all.
//
// This is the property the bound exists for. Cutting a URL to fit and then
// re-emitting the prefix as a well-formed URL turns an attacker-chosen host into
// a shorter, more trustworthy-looking one - pad the name so the cut lands after
// ".mullion.local" and a navigation to "mullion.local.evil.example" logs as our
// own origin. A well-formed lie is worse than visible garbage, so a value that
// cannot fit is reduced by Message instead, and a path cut to fit says so.
func TestURLNeverTruncatesAHost(t *testing.T) {
	deceptive := "https://cdn." + strings.Repeat("pad-", 40) + "mullion.local.evil.example/session/refresh"
	got := URL(deceptive)
	if strings.HasSuffix(got, ".mullion.local") {
		t.Fatalf("URL() = %q, which reads as the trusted origin; the real host was mullion.local.evil.example", got)
	}
	if strings.HasPrefix(got, "https://") && !strings.Contains(got, "evil.example") {
		t.Fatalf("URL() = %q kept a host prefix without the part that identifies it", got)
	}

	// A long path is cut, and says so. The host still arrives whole.
	longPath := "https://mullion.local/" + strings.Repeat("segment/", 40) + "page.html"
	got = URL(longPath)
	if !strings.HasPrefix(got, "https://mullion.local/") {
		t.Fatalf("URL() = %q, want the whole host kept", got)
	}
	if !strings.HasSuffix(got, truncationMarker) {
		t.Fatalf("URL() = %q, want a truncation marker", got)
	}
	if len(got) > URLLimit+len(truncationMarker) {
		t.Fatalf("URL() = %d bytes, want at most %d", len(got), URLLimit+len(truncationMarker))
	}

	// A URL that fits is untouched and unmarked, so the marker means something.
	exact := "https://mullion.local/index.html"
	if got := URL(exact); got != exact {
		t.Fatalf("URL(%q) = %q, want it returned whole and unmarked", exact, got)
	}
}

// The query and fragment markers are read off the raw value, so they stay
// truthful for a URL long enough to be cut - the marker must never assert that a
// query was absent when it was merely beyond the bound. Nor may a credential
// ride a truncation into the log: cut "https://admin:<long>@evil.example" before
// its "@" and Go reads the credential as the host.
func TestURLMarkersAndCredentialsSurviveLength(t *testing.T) {
	long := "https://mullion.local/app/" + strings.Repeat("x", 200) + "?token=s3cr3t"
	got := URL(long)
	if !strings.HasSuffix(got, "?") {
		t.Fatalf("URL() = %q, want the query marker kept on a truncated URL", got)
	}
	if strings.Contains(got, "s3cr3t") || strings.Contains(got, "token") {
		t.Fatalf("URL() = %q leaked the query", got)
	}

	cred := "https://admin:" + strings.Repeat("9", 200) + "@evil.example/p"
	got = URL(cred)
	if strings.Contains(got, "999999") {
		t.Fatalf("URL() = %q leaked the credential", got)
	}
}

// A parse failure must not hand the raw value - query and all - back to the
// mangler: that reproduces issue #78 in its worst form, host deleted and token
// kept. A lone "%" is a legal, un-normalised character that reaches the callback.
func TestURLParseFailureStillDropsTheQuery(t *testing.T) {
	for _, in := range []string{
		"https://mullion.local/p?token=s3cr3t#100%",
		"https://mullion.local/50%off/p?token=s3cr3t",
		"https://mullion.local/p?token=s3cr3t#\x1b[2J",
	} {
		got := URL(in)
		if strings.Contains(got, "s3cr3t") || strings.Contains(got, "token") {
			t.Errorf("URL(%q) = %q leaked the query through the fallback", in, got)
		}
	}
}

func TestURLFallbackKeepsMarkersAndRejectsCutHosts(t *testing.T) {
	invalid := "https://" + strings.Repeat("a", 150) + ".mullion.local.evil.example/50%off/p?token=secret#tail"
	got := URL(invalid)
	if strings.Contains(got, "mullion.local") || strings.Contains(got, "evil.example") {
		t.Fatalf("URL(%q) retained a cut host prefix: %q", invalid, got)
	}
	if !strings.HasSuffix(got, "...?#") {
		t.Fatalf("URL(%q) = %q, want a visible cut and query/fragment markers", invalid, got)
	}
	if len(got) > URLLimit {
		t.Fatalf("URL(%q) = %d bytes, want at most %d", invalid, len(got), URLLimit)
	}

	badHost := "https://evil.example,approved=true/p?token=secret#tail"
	got = URL(badHost)
	if !strings.HasSuffix(got, "?#") {
		t.Fatalf("URL(%q) = %q, want query/fragment markers on fallback", badHost, got)
	}
	if strings.Contains(got, "token") || strings.Contains(got, "secret") {
		t.Fatalf("URL(%q) leaked fallback query: %q", badHost, got)
	}
}

func TestURLNonHTTPFallbackMarksBoundedOutput(t *testing.T) {
	raw := "data:text/plain," + strings.Repeat("x", URLLimit)
	got := URL(raw)
	if len(got) > URLLimit {
		t.Fatalf("URL(data) = %d bytes, want at most %d", len(got), URLLimit)
	}
	if !strings.HasSuffix(got, truncationMarker) {
		t.Fatalf("URL(data) = %q, want a visible truncation marker", got)
	}
}

var reducedURLSink string

func TestURLPathAllocationBytesAreInputSizeIndependent(t *testing.T) {
	measure := func(size int) int64 {
		input := "https://mullion.local/" + strings.Repeat("\u00e9", size/2)
		return testing.Benchmark(func(b *testing.B) {
			for range b.N {
				reducedURLSink = URL(input)
			}
		}).AllocedBytesPerOp()
	}

	oneKiB := measure(1 << 10)
	oneMiB := measure(1 << 20)
	if oneMiB > oneKiB+512 {
		t.Fatalf("URL allocated bytes grow with path input: 1 KiB=%d, 1 MiB=%d", oneKiB, oneMiB)
	}
}

func TestURLUserinfoAllocationBytesAreInputSizeIndependent(t *testing.T) {
	measure := func(size int) int64 {
		input := "https://" + strings.Repeat("a", size) + "@mullion.local" + "host/app.js"
		return testing.Benchmark(func(b *testing.B) {
			for range b.N {
				reducedURLSink = URL(input)
			}
		}).AllocedBytesPerOp()
	}

	oneKiB := measure(1 << 10)
	oneMiB := measure(1 << 20)
	if oneMiB > oneKiB+512 {
		t.Fatalf("URL allocated bytes grow with userinfo input: 1 KiB=%d, 1 MiB=%d", oneKiB, oneMiB)
	}
}

func TestURLTruncationKeepsCompletePathEncodingUnits(t *testing.T) {
	const origin = "https://example.com"
	budget := URLLimit - len(origin)
	for _, c := range []struct {
		name    string
		unit    string
		room    int
		present bool
	}{
		{"percent escape at exhausted boundary", "%2F", 0, false},
		{"percent escape after first byte", "%2F", 1, false},
		{"percent escape before boundary", "%2F", 2, false},
		{"percent escape on boundary", "%2F", 3, true},
		{"encoded rune after first escape", "%E2%82%AC", 3, false},
		{"encoded rune after second escape", "%E2%82%AC", 6, false},
		{"encoded rune on boundary", "%E2%82%AC", 9, true},
		{"raw multibyte rune before boundary", "\u20ac", 8, false},
		{"raw multibyte rune on boundary", "\u20ac", 9, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := "/" + strings.Repeat("a", budget-1-c.room) + c.unit + "tail"
			got := URL(origin + path + "?#")
			if len(got) > URLLimit+len(truncationMarker)+2 {
				t.Fatalf("URL() emitted %d bytes, want at most %d: %q", len(got), URLLimit+len(truncationMarker)+2, got)
			}
			if _, err := url.Parse(got); err != nil {
				t.Fatalf("URL() emitted an unparsable projection %q: %v", got, err)
			}
			encoded := c.unit
			if c.unit == "\u20ac" {
				encoded = "%E2%82%AC"
			}
			if strings.Contains(got, encoded) != c.present {
				t.Fatalf("URL() = %q, complete unit %q presence = %t, want %t", got, encoded, strings.Contains(got, encoded), c.present)
			}
			if !strings.HasSuffix(got, truncationMarker+"?#") {
				t.Fatalf("URL() = %q, want visible truncation and query/fragment markers", got)
			}
		})
	}
}

func TestURLRejectsMalformedEscapeBeyondTheProjection(t *testing.T) {
	raw := "https://decoy.example/" + strings.Repeat("a", 1<<20) + "%zz?secret=value"
	got := URL(raw)
	if strings.HasPrefix(got, "https://decoy.example") {
		t.Fatalf("URL() accepted a malformed late path escape: %q", got)
	}
	if strings.Contains(got, "secret=value") {
		t.Fatalf("URL() leaked a query value through the malformed-path fallback: %q", got)
	}
}
