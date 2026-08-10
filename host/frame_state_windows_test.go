//go:build windows

package host

import (
	"strings"
	"testing"
)

func TestWindowProcPublishesAuthoritativeMoveSizeState(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	const hwnd = windowHandle(0x1240)
	beginHeadlessLifecycleRun(t, host, hwnd)

	maximised := true
	host.queryNativeMaximised = func(got windowHandle) bool {
		if got != hwnd {
			t.Fatalf("maximised query HWND = %#x, want %#x", got, hwnd)
		}
		return maximised
	}
	var posted []string
	host.postFrameState = func(payload string) error {
		posted = append(posted, payload)
		return nil
	}

	host.windowProc(hwnd, wmEnterSizeMove, 0, 0)
	if len(posted) != 1 {
		t.Fatalf("WM_ENTERSIZEMOVE posted %d frame states, want 1", len(posted))
	}
	if want := `{"event":"WindowFrameStateChanged","state":{"maximised":true,"moveSizeActive":true,"generation":1}}`; posted[0] != want {
		t.Fatalf("enter frame state = %q, want %q", posted[0], want)
	}

	maximised = false
	reply := host.handleWebMessage(`{"id":"drag","method":"WindowFrameState","args":[]}`, true)
	if want := `{"id":"drag","ok":true,"result":{"maximised":false,"moveSizeActive":true,"generation":1}}`; reply != want {
		t.Fatalf("restored in-loop frame state = %q, want %q", reply, want)
	}

	host.windowProc(hwnd, wmEnterSizeMove, 0, 0)
	if len(posted) != 1 {
		t.Fatalf("duplicate WM_ENTERSIZEMOVE posted %d frame states, want 1", len(posted))
	}
	host.windowProc(hwnd, wmExitSizeMove, 0, 0)
	if len(posted) != 2 {
		t.Fatalf("WM_EXITSIZEMOVE posted %d frame states, want 2", len(posted))
	}
	if want := `{"event":"WindowFrameStateChanged","state":{"maximised":false,"moveSizeActive":false,"generation":2}}`; posted[1] != want {
		t.Fatalf("exit frame state = %q, want %q", posted[1], want)
	}
	host.windowProc(hwnd, wmExitSizeMove, 0, 0)
	if len(posted) != 2 {
		t.Fatalf("duplicate WM_EXITSIZEMOVE posted %d frame states, want 2", len(posted))
	}

	logText := logger.String()
	for _, want := range []string{
		"move-size state changed, active=true, generation=1",
		"move-size state changed, active=false, generation=2",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("move-size transition log missing %q:\n%s", want, logText)
		}
	}
}
