//go:build windows

package host

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

// fakeConsoleWriter satisfies the Fd() probe isTerminal uses without being a
// real console; the seamed console-mode calls decide everything else, so these
// tests run headlessly on any machine (decision 0006).
type fakeConsoleWriter struct{}

func (fakeConsoleWriter) Write(p []byte) (int, error) { return len(p), nil }
func (fakeConsoleWriter) Fd() uintptr                 { return 0x11 }

func stubConsoleModes(t *testing.T, get func(windows.Handle, *uint32) error, set func(windows.Handle, uint32) error) {
	t.Helper()
	origGet, origSet := getConsoleMode, setConsoleMode
	getConsoleMode, setConsoleMode = get, set
	t.Cleanup(func() { getConsoleMode, setConsoleMode = origGet, origSet })
}

// TestIsTerminalDegradesWhenVTCannotBeEnabled locks the issue #28 contract: a
// legacy console answers GetConsoleMode but refuses the VT bit, and emitting
// SGR there prints the escapes verbatim - so colour must report unavailable
// and the logger fall back to plain text. The pre-fix code discarded the
// SetConsoleMode result and returned true unconditionally, which is exactly
// the line this test rejects.
func TestIsTerminalDegradesWhenVTCannotBeEnabled(t *testing.T) {
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = 0x3; return nil },
		func(windows.Handle, uint32) error { return errors.New("legacy console") },
	)
	if isTerminal(fakeConsoleWriter{}) {
		t.Fatal("isTerminal = true on a console that cannot enable VT; raw SGR escapes would print verbatim (issue #28)")
	}
}

// TestIsTerminalEnablesVTWhenMissing pins the successful enable: colour is on,
// and the mode requested keeps the console's existing bits alongside VT.
func TestIsTerminalEnablesVTWhenMissing(t *testing.T) {
	var requested uint32
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = 0x3; return nil },
		func(_ windows.Handle, mode uint32) error { requested = mode; return nil },
	)
	if !isTerminal(fakeConsoleWriter{}) {
		t.Fatal("isTerminal = false although VT was enabled successfully")
	}
	if want := uint32(0x3 | enableVirtualTerminalProcessing); requested != want {
		t.Fatalf("SetConsoleMode requested 0x%x, want 0x%x (existing bits plus VT)", requested, want)
	}
}

// TestIsTerminalKeepsVTWhenAlreadyEnabled pins the common modern-console case:
// VT already on, colour on, and the console mode is left untouched.
func TestIsTerminalKeepsVTWhenAlreadyEnabled(t *testing.T) {
	setCalls := 0
	stubConsoleModes(t,
		func(_ windows.Handle, mode *uint32) error { *mode = enableVirtualTerminalProcessing; return nil },
		func(windows.Handle, uint32) error { setCalls++; return nil },
	)
	if !isTerminal(fakeConsoleWriter{}) {
		t.Fatal("isTerminal = false on a console with VT already enabled")
	}
	if setCalls != 0 {
		t.Fatalf("SetConsoleMode called %d times although VT was already on, want 0", setCalls)
	}
}

// TestIsTerminalRejectsNonConsoleHandle pins the file-or-pipe case: the mode
// query fails, colour is off, and no mode change is attempted.
func TestIsTerminalRejectsNonConsoleHandle(t *testing.T) {
	setCalls := 0
	stubConsoleModes(t,
		func(windows.Handle, *uint32) error { return errors.New("not a console") },
		func(windows.Handle, uint32) error { setCalls++; return nil },
	)
	if isTerminal(fakeConsoleWriter{}) {
		t.Fatal("isTerminal = true for a non-console handle")
	}
	if setCalls != 0 {
		t.Fatalf("SetConsoleMode called %d times for a non-console, want 0", setCalls)
	}
}
