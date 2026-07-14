//go:build windows

package mullion

import "testing"

// Regression lock: a restored window used to show the maximized system menu,
// because DefWindowProc leaves stale item states behind. The correct rule is
// Restore greyed and Move/Size/Maximise enabled when restored, and the reverse
// when maximized.
func TestTabTitlebarSystemMenuStatesRestored(t *testing.T) {
	style := styleForNativeFrameProfile(activeNativeFrameProfile(), uintptr(wsOverlappedWindow))
	states := tabTitlebarSystemMenuItemStates(false, false, style)
	assertMenuState(t, states, scRestore, false, "restored: Restore must be greyed out")
	assertMenuState(t, states, scMove, true, "restored: Move must be enabled")
	assertMenuState(t, states, scSize, true, "restored: Size must be enabled (WS_THICKFRAME present)")
	assertMenuState(t, states, scMinimize, true, "restored: Minimise must be enabled")
	assertMenuState(t, states, scMaximize, true, "restored: Maximise must be enabled (WS_MAXIMIZEBOX present)")
	assertMenuState(t, states, scClose, true, "restored: Close must be enabled")
}

func TestTabTitlebarSystemMenuStatesMaximized(t *testing.T) {
	style := styleForNativeFrameProfile(activeNativeFrameProfile(), uintptr(wsOverlappedWindow))
	states := tabTitlebarSystemMenuItemStates(true, false, style)
	assertMenuState(t, states, scRestore, true, "maximized: Restore must be enabled")
	assertMenuState(t, states, scMove, false, "maximized: Move must be greyed out")
	assertMenuState(t, states, scSize, false, "maximized: Size must be greyed out")
	assertMenuState(t, states, scMinimize, true, "maximized: Minimise must be enabled")
	assertMenuState(t, states, scMaximize, false, "maximized: Maximise must be greyed out")
	assertMenuState(t, states, scClose, true, "maximized: Close must be enabled")
}

func TestTabTitlebarSystemMenuStatesIconic(t *testing.T) {
	style := styleForNativeFrameProfile(activeNativeFrameProfile(), uintptr(wsOverlappedWindow))
	states := tabTitlebarSystemMenuItemStates(false, true, style)
	assertMenuState(t, states, scRestore, true, "iconic: Restore must be enabled")
	assertMenuState(t, states, scMove, false, "iconic: Move must be greyed out")
	assertMenuState(t, states, scSize, false, "iconic: Size must be greyed out")
	assertMenuState(t, states, scMaximize, false, "iconic: Maximise must be greyed out")
}

// A missing style bit must grey the matching item even in the restored state: the
// menu may not offer an action the window is not styled to perform.
func TestTabTitlebarSystemMenuStatesRespectStyleBits(t *testing.T) {
	styleWithout := func(bit uintptr) uintptr {
		full := styleForNativeFrameProfile(activeNativeFrameProfile(), uintptr(wsOverlappedWindow))
		return full &^ bit
	}
	if states := tabTitlebarSystemMenuItemStates(false, false, styleWithout(uintptr(wsThickFrame))); states[scSize] {
		t.Error("Size must be greyed out when WS_THICKFRAME is absent, got enabled")
	}
	if states := tabTitlebarSystemMenuItemStates(false, false, styleWithout(uintptr(wsMaximizeBox))); states[scMaximize] {
		t.Error("Maximise must be greyed out when WS_MAXIMIZEBOX is absent, got enabled")
	}
	if states := tabTitlebarSystemMenuItemStates(false, false, styleWithout(uintptr(wsMinimizeBox))); states[scMinimize] {
		t.Error("Minimise must be greyed out when WS_MINIMIZEBOX is absent, got enabled")
	}
}

func TestTabTitlebarSystemMenuStatesCoverStandardItems(t *testing.T) {
	states := tabTitlebarSystemMenuItemStates(false, false, uintptr(wsOverlappedWindow))
	expected := []uintptr{scRestore, scMove, scSize, scMinimize, scMaximize, scClose}
	if len(states) != len(expected) {
		t.Fatalf("item count = %d, want %d", len(states), len(expected))
	}
	for _, command := range expected {
		if _, ok := states[command]; !ok {
			t.Errorf("standard sysmenu item missing: 0x%X", command)
		}
	}
}

func assertMenuState(t *testing.T, states map[uintptr]bool, command uintptr, want bool, message string) {
	t.Helper()
	got, ok := states[command]
	if !ok {
		t.Fatalf("sysmenu item 0x%X is not present in states", command)
	}
	if got != want {
		t.Error(message)
	}
}
