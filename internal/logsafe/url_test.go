package logsafe

import (
	"strings"
	"testing"
)

// The bug URL exists to route around (issue #78). Pinned to Message, not to URL:
// it documents the behaviour that made URL necessary, and it fails the day
// someone teaches Message about schemes, at which point this file and its call
// sites want revisiting rather than silently keeping a redundant wrapper.
func TestMessageStillManglesURLsWhichIsWhyURLExists(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://mullion.local/index.html?in=1", "httpindex.html?in=1"},
		{"https://example.com/", "httpexample.com"},
		{"https://evil.example", "httpevil.example"},
	} {
		if got := Message(c.in); got != c.want {
			t.Fatalf("Message(%q) = %q, want %q - if this changed, re-check whether URL is still needed", c.in, got, c.want)
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
