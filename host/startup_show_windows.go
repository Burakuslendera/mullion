//go:build windows

package host

import (
	"time"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// startStartupShowGate holds the window back until the frontend says it has
// something to show, so the user never sees an empty white frame.
//
// The timer is the safety net, not the mechanism: a frontend that never calls
// window.<ns>.shellReady() - because it crashed, or because it simply does not
// call it - must still get a visible window. Config.ShowTimeout bounds the wait;
// a negative value shows the window immediately.
func (host *Host) startStartupShowGate() {
	if host.config.StartHidden {
		return
	}
	if host.config.ShowTimeout < 0 {
		host.requestStartupShow("show_gate_disabled")
		return
	}
	host.startupMu.Lock()
	if host.startupShowReleased || host.startupShowTimer != nil {
		host.startupMu.Unlock()
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(host.config.ShowTimeout, func() {
		host.fireStartupShowGate(timer)
	})
	host.startupShowTimer = timer
	host.startupMu.Unlock()
	host.log.Debug("mullion: initial show gated")
}

func (host *Host) requestStartupShow(reason string) {
	if host.config.StartHidden {
		return
	}
	host.releaseStartupShowGate(nil, reason, false)
}

// fireStartupShowGate receives the timer identity so a callback from an older
// Run cannot release a newer run's gate. It detaches the fired timer before
// posting, so Host never retains a timer that can no longer be stopped.
func (host *Host) fireStartupShowGate(timer *time.Timer) {
	host.releaseStartupShowGate(timer, "frontend_shell_timeout", true)
}

func (host *Host) releaseStartupShowGate(expected *time.Timer, reason string, timedOut bool) {
	host.startupMu.Lock()
	if expected != nil && host.startupShowTimer != expected {
		host.startupMu.Unlock()
		return
	}
	if host.startupShowReleased {
		host.startupMu.Unlock()
		return
	}
	host.startupShowReleased = true
	timer := host.startupShowTimer
	host.startupShowTimer = nil
	// Bind the release to the HWND that owns this gate. A timer callback from an
	// ending Run must never look the handle up later and post to a newer Run's
	// recycled window.
	hwnd := host.window()
	host.startupMu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	postErr := postWindowMessage(hwnd, wmNativeShow)
	if timedOut {
		host.log.Warn("mullion: initial show fallback, reason=frontend_shell_timeout")
	}
	host.log.Debug("mullion: initial show gate released, reason=" + logsafe.Message(reason))
	host.warnIf("initial show post", postErr)
}

func (host *Host) stopStartupShowGate() {
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupShowTimer = nil
	host.startupShowReleased = true
	host.startupMu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}
