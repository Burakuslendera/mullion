//go:build windows

package host

import "testing"

// TestInsetForAutoHideEdges locks the 1px auto-hide reveal inset (docs/decisions/0015).
// The detection (SHAppBarMessage) and the actual taskbar reveal are Win32/live-only and
// are verified on a real machine; this pins the geometry the maximized paths depend on.
//
// The no-edge case is the load-bearing regression guard: it must be the identity, so a
// monitor with a visible taskbar or none maximizes byte-for-byte as before. If the inset
// ever leaks onto an edge with no auto-hide bar, or the per-edge math drops a pixel, one
// of these cases fails instead of a user losing a pixel off every maximize.
func TestInsetForAutoHideEdges(t *testing.T) {
	monitor := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}

	cases := []struct {
		name  string
		edges autoHideEdges
		want  rect
	}{
		{"no auto-hide bar is the identity", autoHideEdges{}, monitor},
		{"bottom bar insets the bottom only", autoHideEdges{bottom: true}, rect{0, 0, 1920, 1079}},
		{"top bar insets the top only", autoHideEdges{top: true}, rect{0, 1, 1920, 1080}},
		{"left bar insets the left only", autoHideEdges{left: true}, rect{1, 0, 1920, 1080}},
		{"right bar insets the right only", autoHideEdges{right: true}, rect{0, 0, 1919, 1080}},
		{"all four edges inset all four", autoHideEdges{true, true, true, true}, rect{1, 1, 1919, 1079}},
	}
	for _, c := range cases {
		if got := insetForAutoHideEdges(monitor, c.edges); got != c.want {
			t.Errorf("%s: insetForAutoHideEdges = %#v, want %#v", c.name, got, c.want)
		}
	}

	// A monitor-relative origin, not just {0,0}: the inset must move the edge, never
	// the opposite one. Bottom bar on a secondary monitor at (2560,0).
	secondary := rect{Left: 2560, Top: 0, Right: 4480, Bottom: 1080}
	if got := insetForAutoHideEdges(secondary, autoHideEdges{bottom: true}); got != (rect{2560, 0, 4480, 1079}) {
		t.Errorf("secondary monitor bottom bar: inset = %#v, want bottom-1 only", got)
	}

	// Inversion guard: a 1px inset that would collapse a degenerate area returns the
	// input unchanged so the caller's clamp - not a negative extent - rejects it.
	thin := rect{Left: 0, Top: 0, Right: 1, Bottom: 1}
	if got := insetForAutoHideEdges(thin, autoHideEdges{right: true, bottom: true}); got != thin {
		t.Errorf("degenerate area must not invert: got %#v, want %#v", got, thin)
	}
}
