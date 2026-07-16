//go:build windows

package webview2

import (
	"strings"
	"testing"
)

// TestSanitizeVersionStripsControlBytesButKeepsValid locks the boundary defence:
// a poisoned registry pv (unprivileged-writable HKCU) that smuggles terminal
// escape bytes is cleaned before it can reach the startup log or `mullion
// doctor`, while a legitimate version - digits, dots, an optional channel word -
// is preserved exactly.
func TestSanitizeVersionStripsControlBytesButKeepsValid(t *testing.T) {
	got := sanitizeVersion("9999.0.0.0\x1b]0;pwned\x07\x1b[2K")
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("sanitizeVersion kept control byte %#x: %q", r, got)
		}
	}
	if !strings.HasPrefix(got, "9999.0.0.0") {
		t.Fatalf("sanitizeVersion dropped the version digits: %q", got)
	}
	for _, valid := range []string{"150.0.4078.65", "94.0.992.31 dev"} {
		if got := sanitizeVersion(valid); got != valid {
			t.Fatalf("sanitizeVersion(%q) = %q, want it unchanged", valid, got)
		}
	}
}
