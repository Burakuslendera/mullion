//go:build windows

package host

import "testing"

// The WM_GETMINMAXINFO clamp is unconditional - a client-extended (frameless)
// window would otherwise maximize over the taskbar. This only pins the guard on a
// bad MINMAXINFO pointer; the geometry itself is covered by the WM_NCCALCSIZE
// tests.
func TestApplyMonitorWorkAreaRejectsInvalidPointer(t *testing.T) {
	if New(Config{}).applyMonitorWorkArea(0, 0) {
		t.Fatal("applyMonitorWorkArea must not clamp with an invalid MINMAXINFO pointer, got true")
	}
}

// Mixed-DPI transition contract: on WM_DPICHANGED the size applied is the extent
// of the rect Windows suggested, verbatim. No second DPI factor is layered on top
// - under Per-Monitor-V2 the suggested rect is already scaled, so an extra multiply
// double-scales and compounds on every monitor hop. These tests pin that contract
// for the paths that would expose it: dragging across monitors, straddling two
// monitors, maximize plus Win+Shift+Arrow, and a programmatic move.

func TestDPIChangedTargetSizeIsSuggestedVerbatim(t *testing.T) {
	// Across a 96->120 transition Windows suggests 720x496 -> 900x620, and that is
	// exactly what gets applied: the function must return the suggested extent
	// unchanged. Adding a `* dpi / 96` on top breaks this test.
	cases := []struct {
		name         string
		suggested    rect
		wantW, wantH int32
		wantOK       bool
	}{
		{"96dpi restored", rect{Left: -1520, Top: 200, Right: -800, Bottom: 696}, 720, 496, true},
		{"120dpi restored", rect{Left: 200, Top: 120, Right: 1100, Bottom: 740}, 900, 620, true},
		{"maximized 120", rect{Left: -8, Top: -8, Right: 1928, Bottom: 1040}, 1936, 1048, true},
		{"degenerate width", rect{Left: 0, Top: 0, Right: 0, Bottom: 400}, 0, 400, false},
		{"degenerate height", rect{Left: 0, Top: 0, Right: 400, Bottom: 0}, 400, 0, false},
		{"negative", rect{Left: 100, Top: 100, Right: 50, Bottom: 50}, -50, -50, false},
	}
	for _, tc := range cases {
		gotW, gotH, gotOK := dpiChangedTargetSize(tc.suggested)
		if gotW != tc.wantW || gotH != tc.wantH || gotOK != tc.wantOK {
			t.Fatalf("%s: dpiChangedTargetSize = (%d,%d,%v), want (%d,%d,%v)",
				tc.name, gotW, gotH, gotOK, tc.wantW, tc.wantH, tc.wantOK)
		}
	}
}

func TestDPIRescaleModelMatchesObservedTransition(t *testing.T) {
	// Values for a 96/120 monitor pair: 720@96 -> 900@120,
	// 496@96 -> 620@120.
	if got := dpiRescaleLength(720, 96, 120); got != 900 {
		t.Fatalf("dpiRescaleLength(720,96,120) = %d, want 900", got)
	}
	if got := dpiRescaleLength(496, 96, 120); got != 620 {
		t.Fatalf("dpiRescaleLength(496,96,120) = %d, want 620", got)
	}
	if got := dpiRescaleLength(900, 120, 96); got != 720 {
		t.Fatalf("dpiRescaleLength(900,120,96) = %d, want 720", got)
	}
	// A zero source DPI falls back to the default instead of dividing by zero.
	if got := dpiRescaleLength(720, 0, 96); got != 720 {
		t.Fatalf("dpiRescaleLength(720,0,96) = %d, want 720", got)
	}
}

func TestDPITransitionRoundTripIsLossless(t *testing.T) {
	// A round trip (96->120->96) must land back on the starting size: no hysteresis.
	for _, start := range []int32{720, 900, 1200, 600} {
		up := dpiRescaleLength(start, 96, 120)
		back := dpiRescaleLength(up, 120, 96)
		if back != start {
			t.Fatalf("round-trip 96->120->96 for %d: got %d (up=%d), want %d", start, back, up, start)
		}
	}
}

func TestDPITransitionNoCompoundingAcrossRepeats(t *testing.T) {
	// Shuttling back and forth (96<->120, three times) must not grow the window:
	// every visit to 120 yields the same size, every visit to 96 the same. If the
	// scale factor compounded, the values would drift upward with each pass.
	width96 := int32(720)
	for i := 0; i < 3; i++ {
		width120 := dpiRescaleLength(width96, 96, 120)
		if width120 != 900 {
			t.Fatalf("shuttle %d: mon120 width = %d, want 900 (no compounding)", i, width120)
		}
		width96 = dpiRescaleLength(width120, 120, 96)
		if width96 != 720 {
			t.Fatalf("shuttle %d: mon96 width = %d, want 720 (no hysteresis)", i, width96)
		}
	}
}

// The WebView2 content scale (devicePixelRatio) is DPI/96: the runtime's own
// monitor-scale detection is off, so the host owns this value and must set it on
// every DPI change. These pin the mapping and, more importantly, that it depends
// only on the absolute current DPI - which is what stops it compounding across
// monitor hops the way a delta-based scale would. The scales are exact in float64
// (120/96 = 1.25, etc.), so == is safe.

func TestRasterizationScaleForDPIMatchesMonitorScale(t *testing.T) {
	cases := []struct {
		name string
		dpi  uint32
		want float64
	}{
		{"96 = 100%", 96, 1.0},
		{"120 = 125% (the bug report's primary monitor)", 120, 1.25},
		{"144 = 150%", 144, 1.5},
		{"192 = 200%", 192, 2.0},
		{"zero falls back to the default, not a 0 scale", 0, 1.0},
	}
	for _, tc := range cases {
		if got := rasterizationScaleForDPI(tc.dpi); got != tc.want {
			t.Fatalf("%s: rasterizationScaleForDPI(%d) = %v, want %v", tc.name, tc.dpi, got, tc.want)
		}
	}
}

func TestRasterizationScaleDependsOnlyOnCurrentDPI(t *testing.T) {
	// Shuttling 120<->96 three times must yield the same scale on every visit: the
	// value is a function of the absolute DPI, never of the previous scale, so it
	// cannot drift. This is the content-scale analogue of
	// TestDPITransitionNoCompoundingAcrossRepeats for the window rect.
	for i := 0; i < 3; i++ {
		if got := rasterizationScaleForDPI(120); got != 1.25 {
			t.Fatalf("visit %d to 120 DPI: scale = %v, want 1.25 (no compounding)", i, got)
		}
		if got := rasterizationScaleForDPI(96); got != 1.0 {
			t.Fatalf("visit %d to 96 DPI: scale = %v, want 1.0 (no hysteresis)", i, got)
		}
	}
}
