//go:build windows

package backdrop

import "testing"

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
