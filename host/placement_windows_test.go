//go:build windows

package host

import (
	"strings"
	"testing"
)

// The placement contract (issue #59, decision 0018): Config.Width/Height are
// logical pixels, so the physical creation size is scaled by the target
// monitor's DPI, and the window is centered in the monitor's work area - not
// its full rect, which would sit the bottom edge under the taskbar. These
// tests pin the pure math; which monitor is asked, and what the window really
// looks like there, is the live checklist's half (docs/verification.md).

func TestCenteredPlacementIsIdentityAt96DPI(t *testing.T) {
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 96, 980, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: 470, Y: 190, Width: 980, Height: 640, DPI: 96}
	if got != want {
		t.Fatalf("centeredPlacement(96) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementScalesTheLogicalSizeByTheMonitorDPI(t *testing.T) {
	// 125% (dpi 120): 980x640 logical is 1225x800 physical, centered in the
	// 1920x1020 work area. These are the numbers issue #59 measured live as
	// wrong - the window came out 980x640 physical at the cascade position.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 120, 980, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: 347, Y: 110, Width: 1225, Height: 800, DPI: 120}
	if got != want {
		t.Fatalf("centeredPlacement(120) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementCentersOnANegativeOriginWorkArea(t *testing.T) {
	// A secondary monitor left of the primary has negative screen coordinates
	// (this machine's sits at -1920,0). The work-area origin must carry through
	// the centering, not be assumed zero.
	work := rect{Left: -1920, Top: 0, Right: 0, Bottom: 1032}
	got, ok := centeredPlacement(work, 96, 980, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: -1450, Y: 196, Width: 980, Height: 640, DPI: 96}
	if got != want {
		t.Fatalf("centeredPlacement(negative origin) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementTreatsZeroDPIAsTheDefault(t *testing.T) {
	// A zero DPI must degrade to the default, never scale the window to zero -
	// the same guard rasterizationScaleForDPI applies for the same reason.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 0, 980, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	if got.Width != 980 || got.Height != 640 || got.DPI != defaultWindowDPI {
		t.Fatalf("centeredPlacement(dpi=0) = %#v, want the unscaled size at dpi %d", got, defaultWindowDPI)
	}
}

func TestCenteredPlacementClampsAnOversizedWindowToTheWorkArea(t *testing.T) {
	// 1600x1200 logical at 150% asks for 2400x1800 physical inside 1920x1020:
	// the size clamps to the work area and the origin lands flush with it,
	// instead of centering a rect that hangs off every edge.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 144, 1600, 1200)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: 0, Y: 0, Width: 1920, Height: 1020, DPI: 144}
	if got != want {
		t.Fatalf("centeredPlacement(oversized) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementClampsAnInt32OverflowingScale(t *testing.T) {
	// Issue #61, negative band: 1_800_000_000 logical at 125% scales to
	// 2.25e9, past int32. The naive post-truncation clamp let the wrapped
	// negative through (observed live: "initial placement, x=1022484608,
	// width=-2044967296" and a silent 166px window at x=32767). The int64
	// clamp must land on the work area instead.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 120, 1_800_000_000, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: 0, Y: 110, Width: 1920, Height: 800, DPI: 120}
	if got != want {
		t.Fatalf("centeredPlacement(overflow) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementClampsThePositiveWrapBand(t *testing.T) {
	// Issue #61, second band (only above 200% scale): 1_717_987_118 at 250%
	// is 4_294_967_795, which truncates to 499 - a silently tiny window that
	// a post-truncation `width <= 0` guard would NOT catch. Only clamping the
	// exact int64 value before truncation closes this band.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := centeredPlacement(work, 240, 1_717_987_118, 640)
	if !ok {
		t.Fatal("centeredPlacement ok = false")
	}
	want := initialPlacement{X: 0, Y: 0, Width: 1920, Height: 1020, DPI: 240}
	if got != want {
		t.Fatalf("centeredPlacement(positive wrap) = %#v, want %#v", got, want)
	}
}

func TestCenteredPlacementRejectsAZeroScaledSize(t *testing.T) {
	// A degenerate effective DPI (a broken driver reporting 1) can scale a
	// small logical length to zero. That must reject into the CW_USEDEFAULT
	// fallback, not create a zero-width window with ok=true.
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	if got, ok := centeredPlacement(work, 1, 50, 640); ok {
		t.Fatalf("centeredPlacement(zero-scaled) = %#v, ok = true, want false", got)
	}
}

func TestCenteredPlacementRejectsDegenerateInput(t *testing.T) {
	cases := map[string]struct {
		work               rect
		logicalW, logicalH int32
	}{
		"empty work area":    {rect{}, 980, 640},
		"inverted work area": {rect{Left: 100, Top: 100, Right: 50, Bottom: 50}, 980, 640},
		"zero width":         {rect{Right: 1920, Bottom: 1020}, 0, 640},
		"zero height":        {rect{Right: 1920, Bottom: 1020}, 980, 0},
	}
	for name, testCase := range cases {
		if _, ok := centeredPlacement(testCase.work, 96, testCase.logicalW, testCase.logicalH); ok {
			t.Errorf("%s: centeredPlacement ok = true, want false", name)
		}
	}
}

func TestFormatInitialPlacementLogCarriesEveryMetric(t *testing.T) {
	line := formatInitialPlacementLog(initialPlacement{X: 347, Y: 110, Width: 1225, Height: 800, DPI: 120})
	for _, part := range []string{"x=347", "y=110", "width=1225", "height=800", "dpi=120"} {
		if !strings.Contains(line, part) {
			t.Fatalf("placement log %q is missing %q", line, part)
		}
	}
}
