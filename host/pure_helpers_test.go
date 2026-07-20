//go:build windows

package host

import (
	"strings"
	"testing"
)

// Locks for small pure helpers an audit found untested. Each is a genuine
// fails-before: reverting the helper's guarantee fails the corresponding case.

// clampSourceForLog bounds the untrusted rejected-source string in the #56 debug
// line: a foreign data:/blob: URI can be arbitrarily long, and only the first
// bytes identify it, so a source over the limit is cut and one at the limit is
// left alone. Dropping the cut would let the source produce an unbounded log line.
func TestClampSourceForLog(t *testing.T) {
	atLimit := strings.Repeat("a", 160)
	if got := clampSourceForLog(atLimit); got != atLimit {
		t.Fatalf("clampSourceForLog cut a source already at the 160-byte limit")
	}
	if got := clampSourceForLog(strings.Repeat("b", 300)); len(got) != 160 {
		t.Fatalf("clampSourceForLog(300 bytes) = len %d, want 160", len(got))
	}
}

// isHotBoundsSyncSource gates both the deferred-webview early return in
// syncWebViewBounds and the log dedup, so which sources are "hot" (the
// high-frequency resize/move messages) is load-bearing: adding or dropping a
// member silently changes hot-path behaviour.
func TestIsHotBoundsSyncSource(t *testing.T) {
	for _, source := range []string{"wm_size", "wm_move", "wm_moving", "wm_windowpos_changing", "wm_windowpos_changed"} {
		if !isHotBoundsSyncSource(source) {
			t.Errorf("isHotBoundsSyncSource(%q) = false, want true", source)
		}
	}
	for _, source := range []string{"wm_dpi_changed", "show", "restore", "maximize", "deferred_restore", "frontend_ready", ""} {
		if isHotBoundsSyncSource(source) {
			t.Errorf("isHotBoundsSyncSource(%q) = true, want false", source)
		}
	}
}

// shouldLogBoundsSync dedups the high-frequency bounds log: a hot source with
// unchanged dimensions is suppressed, but the first log, a dimension change, a
// mismatch, or any non-hot source always logs. Collapsing the dedup would flood
// the log on every WM_SIZE of a resize drag, and nothing else would catch it.
func TestShouldLogBoundsSyncDedupesHotSources(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true})

	if !host.shouldLogBoundsSync("wm_size", 800, 600, 800, 600, false) {
		t.Fatal("the first hot bounds log must not be suppressed")
	}
	if host.shouldLogBoundsSync("wm_size", 800, 600, 800, 600, false) {
		t.Fatal("a repeated hot bounds log with identical dims must be suppressed")
	}
	if !host.shouldLogBoundsSync("wm_size", 801, 600, 801, 600, false) {
		t.Fatal("a hot bounds log with changed dims must not be suppressed")
	}
	if !host.shouldLogBoundsSync("wm_size", 801, 600, 801, 600, true) {
		t.Fatal("a bounds mismatch must always log, even when the dims are unchanged")
	}
	if !host.shouldLogBoundsSync("restore", 801, 600, 801, 600, false) {
		t.Fatal("a non-hot source must always log")
	}
}

// cssColour renders alpha in CSS's 0..1 range so a translucent BackgroundColour
// stays translucent; emitting the raw 0..255 byte would read as fully opaque.
func TestCssColourRendersFractionalAlpha(t *testing.T) {
	if got := cssColour(Colour{R: 10, G: 20, B: 30, A: 128}); got != "rgba(10,20,30,0.5019607843137255)" {
		t.Fatalf("cssColour(half alpha) = %q", got)
	}
	if got := cssColour(Colour{R: 0, G: 0, B: 0, A: 255}); got != "rgba(0,0,0,1)" {
		t.Fatalf("cssColour(opaque) = %q, want rgba(0,0,0,1)", got)
	}
}

// contrastColour picks legible text for a background of unknown brightness. Only
// the dark-background branch is exercised elsewhere; the default white background
// must resolve to dark text or the error message renders unreadably light-on-white.
func TestContrastColourForLightBackground(t *testing.T) {
	if got := contrastColour(Colour{R: 255, G: 255, B: 255, A: 255}); got != "#1a1a1a" {
		t.Fatalf("contrastColour(white) = %q, want #1a1a1a", got)
	}
	if got := contrastColour(Colour{R: 30, G: 30, B: 30, A: 255}); got != "#f2f2f2" {
		t.Fatalf("contrastColour(dark) = %q, want #f2f2f2", got)
	}
}
