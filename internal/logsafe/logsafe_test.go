package logsafe

import (
	"errors"
	"strings"
	"testing"
	"unsafe"
)

func TestReasonSanitizesWindowsPathWithSpaces(t *testing.T) {
	got := Message(`open C:\Users\Example User\AppData\Roaming\Acme\logs\latest.log: access denied`)
	want := "open latest.log: access denied"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	for _, forbidden := range []string{`C:\Users`, "Example User", "AppData", "Acme"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestReasonSanitizesQuotedWindowsPathWithSpaces(t *testing.T) {
	got := Message(`open "C:\Users\Example User\AppData\Roaming\Acme\logs\latest.log": access denied`)
	if !strings.Contains(got, "latest.log") {
		t.Fatalf("Message() = %q, want file name", got)
	}
	for _, forbidden := range []string{`C:\Users`, "Example User", "AppData", "Acme"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

// Synthetic apostrophe fixtures only. Windows user/folder names can contain
// apostrophes (O'Brien, D'Angelo, Team's Files); the sanitizer must still
// collapse the whole path to its file name and leak no directory/user segment.

func TestMessageSanitizesApostropheWindowsUserPath(t *testing.T) {
	got := Message(`open C:\Users\Alice O'Brien\AppData\Acme\secret.log: denied`)
	want := "open secret.log: denied"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"Alice", "O'Brien", "AppData", "Acme", `C:\Users`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestMessageSanitizesApostropheFolderPath(t *testing.T) {
	got := Message(`open C:\Work\Team's Files\rollout.jsonl: access denied`)
	want := "open rollout.jsonl: access denied"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"Team's", "Files", `C:\Work`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestMessageSanitizesApostropheUNCPath(t *testing.T) {
	got := Message(`read \\server\share\O'Brien\rollout.jsonl: denied`)
	want := "read rollout.jsonl: denied"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"server", "share", "O'Brien", `\\server`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

// Two whitespace-separated paths collapse to the final file name. This is
// pre-existing span behavior (the sanitizer intentionally does not treat
// whitespace as a terminator, because Windows paths may contain spaces); the
// privacy contract is that no directory or user segment survives.
func TestMessageSanitizesMultipleApostrophePaths(t *testing.T) {
	got := Message(`copy C:\Users\Alice O'Brien\a.log to D:\Temp\Team's\b.log: failed`)
	if !strings.Contains(got, "b.log") {
		t.Fatalf("Message() = %q, want final file name retained", got)
	}
	for _, forbidden := range []string{"Alice", "O'Brien", "Team's", "Temp", "Users", "AppData", `C:\`, `D:\`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestMessagePreservesReasonWhileStrippingApostrophePath(t *testing.T) {
	got := Message(`open C:\Users\D'Angelo\AppData\rollout.jsonl: permission denied while reading`)
	want := "open rollout.jsonl: permission denied while reading"
	if got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"D'Angelo", "AppData", `C:\Users`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestMessageSanitizesQuotedApostrophePath(t *testing.T) {
	got := Message(`open "C:\Users\O'Brien\latest.log": denied`)
	if !strings.Contains(got, "latest.log") {
		t.Fatalf("Message() = %q, want file name", got)
	}
	for _, forbidden := range []string{`C:\Users`, "O'Brien"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Message() leaked %q in %q", forbidden, got)
		}
	}
}

func TestReasonAndMessagePreserveEmptyAndApostropheBehavior(t *testing.T) {
	if got := Reason(nil); got != "unknown" {
		t.Fatalf("Reason(nil) = %q, want %q", got, "unknown")
	}
	if got := Message("   "); got != "unknown" {
		t.Fatalf("Message(blank) = %q, want %q", got, "unknown")
	}
	got := Reason(errors.New(`stat C:\Users\Ana O'Neil\x.log: no such file`))
	if !strings.Contains(got, "x.log") {
		t.Fatalf("Reason() = %q, want file name", got)
	}
	for _, forbidden := range []string{"Ana", "O'Neil", `C:\Users`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("Reason() leaked %q in %q", forbidden, got)
		}
	}
}

// TestMessageStripsControlBytes locks the escape-sequence half of the log-safety
// contract. CRLF forging was already blocked; a frontend-controlled string must
// also not carry an ANSI/OSC terminal escape, a NUL, or a provenance-erasing
// backspace through to the caller's Logger. Each of these bytes reaches Message
// unbounded from the frontend (a bridge method name, a WindowDiagnostic detail),
// so the sanitizer is the boundary that must neutralise them.
func TestMessageStripsControlBytes(t *testing.T) {
	isControl := func(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) }
	cases := []struct {
		name string
		in   string
	}{
		{"csi clear screen", "a\x1b[2Jb"},
		{"osc title with bel", "x\x1b]0;pwned\x07y"},
		{"backspace erases prefix", "mullion:\x08\x08\x08fake"},
		{"nul byte", "a\x00b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Message(c.in)
			for _, r := range got {
				if isControl(r) {
					t.Fatalf("Message(%q) = %q still carries control rune %#x", c.in, got, r)
				}
			}
		})
	}
	// The payload text survives, minus the escapes: the line stays readable, the
	// escape sequence is just inert.
	if got := Message("a\x1b[2Jb"); !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("Message dropped payload text: %q", got)
	}
	// FileName sits on the same boundary and must strip too.
	if got := FileName("\x1b[2Jname.log"); strings.ContainsRune(got, 0x1b) {
		t.Fatalf("FileName leaked ESC: %q", got)
	}
}

var sanitizedTokenSink string
var diagnosticURLStartSink int
var diagnosticURLValueSink string

func TestSanitizeTokenTrailingPunctuationIsLinear(t *testing.T) {
	short := "x.:;,.:;,"
	long := "x" + strings.Repeat(".:;,", 2048)
	if got := sanitizeToken(long); got != long {
		t.Fatalf("sanitizeToken() changed punctuation: got len %d, want len %d", len(got), len(long))
	}
	if got, want := sanitizeToken(".:;,"), "unknown.:;,"; got != want {
		t.Fatalf("sanitizeToken(all punctuation) = %q, want %q", got, want)
	}

	shortAllocs := testing.AllocsPerRun(10, func() {
		sanitizedTokenSink = sanitizeToken(short)
	})
	longAllocs := testing.AllocsPerRun(10, func() {
		sanitizedTokenSink = sanitizeToken(long)
	})
	if longAllocs > shortAllocs+2 {
		t.Fatalf("allocations grow with suffix length: short=%v long=%v", shortAllocs, longAllocs)
	}
}

func TestDiagnosticBoundsInputAndKeepsFirstMeaningfulURL(t *testing.T) {
	raw := strings.Repeat("prefix.:;, ", DiagnosticLimit*2) +
		"https://mullion.localhost/app/main.js?token=secret " +
		strings.Repeat("tail", DiagnosticLimit)
	got := Diagnostic(raw)
	if len(got) > DiagnosticLimit {
		t.Fatalf("Diagnostic() len = %d, limit = %d", len(got), DiagnosticLimit)
	}
	if !strings.Contains(got, "https://mullion.localhost/app/main.js?") {
		t.Fatalf("Diagnostic() lost the first meaningful URL: %q", got)
	}
	if strings.Contains(got, "token=secret") {
		t.Fatalf("Diagnostic() retained a query value: %q", got)
	}
}

func TestDiagnosticNeverPrintsAnInterruptedHost(t *testing.T) {
	raw := "before https://mullion.example." + strings.Repeat("attacker", DiagnosticLimit)
	got := Diagnostic(raw)
	if strings.Contains(got, "https://mullion.example") {
		t.Fatalf("Diagnostic() printed a host prefix as a whole URL: %q", got)
	}
	if len(got) > DiagnosticLimit {
		t.Fatalf("Diagnostic() len = %d, limit = %d", len(got), DiagnosticLimit)
	}
}

func TestDiagnosticFileNameDoesNotRetainLargeInput(t *testing.T) {
	raw := strings.Repeat("x", DiagnosticLimit*4) + `/tiny.js`
	got := DiagnosticFileName(raw)
	if got != "tiny.js" {
		t.Fatalf("DiagnosticFileName() = %q, want tiny.js", got)
	}
	rawStart := uintptr(unsafe.Pointer(unsafe.StringData(raw)))
	gotStart := uintptr(unsafe.Pointer(unsafe.StringData(got)))
	if gotStart >= rawStart && gotStart < rawStart+uintptr(len(raw)) {
		t.Fatal("DiagnosticFileName() shares the large input's backing storage")
	}
}

func TestDiagnosticRejectsAuthorityCutByASCIIWhitespace(t *testing.T) {
	for _, terminator := range []byte{'\t', '\n', '\r', '\v', '\f'} {
		raw := strings.Repeat("context ", DiagnosticLimit) +
			"https://mullion.local" + "host" + string(terminator) + ".evil.example/path"
		got := Diagnostic(raw)
		if strings.Contains(got, "https://mullion.localhost") {
			t.Fatalf("terminator %#x preserved an incomplete authority: %q", terminator, got)
		}
	}
}

func TestDiagnosticCompletesBoundaryEscapeAndKeepsURLMarks(t *testing.T) {
	const base = "https://mullion.localhost/"
	padding := strings.Repeat("a", URLLimit-len(base))
	raw := strings.Repeat("context ", DiagnosticLimit) +
		base + padding + "%2Ftail?secret=value#fragment"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/") || !strings.Contains(got, "...?#") {
		t.Fatalf("Diagnostic() lost the safely truncated URL or its marks: %q", got)
	}
	if strings.Contains(got, "secret=value") || strings.Contains(got, "fragment") {
		t.Fatalf("Diagnostic() retained query or fragment values: %q", got)
	}
}

func TestDiagnosticURLSelectionDoesNotAllocatePerFakeScheme(t *testing.T) {
	short := "https://% https://mullion.localhost/app.js"
	long := strings.Repeat("https://% ", 2048) + "https://mullion.localhost/app.js"
	start, value := firstDiagnosticURL(long)
	if start == 0 || value != "https://mullion.localhost/app.js" {
		t.Fatalf("firstDiagnosticURL() = (%d, %q), want trailing meaningful URL", start, value)
	}

	shortAllocs := testing.AllocsPerRun(10, func() {
		diagnosticURLStartSink, diagnosticURLValueSink = firstDiagnosticURL(short)
	})
	longAllocs := testing.AllocsPerRun(10, func() {
		diagnosticURLStartSink, diagnosticURLValueSink = firstDiagnosticURL(long)
	})
	if longAllocs > shortAllocs+2 {
		t.Fatalf("allocations grow with fake scheme count: short=%v long=%v", shortAllocs, longAllocs)
	}

	shortBytes := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			diagnosticURLStartSink, diagnosticURLValueSink = firstDiagnosticURL(short)
		}
	}).AllocedBytesPerOp()
	longBytes := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			diagnosticURLStartSink, diagnosticURLValueSink = firstDiagnosticURL(long)
		}
	}).AllocedBytesPerOp()
	if longBytes > shortBytes+64 {
		t.Fatalf("allocated bytes grow with fake scheme count: short=%d long=%d", shortBytes, longBytes)
	}
}

func TestDiagnosticURLSelectionSkipsOverBudgetAuthority(t *testing.T) {
	decoy := "https://" + strings.Repeat("a", URLLimit) + "/decoy"
	raw := strings.Repeat("context ", DiagnosticLimit) +
		decoy + " https://mullion.localhost/app.js?secret=value"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js?") {
		t.Fatalf("Diagnostic() lost the later meaningful URL: %q", got)
	}
	if strings.Contains(got, decoy) || strings.Contains(got, "secret=value") {
		t.Fatalf("Diagnostic() retained the rejected decoy or query value: %q", got)
	}
}

func TestDiagnosticURLSelectionDropsUserinfoCredentials(t *testing.T) {
	raw := strings.Repeat("context ", DiagnosticLimit) +
		"https://alice:hunter2@mullion.local" + "host/app.js?secret=value"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js?") {
		t.Fatalf("Diagnostic() lost the credential-free URL: %q", got)
	}
	for _, secret := range []string{"alice", "hunter2", "secret=value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Diagnostic() retained %q in %q", secret, got)
		}
	}
}

func TestDiagnosticURLSelectionValidatesTheWholePath(t *testing.T) {
	bad := "https://decoy.example/" + strings.Repeat("a", DiagnosticLimit) + "%zz"
	raw := strings.Repeat("context ", DiagnosticLimit) +
		bad + " https://mullion.localhost/app.js"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js") {
		t.Fatalf("Diagnostic() let a malformed long-path decoy displace the later URL: %q", got)
	}
	if strings.Contains(got, "https://decoy.example") {
		t.Fatalf("Diagnostic() retained the malformed decoy: %q", got)
	}
}

func TestDiagnosticURLSelectionRejectsLateControlByteInWholePath(t *testing.T) {
	bad := "https://decoy.example/" + strings.Repeat("a", DiagnosticLimit) + "\x1b"
	raw := strings.Repeat("context ", DiagnosticLimit) +
		bad + " https://mullion.localhost/app.js"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js") {
		t.Fatalf("Diagnostic() let a control-bearing long-path decoy displace the later URL: %q", got)
	}
	if strings.Contains(got, "https://decoy.example") {
		t.Fatalf("Diagnostic() retained the control-bearing decoy: %q", got)
	}
}

func TestDiagnosticURLSelectionSkipsMalformedUserinfoDecoys(t *testing.T) {
	for _, userinfo := range []string{"bad%", "bad%A", "bad%zz", "bad^name", "bad\u00e9"} {
		raw := strings.Repeat("context ", DiagnosticLimit) +
			"https://" + userinfo + "@decoy.example/hidden https://mullion.localhost/app.js"
		got := Diagnostic(raw)
		if !strings.Contains(got, "https://mullion.localhost/app.js") {
			t.Errorf("Diagnostic() let malformed userinfo %q displace the later URL: %q", userinfo, got)
		}
		if strings.Contains(got, "https://decoy.example") {
			t.Errorf("Diagnostic() retained the malformed-userinfo decoy %q: %q", userinfo, got)
		}
	}
}

func TestDiagnosticReservesLateURLAfterPrefixReductionExpands(t *testing.T) {
	raw := strings.Repeat(". ", DiagnosticLimit) +
		"https://mullion.localhost/app.js?secret=value"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js?") {
		t.Fatalf("Diagnostic() let an expanding plain prefix displace the selected URL: %q", got)
	}
	if strings.Contains(got, "secret=value") {
		t.Fatalf("Diagnostic() retained the selected URL query value: %q", got)
	}
	if len(got) > DiagnosticLimit {
		t.Fatalf("Diagnostic() emitted %d bytes, limit = %d", len(got), DiagnosticLimit)
	}
}

func TestDiagnosticURLSelectionSkipsMalformedBracketedAuthority(t *testing.T) {
	raw := strings.Repeat("context ", DiagnosticLimit) +
		strings.Repeat("https://[bad/path ", 10) + "https://mullion.localhost/app.js"
	got := Diagnostic(raw)
	if !strings.Contains(got, "https://mullion.localhost/app.js") {
		t.Fatalf("Diagnostic() lost the later meaningful URL: %q", got)
	}
}

func TestDiagnosticURLSelectionKeepsValidEmptyPorts(t *testing.T) {
	for _, target := range []string{
		"https://example.invalid:/app.js",
		"https://[2001:db8::1]:/app.js",
	} {
		raw := strings.Repeat("context ", DiagnosticLimit) + target
		got := Diagnostic(raw)
		if !strings.Contains(got, target) {
			t.Errorf("Diagnostic() dropped production-valid empty port %q: %q", target, got)
		}
	}
}

func TestEveryAcceptedDiagnosticURLCandidateReducesAsAURL(t *testing.T) {
	longPath := "https://mullion.localhost/" +
		strings.Repeat("a", URLLimit) + "%2Ftail?secret=value#fragment"
	for _, run := range []string{
		"https://mullion.localhost/app.js?secret=value#fragment",
		"http://192.0.2.1:8080/a%2Fb",
		"https://[2001:db8::1]:443/app.js",
		"https://alice:hunter2@mullion.local" + "host/app.js",
		longPath,
		"https://example.invalid:/app.js",
		"https://[2001:db8::1]:/app.js",
	} {
		candidate, ok := diagnosticURLCandidate(run, 0)
		if !ok {
			t.Fatalf("diagnosticURLCandidate(%q) rejected a valid URL", run)
		}
		if got := reduceRun(candidate, 0); !hasHTTPPrefix(got) {
			t.Fatalf("accepted candidate %q reduced to non-URL %q", candidate, got)
		}
	}

	for _, run := range []string{
		"https://[bad/path",
		"https://host:bad/path",
		"https://host/%zz",
		"https://" + strings.Repeat("a", URLLimit) + "/path",
		"https://host/" + strings.Repeat("a", URLLimit) + "%zz",
	} {
		if candidate, ok := diagnosticURLCandidate(run, 0); ok {
			t.Fatalf("diagnosticURLCandidate(%q) accepted %q", run, candidate)
		}
	}
}
