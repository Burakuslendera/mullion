//go:build windows

package host

import "testing"

func TestNCCalcClientRectAddsRestoredFrameCompensation(t *testing.T) {
	target := rect{Left: 10, Top: 20, Right: 910, Bottom: 640}
	got := applyRestoredClientFrameCompensation(target)
	want := rect{Left: 10, Top: 20, Right: 910, Bottom: 641}
	if got != want {
		t.Fatalf("applyRestoredClientFrameCompensation() = %#v, want %#v", got, want)
	}
}

func TestNCCalcClientRectClampsMaximizedRectToWorkArea(t *testing.T) {
	target := rect{Left: -8, Top: -8, Right: 1930, Bottom: 1040}
	workArea := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1020}
	got, ok := clampRectToArea(target, workArea)
	if !ok {
		t.Fatal("clampRectToArea(maximized) ok = false")
	}
	if got != workArea {
		t.Fatalf("clampRectToArea(maximized) = %#v, want %#v", got, workArea)
	}
}

func TestNCCalcClientRectRejectsInvalidMaximizedClamp(t *testing.T) {
	target := rect{Left: 50, Top: 50, Right: 60, Bottom: 60}
	workArea := rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	if _, ok := clampRectToArea(target, workArea); ok {
		t.Fatal("clampRectToArea(invalid maximized) ok = true, want false")
	}
}
