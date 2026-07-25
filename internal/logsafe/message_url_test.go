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
// solved a level down: url.Parse rejects the whole sentence and URL hands it
// straight back to Message.
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
	} {
		if got := Message(c.in); got != c.want {
			t.Errorf("Message(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// The property that keeps this out of the ~90 call sites decisions/0025 did not
// want to audit: a message carrying no http(s) URL is reduced exactly as it was
// before, byte for byte. Asserted as the property rather than as a table of
// remembered outputs, so it stays true for inputs nobody thought of.
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
}

// The forgery decisions/0025 spent a round eliminating, reachable again through
// this widening if the scan ran in the wrong order.
//
// StripControl folds a control byte to a space. Find the URL runs afterwards and
// a host with one inside it splits, and the part before the fold is printed as
// though it were the whole host - a foreign origin rendered as the trusted one.
// Finding the run first keeps the control byte inside it, where URL refuses the
// host outright and the value falls back to the reduction that leaves no host at
// all.
func TestAControlByteInsideAHostCannotForgeAShorterOne(t *testing.T) {
	for _, message := range []string{
		"blocked https://mullion.local\u0085.evil.example/x",
		"blocked https://mullion.local\u0086.evil.example/x",
		"blocked https://mullion.local\x7f.evil.example/x",
		"blocked https://mullion.local\x01.evil.example/x",
	} {
		got := Message(message)
		if strings.Contains(got, "mullion.local") {
			t.Errorf("Message(%q) = %q - a host split by a folded control byte was printed as a whole one", message, got)
		}
		if strings.Contains(got, "evil.example") {
			t.Errorf("Message(%q) = %q - the value was reduced as a URL although its host is not hostname-shaped", message, got)
		}
	}
}

// A URL run ends at ASCII whitespace and at nothing else. Ending it at any byte
// that StripControl would fold is the same defect as folding first.
func TestAURLRunEndsOnlyAtASCIIWhitespace(t *testing.T) {
	for _, separator := range []string{" ", "\t", "\n", "\r", "\v", "\f"} {
		message := "at https://a.example/x" + separator + "then more"
		if got := Message(message); !strings.Contains(got, "https://a.example/x") {
			t.Errorf("Message(%q) = %q - whitespace must end the run cleanly", message, got)
		}
	}
	// A non-whitespace byte keeps the run open, so the value is judged whole
	// rather than cut. Here the zero-width space is in the path, where the host
	// really is a.example and EscapedPath encodes the rest - nothing is hidden
	// and nothing is forged.
	const inPath = "at https://a.example/x\u200bhidden.evil.example/y"
	if got := Message(inPath); !strings.Contains(got, "hidden.evil.example") {
		t.Errorf("Message(%q) = %q - the run was cut at a zero-width space and the rest of the path vanished", inPath, got)
	}
	// In the host it is the forgery case: a.example is not the whole host, so
	// nothing hostname-shaped may be printed at all.
	const inHost = "at https://a.example\u200b.evil.example/y"
	if got := Message(inHost); strings.Contains(got, "a.example") {
		t.Errorf("Message(%q) = %q - a host carrying a zero-width space was printed as a shorter one", inHost, got)
	}
}

// URL may be entered from Message; Message must never be re-entered from the
// branches of URL that keep the scheme on the value they hand back, or the two
// call each other for ever. The value below takes exactly that path - an
// http(s) prefix whose host url.Parse will not accept - and a regression here
// does not fail this test, it hangs the package.
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

// A blob: URL wraps a web origin, and both entry points now say so - Message
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
