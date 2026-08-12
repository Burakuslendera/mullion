//go:build windows

package backdrop

import (
	"errors"
	"testing"
)

// The message-loop cleanup owns every exit after CreateWindowEx. A GetMessage
// error and a WM_QUIT posted without WM_DESTROY both leave activeBackdrop equal
// to the created HWND, so they must destroy and then drain. The ordinary
// WM_DESTROY path clears activeBackdrop first and must not destroy the stale,
// potentially recycled value again.
func TestCleanupBackdropWindowOwnsEveryLoopExitWithoutDoubleDestroy(t *testing.T) {
	const hwnd = uintptr(0x1234)
	tests := []struct {
		name          string
		active        uintptr
		wantDestroyed bool
	}{
		{name: "GetMessage minus one", active: hwnd, wantDestroyed: true},
		{name: "external WM_QUIT zero", active: hwnd, wantDestroyed: true},
		{name: "normal WM_DESTROY then zero", active: 0, wantDestroyed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			active := test.active
			var order []string
			cleanupBackdropWindow(hwnd, &active,
				func(got uintptr) {
					if got != hwnd {
						t.Fatalf("destroy HWND = %#x, want %#x", got, hwnd)
					}
					order = append(order, "destroy")
				},
				func() { order = append(order, "drain") },
			)
			if test.wantDestroyed {
				if len(order) != 2 || order[0] != "destroy" || order[1] != "drain" {
					t.Fatalf("cleanup order = %v, want [destroy drain]", order)
				}
				if active != 0 {
					t.Fatalf("active HWND = %#x after cleanup, want zero", active)
				}
			} else if len(order) != 0 {
				t.Fatalf("normal destruction cleanup = %v, want no second destroy", order)
			}
		})
	}
}

func TestArmBackdropWatchClearsStaleTargetAndCommitsOnlyAfterTimerSuccess(t *testing.T) {
	timerFailure := errors.New("timer failed")
	tests := []struct {
		name       string
		target     uintptr
		armErr     error
		wantArms   int
		wantTarget uintptr
	}{
		{name: "no target", target: 0, wantTarget: 0},
		{name: "timer failure", target: 0x2222, armErr: timerFailure, wantArms: 1, wantTarget: 0},
		{name: "timer success", target: 0x3333, wantArms: 1, wantTarget: 0x3333},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			watched := uintptr(0x1111)
			var arms int
			err := armBackdropWatch(test.target, &watched, func() error {
				arms++
				return test.armErr
			})
			if !errors.Is(err, test.armErr) {
				t.Fatalf("armBackdropWatch error = %v, want %v", err, test.armErr)
			}
			if arms != test.wantArms {
				t.Fatalf("timer arms = %d, want %d", arms, test.wantArms)
			}
			if watched != test.wantTarget {
				t.Fatalf("watched target = %#x, want %#x", watched, test.wantTarget)
			}
		})
	}
}

func TestClearBackdropWatchStateRejectsStaleAndLiveOwnership(t *testing.T) {
	const active = uintptr(0x1234)
	tests := []struct {
		name        string
		destroyed   uintptr
		active      uintptr
		wantActive  uintptr
		wantWatched uintptr
	}{
		{name: "different window", destroyed: 0x5678, active: active, wantActive: active, wantWatched: 0x9999},
		{name: "owned window", destroyed: active, active: active, wantActive: 0, wantWatched: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current, watched := test.active, uintptr(0x9999)
			clearBackdropWatchState(test.destroyed, &current, &watched)
			if current != test.wantActive || watched != test.wantWatched {
				t.Fatalf("state after destroy = active:%#x watched:%#x, want active:%#x watched:%#x", current, watched, test.wantActive, test.wantWatched)
			}
		})
	}
}
