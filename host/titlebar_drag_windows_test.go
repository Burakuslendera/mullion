//go:build windows

package host

import (
	"errors"
	"testing"
)

func TestApplyTitlebarDragSendsCursorLParam(t *testing.T) {
	var got uintptr
	ok := New(Config{}).applyTitlebarDrag(titlebarDragDispatcher{
		releaseCapture: func() error { return nil },
		cursor:         point{X: 320, Y: -24},
		send: func(lParam uintptr) error {
			got = lParam
			return nil
		},
	})
	if !ok {
		t.Fatal("New(Config{}).applyTitlebarDrag() = false, want true")
	}
	if pointFromLParam(got) != (point{X: 320, Y: -24}) {
		t.Fatalf("drag lParam point = %#v, want cursor point", pointFromLParam(got))
	}
}

func TestApplyTitlebarDragReportsSendFailure(t *testing.T) {
	ok := New(Config{}).applyTitlebarDrag(titlebarDragDispatcher{
		releaseCapture: func() error { return nil },
		cursor:         point{X: 1, Y: 2},
		send:           func(uintptr) error { return errors.New("send failed") },
	})
	if ok {
		t.Fatal("New(Config{}).applyTitlebarDrag() = true, want false")
	}
}
