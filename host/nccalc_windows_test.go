//go:build windows

package host

import "testing"

func TestNCCalcClientRectAddsRestoredFrameCompensation(t *testing.T) {
	target := rect{Left: 10, Top: 20, Right: 910, Bottom: 640}
	got, ok := nccalcClientRect(target, rect{}, false)
	if !ok {
		t.Fatal("nccalcClientRect(restored) ok = false")
	}
	want := rect{Left: 10, Top: 20, Right: 910, Bottom: 641}
	if got != want {
		t.Fatalf("nccalcClientRect(restored) = %#v, want %#v", got, want)
	}
}

func TestNCCalcClientRectClampsMaximizedRectToWorkArea(t *testing.T) {
	target := rect{Left: -8, Top: -8, Right: 1930, Bottom: 1040}
	workArea := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := nccalcClientRect(target, workArea, true)
	if !ok {
		t.Fatal("nccalcClientRect(maximized) ok = false")
	}
	if got != workArea {
		t.Fatalf("nccalcClientRect(maximized) = %#v, want %#v", got, workArea)
	}
}

func TestNCCalcClientRectRejectsInvalidMaximizedClamp(t *testing.T) {
	target := rect{Left: 50, Top: 50, Right: 60, Bottom: 60}
	workArea := rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	if _, ok := nccalcClientRect(target, workArea, true); ok {
		t.Fatal("nccalcClientRect(invalid maximized) ok = true, want false")
	}
}

// TestInsetForAutoHideEdgesReservesASliverPerEdge locks the auto-hide taskbar
// inset (docs/decisions/0015): a maximized client rect on a monitor with an
// auto-hide taskbar must give up exactly one pixel on that taskbar's edge - and
// only that edge - so the shell keeps revealing the taskbar on hover. A no-op
// implementation (the behaviour before the fix) fails this test.
func TestInsetForAutoHideEdgesReservesASliverPerEdge(t *testing.T) {
	full := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}

	if got := insetForAutoHideEdges(full, autoHideEdges{}); got != full {
		t.Fatalf("insetForAutoHideEdges(no edges) = %#v, want it unchanged %#v", got, full)
	}

	cases := []struct {
		name  string
		edges autoHideEdges
		want  rect
	}{
		{"bottom", autoHideEdges{Bottom: true}, rect{Left: 0, Top: 0, Right: 1920, Bottom: 1079}},
		{"top", autoHideEdges{Top: true}, rect{Left: 0, Top: 1, Right: 1920, Bottom: 1080}},
		{"left", autoHideEdges{Left: true}, rect{Left: 1, Top: 0, Right: 1920, Bottom: 1080}},
		{"right", autoHideEdges{Right: true}, rect{Left: 0, Top: 0, Right: 1919, Bottom: 1080}},
	}
	for _, c := range cases {
		if got := insetForAutoHideEdges(full, c.edges); got != c.want {
			t.Fatalf("insetForAutoHideEdges(%s) = %#v, want %#v", c.name, got, c.want)
		}
	}
}

// TestInsetForAutoHideEdgesLeavesADegenerateRectAlone guards the inset against
// inverting a rect too small to give a pixel up - a maximized rect never is, but
// the guard keeps a degenerate input from producing an inverted rect.
func TestInsetForAutoHideEdgesLeavesADegenerateRectAlone(t *testing.T) {
	tiny := rect{Left: 0, Top: 0, Right: 1, Bottom: 1}
	all := autoHideEdges{Left: true, Top: true, Right: true, Bottom: true}
	if got := insetForAutoHideEdges(tiny, all); got != tiny {
		t.Fatalf("insetForAutoHideEdges(tiny) = %#v, want it unchanged %#v", got, tiny)
	}
}
