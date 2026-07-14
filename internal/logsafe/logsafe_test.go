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
