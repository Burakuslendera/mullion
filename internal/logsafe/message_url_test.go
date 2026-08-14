package logsafe

import (
	"strings"
	"testing"
)

// A URL sitting inside a message keeps its host (issue #80). Issue #78 fixed the
// fields whose value *is* a URL; these are the fields whose value *contains*
// one - a JS error from window.onerror, a recovered panic naming the navigation
// it was handling - and they are the ERROR lines a "blank window" report is
// triaged with.
//
// Swapping those call sites to URL fixes nothing, which is why this had to be
// solved a level down: the value is not a URL, so URL hands it back here
// unreduced.
func TestMessageKeepsURLsEmbeddedInASentence(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{
			"Failed to fetch dynamically imported module: https://mullion.local/app/main.js",
			"Failed to fetch dynamically imported module: https://mullion.local/app/main.js",
		},
		{
			// Chrome quotes the URL, so a run cannot be required to start at a
			// word boundary - and the quote must stay glued to it.
			"Refused to load the script 'https://cdn.evil.example/x.js' because it violates CSP",
			"Refused to load the script 'https://cdn.evil.example/x.js' because it violates CSP",
		},
		{
			"navigate https://mullion.local/index.html: nil map",
			"navigate https://mullion.local/index.html: nil map",
		},
		{
			// The query still goes, wherever the URL sits: a token in one is the
			// disclosure this package exists for (decisions/0025).
			"load failed for https://mullion.local/app?token=s3cr3t and gave up",
			"load failed for https://mullion.local/app? and gave up",
		},
		{
			// Two of them, and a Windows path in the same sentence: the path is
			// still reduced, the URLs are not.
			`opening C:\Users\alice\app.log for https://a.example/x and https://b.example/y`,
			"opening app.log for https://a.example/x and https://b.example/y",
		},
		{
			// A URL carrying another one in its query is one run, not two. If
			// the scan stepped into the run instead of over it, the inner scheme
			// would be matched after the outer run had already been consumed and
			// the slice would panic - on the commonest redirect shape there is.
			"https://mullion.local/login?next=https://mullion.local/app",
			"https://mullion.local/login?",
		},
		{
			// Trailing whitespace after a run must not survive as a trailing
			// space in the field.
			"see https://a.example/x  ",
			"see https://a.example/x",
		},
	} {
		if got := Message(c.in); got != c.want {
			t.Errorf("Message(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// The property that keeps this out of the ~90 call sites decisions/0025 did not
// want to audit: a message carrying no http(s) URL is reduced as it was before.
//
// It is not quite byte-identical, and the exception is the reason this test says
// so rather than claiming more than it can. A message whose whole reduction
// collapses to nothing - "// /" does, through FileName returning a lone space -
// used to come out as the empty string and now comes out as "unknown". A log
// field with nothing after the "=" is worse, so the change is kept; the claim is
// narrowed instead.
func TestMessageIsUnchangedWhereThereIsNoURL(t *testing.T) {
	for _, message := range []string{
		"",
		"   ",
		"plain message with no url at all",
		`open C:\Users\alice\secret.txt: access denied`,
		`open "C:\Program Files\App\my log.txt" failed`,
		`\\FILESERVER\share\finance\q3.xlsx not found`,
		"file:///C:/Users/alice/notes.txt could not be read",
		"blob:null/9f3c-uuid revoked",
		"error at /home/alice/src/main.go:42",
		"ftp://example.com/pub/file.txt refused",
		"hxxps://not-a-scheme.example/x",
		"trailing colon: value",
		"\x00\x1b[2Jcleared\x07",
		"line one\r\nline two",
	} {
		if got, want := Message(message), messagePlain(message); got != want {
			t.Errorf("Message(%q) = %q, want the untouched reduction %q", message, got, want)
		}
	}
	// The known exception, pinned so it cannot widen unnoticed.
	for _, message := range []string{"// /", `\\ '`, `C:\Users\alice\ \`} {
		if got := Message(message); got != "unknown" {
			t.Errorf("Message(%q) = %q, want %q", message, got, "unknown")
		}
	}
}

// The forgery decisions/0025 spent a round eliminating, which this widening can
// re-open in three different ways. Each row below is one of them.
//
// The trusted origin must never appear as a well-formed origin unless it really
// was the whole host. Asserting the absence of the mangled form as well would be
// wrong: "httpmullion.local" is the old reduction doing its job, and no reader
// mistakes it for an origin - that is the whole distinction 0025 draws.
func TestNothingPrintsAShortenedHostAsAWholeOne(t *testing.T) {
	const forged = "https://mullion.local"
	for _, c := range []struct{ how, in string }{
		// A control byte folded to a space would split the host if the scan ran
		// after StripControl. The scan runs first, so URL sees the whole host
		// and refuses it.
		{"C1 NEL inside the host", "blocked https://mullion.local\u0085.evil.example/x"},
		{"C1 non-space inside the host", "blocked https://mullion.local\u0086.evil.example/x"},
		{"DEL inside the host", "blocked https://mullion.local\x7f.evil.example/x"},
		{"C0 inside the host", "blocked https://mullion.local\x01.evil.example/x"},
		// A URL parser deletes TAB, LF and CR outright, so to a browser these
		// are one host and the part before the byte is a prefix of it. Ending
		// the run there and printing what precedes it is the forgery.
		{"tab inside the host", "blocked https://mullion.local\t.evil.example/x"},
		{"newline inside the host", "blocked https://mullion.local\n.evil.example/x"},
		{"carriage return inside the host", "blocked https://mullion.local\r.evil.example/x"},
		{"vertical tab inside the host", "blocked https://mullion.local\v.evil.example/x"},
		{"form feed inside the host", "blocked https://mullion.local\f.evil.example/x"},
		// A printable byte must not end a run at all. Go's parser accepts a
		// quote in a host, so cutting the run there would hand URL a shorter
		// host that parses.
		{"double quote inside the host", `blocked https://mullion.local"x.evil.example/y`},
		{"apostrophe inside the host", "blocked https://mullion.local'x.evil.example/y"},
		// A zero-width space renders as nothing, so a host carrying one looks
		// exactly like the shorter host it is not.
		{"zero-width space inside the host", "blocked https://mullion.local\u200b.evil.example/x"},
	} {
		if got := Message(c.in); strings.Contains(got, forged) {
			t.Errorf("%s: Message(%q) = %q - a shortened host was printed as a whole one", c.how, c.in, got)
		}
	}
}

// The same forgery through the other door: a value bounded before it is scanned.
// URL's non-http fallback cuts to URLLimit and then hands the result to Message,
// which keeps hosts - so a cut landing inside one would print the prefix as an
// origin. The padding is chosen by whoever wrote the value, so landing the cut
// on a label boundary is free.
func TestABoundedValueCannotForgeAnOrigin(t *testing.T) {
	// Sized so the 160-byte cut falls immediately after ".mullion.local".
	const prefix = "blob:https://cdn."
	pad := strings.Repeat("a", URLLimit-len(prefix)-len(".mullion.local"))
	attack := prefix + pad + ".mullion.local.evil.example/x"

	got := URL(attack)
	if strings.Contains(got, "https://cdn.") {
		t.Errorf("URL(<%d bytes>) = %q - a cut host was printed as a whole origin", len(attack), got)
	}
	// The same value under the limit is not cut, so its origin survives whole.
	short := "blob:https://cdn.mullion.local.evil.example/x"
	if want := "blob:https://cdn.mullion.local.evil.example/x"; URL(short) != want {
		t.Errorf("URL(%q) = %q, want %q", short, URL(short), want)
	}
}

// A run ends at whitespace, and what follows must still be visible. Two URLs
// separated by a control byte would otherwise weld into one, which reads as the
// second being a path of the first - a foreign origin hidden inside a trusted
// one.
func TestTwoURLsAreNeverWeldedIntoOne(t *testing.T) {
	// The separator ends the first run and then folds away, so the part between
	// the two runs reduces to nothing. Without a rule for that, the second URL
	// is written straight onto the first.
	for _, separator := range []string{" ", " \x00", " \x7f", " \u0085", " \u00a0", "\t"} {
		message := "load failed for https://mullion.local/a" + separator + "https://evil.example/b"
		got := Message(message)
		if strings.Contains(got, "/ahttps://evil.example") {
			t.Errorf("separator %q: Message(...) = %q - two URLs were welded into one token", separator, got)
		}
		if !strings.Contains(got, "https://evil.example/b") {
			t.Errorf("separator %q: Message(...) = %q - the second URL vanished", separator, got)
		}
	}
	// With no whitespace the two are one run, and what follows depends on the
	// byte. A C0 byte or DEL makes url.Parse refuse the whole run, so both
	// origins are lost - the direction this package fails in.
	for _, separator := range []string{"\x00", "\x7f"} {
		message := "load failed for https://mullion.local/a" + separator + "https://evil.example/b"
		if got := Message(message); strings.Contains(got, "https://") {
			t.Errorf("separator %q: Message(...) = %q - a run carrying a control byte was reduced as a URL", separator, got)
		}
	}
	// A byte a URL may legitimately carry in its path leaves one honest URL:
	// mullion.local really is its host, and the separator survives
	// percent-encoded, so the second origin sits visibly inside the path rather
	// than passing as part of it.
	const inPath = "load failed for https://mullion.local/a\u0085https://evil.example/b"
	if got := Message(inPath); !strings.Contains(got, "%C2%85") {
		t.Errorf("Message(%q) = %q - the separator between the two URLs was not preserved", inPath, got)
	}
}

// A run ends cleanly at whitespace, and a byte that is not whitespace keeps it
// open. In a path a zero-width space is percent-encoded and the rest of the path
// survives - nothing is hidden and nothing is forged.
func TestAURLRunEndsOnlyAtASCIIWhitespace(t *testing.T) {
	for _, separator := range []string{" ", "\t", "\n", "\r", "\v", "\f"} {
		message := "at https://a.example/x" + separator + "then more"
		if got := Message(message); !strings.Contains(got, "https://a.example/x") {
			t.Errorf("Message(%q) = %q - whitespace must end the run cleanly once the host is complete", message, got)
		}
	}
	const inPath = "at https://a.example/x\u200bhidden.evil.example/y"
	if got := Message(inPath); !strings.Contains(got, "hidden.evil.example") {
		t.Errorf("Message(%q) = %q - the run was cut at a zero-width space and the rest of the path vanished", inPath, got)
	}
}

// URL may be entered from Message; Message must never be re-entered from the
// branches of URL that keep the scheme on the value they hand back, or the two
// call each other until the stack runs out. A regression does not fail this
// test with a message - it kills the package with a stack overflow, which is
// loud but lands in whichever test happens to run first.
//
// Only the second row exercises that cycle: it is an http(s) value whose host
// url.Parse will not accept, which is the branch that must not return to
// Message. The others document the paths that do terminate.
func TestURLAndMessageDoNotRecurse(t *testing.T) {
	for _, message := range []string{
		"https://exa mple.com/p",
		"https://bad%host/x",
		"blob:https://evil.example/uuid",
		"https://" + strings.Repeat("a", 400) + ".example/x",
	} {
		Message(message)
		URL(message)
	}
}

// A blob: URL wraps a web origin, and both entry points say so - Message
// because it scans, URL because a value it cannot reduce as a URL goes to
// Message rather than past it. The wrapper stays glued to what it wraps: one
// token in, one token out.
func TestBlobURLKeepsTheOriginItWraps(t *testing.T) {
	const in = "blob:https://evil.example/uuid"
	const want = "blob:https://evil.example/uuid"
	if got := Message(in); got != want {
		t.Errorf("Message(%q) = %q, want %q", in, got, want)
	}
	if got := URL(in); got != want {
		t.Errorf("URL(%q) = %q, want %q", in, got, want)
	}
}

func TestLongOpaqueHTTPFallbackKeepsInnerOrigin(t *testing.T) {
	for _, prefix := range []string{"blob:", "filesystem:"} {
		raw := prefix + "https://evil.example/" + strings.Repeat("segment/", URLLimit)
		got := URL(raw)
		if !strings.HasPrefix(got, prefix+"https://evil.example/") {
			t.Fatalf("URL(%q) = %q, want the complete wrapped HTTP origin", raw, got)
		}
		if !strings.HasSuffix(got, truncationMarker) {
			t.Fatalf("URL(%q) = %q, want a bounded opaque suffix marker", raw, got)
		}
		if len(got) > URLLimit+len(truncationMarker) {
			t.Fatalf("URL(%q) = %d bytes, want at most %d", raw, len(got), URLLimit+len(truncationMarker))
		}
	}
}

func TestLongFileFallbackKeepsFileNameTail(t *testing.T) {
	raw := "file:///C:/Users/" + "Alice/" + strings.Repeat("private/", URLLimit) + "secret.html"
	got := URL(raw)
	if !strings.Contains(got, "secret.html") {
		t.Fatalf("URL(%q) = %q, want the final file name", raw, got)
	}
	for _, forbidden := range []string{"Alice", "Users", "private"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("URL(%q) = %q leaked %q from the path", raw, got, forbidden)
		}
	}
	if !strings.HasSuffix(got, truncationMarker) {
		t.Fatalf("URL(%q) = %q, want a visible input-bound marker", raw, got)
	}
	if len(got) > URLLimit {
		t.Fatalf("URL(%q) = %d bytes, want at most %d", raw, len(got), URLLimit)
	}
}
