//go:build windows

package host

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// fakeConsoleWriter satisfies the Fd() probe isTerminal uses without being a
// real console; the seamed console-mode calls decide everything else, so these
// tests run headlessly on any machine (decision 0006). Writes are captured so
// the degradation notice is observable.
type fakeConsoleWriter struct {
	bytes.Buffer
}

func (*fakeConsoleWriter) Fd() uintptr { return 0x11 }

func stubConsoleModes(t *testing.T, get func(windows.Handle, *uint32) error, set func(windows.Handle, uint32) error) {
	t.Helper()
	origGet, origSet := getConsoleMode, setConsoleMode
	getConsoleMode, setConsoleMode = get, set
	t.Cleanup(func() { getConsoleMode, setConsoleMode = origGet, origSet })
}

func stubLegacyConsole(t *testing.T) {
	t.Helper()
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = 0x3; return nil },
		func(windows.Handle, uint32) error { return errors.New("legacy console") },
	)
}

// TestIsTerminalDegradesWhenVTCannotBeEnabled locks the issue #28 contract: a
// legacy console answers GetConsoleMode but refuses the VT bit, and emitting
// SGR there prints the escapes verbatim - so colour must report unavailable
// (and the refusal flagged, so ColourLogger can announce it). The pre-fix code
// discarded the SetConsoleMode result and returned true unconditionally, which
// is exactly the line this test rejects.
func TestIsTerminalDegradesWhenVTCannotBeEnabled(t *testing.T) {
	stubLegacyConsole(t)
	colour, vtRefused := isTerminal(&fakeConsoleWriter{})
	if colour {
		t.Fatal("isTerminal colour = true on a console that cannot enable VT; raw SGR escapes would print verbatim (issue #28)")
	}
	if !vtRefused {
		t.Fatal("isTerminal vtRefused = false on a VT refusal; the degradation would stay silent")
	}
}

// TestIsTerminalEnablesVTWhenMissing pins the successful enable: colour is on,
// no refusal, and the mode requested keeps the console's existing bits
// alongside VT.
func TestIsTerminalEnablesVTWhenMissing(t *testing.T) {
	var requested uint32
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = 0x3; return nil },
		func(_ windows.Handle, mode uint32) error { requested = mode; return nil },
	)
	colour, vtRefused := isTerminal(&fakeConsoleWriter{})
	if !colour {
		t.Fatal("isTerminal colour = false although VT was enabled successfully")
	}
	if vtRefused {
		t.Fatal("isTerminal vtRefused = true although the enable succeeded")
	}
	if want := uint32(0x3 | enableVirtualTerminalProcessing); requested != want {
		t.Fatalf("SetConsoleMode requested 0x%x, want 0x%x (existing bits plus VT)", requested, want)
	}
}

// TestIsTerminalKeepsVTWhenAlreadyEnabled pins the common modern-console case:
// VT already on, colour on, no refusal, and the console mode is left
// untouched.
func TestIsTerminalKeepsVTWhenAlreadyEnabled(t *testing.T) {
	setCalls := 0
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = enableVirtualTerminalProcessing; return nil },
		func(windows.Handle, uint32) error { setCalls++; return nil },
	)
	colour, vtRefused := isTerminal(&fakeConsoleWriter{})
	if !colour {
		t.Fatal("isTerminal colour = false on a console with VT already enabled")
	}
	if vtRefused {
		t.Fatal("isTerminal vtRefused = true although VT was already on")
	}
	if setCalls != 0 {
		t.Fatalf("SetConsoleMode called %d times although VT was already on, want 0", setCalls)
	}
}

// TestIsTerminalRejectsNonConsoleHandle pins the file-or-pipe case: the mode
// query fails, colour is off, no refusal is reported - redirection is normal,
// not a degradation - and no mode change is attempted.
func TestIsTerminalRejectsNonConsoleHandle(t *testing.T) {
	setCalls := 0
	stubConsoleModes(t,
		func(windows.Handle, *uint32) error { return errors.New("not a console") },
		func(windows.Handle, uint32) error { setCalls++; return nil },
	)
	colour, vtRefused := isTerminal(&fakeConsoleWriter{})
	if colour {
		t.Fatal("isTerminal colour = true for a non-console handle")
	}
	if vtRefused {
		t.Fatal("isTerminal vtRefused = true for a non-console handle; a pipe is not a refusal")
	}
	if setCalls != 0 {
		t.Fatalf("SetConsoleMode called %d times for a non-console, want 0", setCalls)
	}
}

const colourDisabledNotice = "mullion: colour disabled, reason=console cannot enable virtual terminal processing"

// unsetNoColour clears an ambient NO_COLOR for the duration of the test.
// enableColour short-circuits on the variable's mere presence (empty value
// included, per no-color.org), so t.Setenv("NO_COLOR", "") would not help - the
// variable must be genuinely absent for the console probe to run at all.
func unsetNoColour(t *testing.T) {
	t.Helper()
	value, ok := os.LookupEnv("NO_COLOR")
	if !ok {
		return
	}
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("NO_COLOR", value) })
}

// TestColourLoggerAnnouncesVTRefusalOnce locks the degradation notice: on a
// console that refused VT, construction emits exactly one plain WARN line
// saying colour is disabled - without it the colours vanish with no trace of
// why - and the logger's own lines stay plain and escape-free. Two writes
// follow the construction, so a notice wrongly emitted per write - instead of
// once per logger - fails the exactly-one count.
func TestColourLoggerAnnouncesVTRefusalOnce(t *testing.T) {
	unsetNoColour(t)
	stubLegacyConsole(t)
	w := &fakeConsoleWriter{}
	log := ColourLogger(w)
	log.Error("boom")
	log.Info("still here")

	out := w.String()
	if got := strings.Count(out, colourDisabledNotice); got != 1 {
		t.Fatalf("colour-disabled notice appeared %d times, want exactly 1:\n%q", got, out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("legacy-console output carries escape sequences: %q", out)
	}
	if !strings.Contains(out, "boom\n") || !strings.Contains(out, "still here\n") {
		t.Fatalf("log lines missing from output: %q", out)
	}
}

// TestColourLoggerStaysSilentWhenNoColourIsExplicit pins the boundary of the
// notice: NO_COLOR is the user's explicit choice, not a degradation, so even a
// console that would refuse VT gets no notice - NO_COLOR short-circuits before
// any console probe.
func TestColourLoggerStaysSilentWhenNoColourIsExplicit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	stubLegacyConsole(t)
	w := &fakeConsoleWriter{}
	ColourLogger(w).Info("hello")

	out := w.String()
	if strings.Contains(out, colourDisabledNotice) {
		t.Fatalf("NO_COLOR sink got the degradation notice: %q", out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("NO_COLOR sink got escape sequences: %q", out)
	}
}

// TestColourLoggerStaysSilentForNonTerminalSink pins the other boundary: a
// pipe or a file is normal redirection, so a captured log never carries the
// notice.
func TestColourLoggerStaysSilentForNonTerminalSink(t *testing.T) {
	unsetNoColour(t)
	stubConsoleModes(t,
		func(windows.Handle, *uint32) error { return errors.New("not a console") },
		func(windows.Handle, uint32) error { return nil },
	)
	w := &fakeConsoleWriter{}
	ColourLogger(w).Info("hello")

	if out := w.String(); strings.Contains(out, colourDisabledNotice) {
		t.Fatalf("non-terminal sink got the degradation notice: %q", out)
	}
}
