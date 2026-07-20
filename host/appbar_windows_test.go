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

// TestMaximizeMonitorInfoInsetsAutoHideEdges locks the wiring of maximizeMonitorInfo
// (docs/decisions/0015): the shell probe is asked about the window's monitor rect,
// its answer insets the work area, and the monitor rect itself is left untouched.
// TestInsetForAutoHideEdges pins the 1px arithmetic; this pins that the arithmetic
// is actually reached from the maximize-geometry paths, which was previously only a
// live observation. The seams stand in for the two Win32 queries per decision 0006.
func TestMaximizeMonitorInfoInsetsAutoHideEdges(t *testing.T) {
	// An auto-hide taskbar reserves no work area, so rcWork == rcMonitor - the exact
	// configuration 0015 exists for.
	monitor := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}

	origInfo := monitorInfoForWindow
	origEdges := autoHideEdgesForMonitor
	defer func() {
		monitorInfoForWindow = origInfo
		autoHideEdgesForMonitor = origEdges
	}()

	monitorInfoForWindow = func(windowHandle) (monitorInfo, bool) {
		return monitorInfo{Monitor: monitor, Work: monitor}, true
	}
	var probed []rect
	autoHideEdgesForMonitor = func(monitor rect) autoHideEdges {
		probed = append(probed, monitor)
		return autoHideEdges{bottom: true}
	}

	info, ok := maximizeMonitorInfo(0)
	if !ok {
		t.Fatal("maximizeMonitorInfo = !ok, want ok")
	}
	if want := (rect{Left: 0, Top: 0, Right: 1920, Bottom: 1079}); info.Work != want {
		t.Errorf("Work = %#v, want bottom inset by 1: %#v", info.Work, want)
	}
	if info.Monitor != monitor {
		t.Errorf("Monitor = %#v, want untouched %#v", info.Monitor, monitor)
	}
	if len(probed) != 1 || probed[0] != monitor {
		t.Errorf("shell probe rects = %#v, want exactly one probe of the monitor rect", probed)
	}

	// A failed monitor query must fail the whole lookup, never probe the shell.
	probed = nil
	monitorInfoForWindow = func(windowHandle) (monitorInfo, bool) { return monitorInfo{}, false }
	if _, ok := maximizeMonitorInfo(0); ok {
		t.Error("maximizeMonitorInfo = ok on failed monitor query, want !ok")
	}
	if len(probed) != 0 {
		t.Errorf("shell probed %d times on failed monitor query, want 0", len(probed))
	}
}
