//go:build windows

package host

import (
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// ProcessFailed(BrowserProcessExited) is the one WebView2 event whose runtime
// contract ends the embedded control: Microsoft documents that the WebView has
// already moved to Closed and that only a full recreation can recover. The
// host's policy is fail-closed instead of recreating (issue #155, decision
// 0052): the terminal outcome is recorded once and the window's destruction -
// which owns every browser/controller/core/environment release through the
// audited WM_DESTROY teardown - is scheduled as a tagged private command, never
// performed inline inside the runtime's event callback. A closed WebView
// therefore cannot remain attached to a ready Host, and Run reports
// ErrBrowserProcessExited instead of a false normal close.

// requestBrowserProcessExitTerminal records the one fail-closed terminal
// outcome of ProcessFailed(BrowserProcessExited) and schedules the window
// teardown. The browserExitTerminal flag is the exactly-once guard for
// duplicate events and is per-Run state: beginRun resets it, so a stale or
// repeated callback can request at most one tagged command per session.
//
// A browser already in ShuttingDown is refused: its teardown already owns the
// outcome, and handlers abandoned by ShuttingDown cannot legitimately deliver
// a later event. With no live window the request still records the cause for
// Run's return, but posts nothing - the existing destroy path owns teardown.
//
// The command is posted, never executed here. The callback runs inside the
// runtime's event dispatch; DestroyWindow or a re-embed from there would
// re-enter WebView2 from its own event (the nested-message-loop hazard the
// vendor sample warns about).
func (host *Host) requestBrowserProcessExitTerminal(browser *webview2.Browser) {
	if host.browserExitTerminal || browser.IsShuttingDown() {
		return
	}
	host.browserExitTerminal = true
	host.log.Debug("mullion: browser process exit terminal requested, action=host_close")
	if hwnd := host.window(); hwnd != 0 {
		host.warnIf("browser process exit post", host.postRunCommand(host.currentRun(), wmNativeProcessExit, 0))
	}
}

// applyBrowserProcessExitTeardown is the tagged command's body: destroy the
// window so WM_DESTROY performs the one ownership teardown. It runs on the UI
// thread through dispatchNativeHostCommand, which has already proven the
// command's HWND and Run token; a second delivery after WM_DESTROY is rejected
// there, and WM_DESTROY's own teardown is idempotent behind it. The log names
// the latched cause - the command is shared by the browser-process-exit policy
// and the asset boundary escalation (issue #150), and a pasted log must show
// which one tore the window down.
func (host *Host) applyBrowserProcessExitTeardown(hwnd windowHandle) {
	if host.assetBoundaryTerminal {
		host.log.Debug("mullion: asset boundary terminal applying, action=host_close")
	} else {
		host.log.Debug("mullion: browser process exit terminal applying, action=host_close")
	}
	procDestroyWindow.Call(uintptr(hwnd))
}

// browserExitTerminalOutcome is the message loop's exit result. The loop
// cannot distinguish a user close from the terminal teardown by its exit code,
// so the recorded cause decides: a session that ended because the browser
// process died is returned as ErrBrowserProcessExited, one that ended because
// the asset boundary escalated (issue #150) as ErrAssetBoundaryClosed - never
// as success.
func (host *Host) browserExitTerminalOutcome() error {
	if host.browserExitTerminal {
		return ErrBrowserProcessExited
	}
	if host.assetBoundaryTerminal {
		return ErrAssetBoundaryClosed
	}
	return nil
}
