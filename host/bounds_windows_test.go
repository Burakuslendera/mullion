//go:build windows

package host

import (
	"strings"
	"testing"
)

func TestWebViewBoundsMismatch(t *testing.T) {
	tests := []struct {
		name             string
		clientWidth      int32
		clientHeight     int32
		controllerWidth  int32
		controllerHeight int32
		want             bool
	}{
		{name: "tiny client ignored", clientWidth: 250, clientHeight: 180, controllerWidth: 1, controllerHeight: 1, want: false},
		{name: "tiny controller", clientWidth: 900, clientHeight: 620, controllerWidth: 60, controllerHeight: 40, want: true},
		{name: "under seventy five percent", clientWidth: 900, clientHeight: 620, controllerWidth: 600, controllerHeight: 620, want: true},
		{name: "matching", clientWidth: 900, clientHeight: 620, controllerWidth: 900, controllerHeight: 620, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := webViewBoundsMismatch(test.clientWidth, test.clientHeight, test.controllerWidth, test.controllerHeight)
			if got != test.want {
				t.Fatalf("webViewBoundsMismatch() = %v, want %v", got, test.want)
			}
		})
	}
}

// The frame is client-extended, so the WebView must cover the full client area in
// every state. A non-zero inset would leave a dead strip that the frontend cannot
// paint into and the user cannot click through.
func TestWebViewBoundsTargetFullClient(t *testing.T) {
	target := webViewBoundsTarget(0, 900, 620)
	if target.Left != 0 || target.Top != 0 || target.Right != 900 || target.Bottom != 620 {
		t.Fatalf("webview bounds target = %+v, want full client", target)
	}
}

func TestFormatWebViewBoundsLogs(t *testing.T) {
	syncLog := formatWebViewBoundsSyncLog("frontend_ready", 900, 620, 144, 60, 40)
	for _, expected := range []string{
		"mullion: webview bounds sync",
		"source=frontend_ready",
		"client_width=900",
		"client_height=620",
		"dpi=144",
		"controller_width=60",
		"controller_height=40",
	} {
		if !strings.Contains(syncLog, expected) {
			t.Fatalf("sync log missing %q:\n%s", expected, syncLog)
		}
	}

	mismatchLog := formatWebViewBoundsMismatchLog("frontend_ready", 900, 620, 60, 40)
	if !strings.Contains(mismatchLog, "mullion: frontend ready but surface tiny/bounds mismatch") {
		t.Fatalf("mismatch log missing frontend-ready warning:\n%s", mismatchLog)
	}
}

// The wParam-to-source mapping is what keeps the diagnostics honest once the
// frontend-ready syncs travel as messages: "frontend_ready" drives the special
// "frontend ready but surface tiny" warning, so losing the label to a generic
// deferred one would erase the one line that separates an asset failure from a
// late resize.
func TestBoundsSyncSourceFromWParam(t *testing.T) {
	tests := []struct {
		wParam uintptr
		want   string
	}{
		{boundsSyncWParamDeferred, "deferred_window_state"},
		{boundsSyncWParamFrontendReady, "frontend_ready"},
		{boundsSyncWParamFrontendShellReady, "frontend_shell_ready"},
		// The deferred window actions (issue #46): each keeps its own label so a
		// bounds regression after a restore is distinguishable from one after an
		// exit-size-move, and from the immediate sync of the same action.
		{boundsSyncWParamDeferredRestore, "deferred_restore"},
		{boundsSyncWParamDeferredMaximize, "deferred_maximize"},
		{boundsSyncWParamDeferredExitSizeMove, "deferred_wm_exitsizemove"},
		{99, "deferred_window_state"},
	}
	for _, test := range tests {
		if got := boundsSyncSourceFromWParam(test.wParam); got != test.want {
			t.Errorf("boundsSyncSourceFromWParam(%d) = %q, want %q", test.wParam, got, test.want)
		}
	}
}

// The deferred sync used to post wParam 0 -> "deferred_window_state", which
// shouldNotifyBoundsSource treats as a parent-position notify. The new
// deferred_ labels must stay in that set, or renaming them would silently stop
// the deferred sync from notifying WebView2 that the parent moved (issue #46).
func TestDeferredBoundsSourcesStillNotifyParent(t *testing.T) {
	for _, wParam := range []uintptr{
		boundsSyncWParamDeferredRestore,
		boundsSyncWParamDeferredMaximize,
		boundsSyncWParamDeferredExitSizeMove,
	} {
		source := boundsSyncSourceFromWParam(wParam)
		if !shouldNotifyBoundsSource(source) {
			t.Errorf("shouldNotifyBoundsSource(%q) = false: the deferred sync must still notify the parent moved", source)
		}
	}
}

func TestDeferredWebViewBoundsSyncDoesNotWarn(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})

	host.syncWebViewBounds("wm_dpi_changed")

	logText := logger.String()
	if !strings.Contains(logText, "mullion: webview bounds sync deferred") {
		t.Fatalf("log text missing deferred bounds sync:\n%s", logText)
	}
	if strings.Contains(logText, "level=WARN") {
		t.Fatalf("deferred bounds sync produced a warning:\n%s", logText)
	}
}

// WM_WINDOWPOSCHANGING and WM_MOVING provide proposed geometry. DefWindowProc
// turns the applied WM_WINDOWPOSCHANGED geometry into WM_SIZE and WM_MOVE, so
// only those post-apply messages may own a controller sync. The seam proves
// routing without an HWND or WebView2 controller.
func TestWindowProcBoundsSyncsOnlyPostApplyMessages(t *testing.T) {
	var sources []string
	var delegated []uint32
	host := &Host{
		syncWindowBounds: func(source string) {
			sources = append(sources, source)
		},
		defaultWindowProc: func(_ windowHandle, message uint32, _, _ uintptr) uintptr {
			delegated = append(delegated, message)
			return 0
		},
	}
	for _, message := range []uint32{
		wmWindowPosChanging,
		wmWindowPosChanged,
		wmSize,
		wmMove,
		wmMoving,
	} {
		host.windowProc(0, message, 0, 0)
	}
	got := strings.Join(sources, ",")
	if want := "wm_size,wm_move"; got != want {
		t.Fatalf("bounds sync sources = %q, want %q", got, want)
	}
	wantDelegated := []uint32{wmWindowPosChanging, wmWindowPosChanged, wmSize, wmMove, wmMoving}
	if len(delegated) != len(wantDelegated) {
		t.Fatalf("default-proc calls = %#v, want %#v", delegated, wantDelegated)
	}
	for index, want := range wantDelegated {
		if got := delegated[index]; got != want {
			t.Fatalf("default-proc call %d = %#x, want %#x", index, got, want)
		}
	}
}
