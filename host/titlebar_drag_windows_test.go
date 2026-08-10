//go:build windows

package host

import (
	"errors"
	"strings"
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

func TestTitlebarDragHitTestDiagnosticReportsBoundedGeometry(t *testing.T) {
	t.Setenv("MULLION_HITTEST_DIAG", "1")
	logger := &captureLogger{}
	const maxInt32 = int32(1<<31 - 1)
	host := New(Config{
		ResizeBorder:                maxInt32,
		HitTestTitlebarHeight:       maxInt32,
		HitTestCaptionControlsWidth: maxInt32,
		Logger:                      logger,
	})
	windowRect := rect{Left: 100, Top: 200, Right: 120, Bottom: 210}
	cursor := point{X: 110, Y: 205}
	hit := nativeHitTestForRect(host.config.hitTestMetrics(), windowRect, cursor, ^uint32(0), false)
	host.logTitlebarDragHitTestDiagnostic(cursor, windowRect, ^uint32(0), false, hit)

	logText := logger.String()
	for _, want := range []string{
		"geometry_valid=true",
		"side_border=10",
		"top_border=5",
		"titlebar_height=10",
		"controls_width=20",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("bounded diagnostic missing %q:\n%s", want, logText)
		}
	}
}

func TestTitlebarDragHitTestDiagnosticRejectsInvalidGeometry(t *testing.T) {
	t.Setenv("MULLION_HITTEST_DIAG", "true")
	logger := &captureLogger{}
	host := New(Config{Logger: logger})
	windowRect := rect{Left: 10, Top: 10, Right: 10, Bottom: 20}
	cursor := point{X: 10, Y: 10}
	hit := nativeHitTestForRect(host.config.hitTestMetrics(), windowRect, cursor, 96, false)
	host.logTitlebarDragHitTestDiagnostic(cursor, windowRect, 96, false, hit)
	logText := logger.String()
	if !strings.Contains(logText, "geometry_valid=false") || !strings.Contains(logText, "hit=HTCLIENT") {
		t.Fatalf("invalid diagnostic did not match resolver rejection:\n%s", logText)
	}
}
