package host

import (
	"bytes"
	"strings"
	"testing"
)

func TestColourLineWrapsAndResets(t *testing.T) {
	got := colourLine(ansiBoldRed, "boom", true)
	if !strings.HasPrefix(got, ansiBoldRed) {
		t.Fatalf("coloured line missing the SGR prefix: %q", got)
	}
	if !strings.HasSuffix(got, ansiReset+"\n") {
		t.Fatalf("coloured line not reset+newline terminated: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Fatalf("coloured line dropped the message: %q", got)
	}
}

func TestColourLinePlainWhenDisabled(t *testing.T) {
	got := colourLine(ansiBoldRed, "boom", false)
	if got != "boom\n" {
		t.Fatalf("plain line = %q, want %q", got, "boom\n")
	}
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("plain line leaked an escape byte: %q", got)
	}
}

// TestColourLoggerToNonTerminalIsPlain locks the invariant that a redirected
// sink (a file or a pipe - here a bytes.Buffer, which is not a terminal) never
// receives escape sequences, so a captured log stays clean.
func TestColourLoggerToNonTerminalIsPlain(t *testing.T) {
	var buf bytes.Buffer
	log := ColourLogger(&buf)
	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	out := buf.String()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("non-terminal sink got escape sequences: %q", out)
	}
	for _, want := range []string{"d\n", "i\n", "w\n", "e\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q in %q", want, out)
		}
	}
}

func TestColourLoggerNilWriterIsNop(t *testing.T) {
	if _, ok := ColourLogger(nil).(NopLogger); !ok {
		t.Fatal("ColourLogger(nil) should be a NopLogger")
	}
}
