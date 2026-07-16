package logsafe

import (
	"errors"
	"strings"
	"testing"
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
