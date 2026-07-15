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
	host.startupShowTimer = time.AfterFunc(host.config.ShowTimeout, func() {
		host.log.Warn("mullion: initial show fallback, reason=frontend_shell_timeout")
		host.requestStartupShow("frontend_shell_timeout")
	})
	host.startupMu.Unlock()
	host.log.Debug("mullion: initial show gated")
}

func (host *Host) requestStartupShow(reason string) {
	if host.config.StartHidden {
		return
	}
	host.startupShowOnce.Do(func() {
		host.stopStartupShowGate()
		host.log.Debug("mullion: initial show gate released, reason=" + logsafe.Message(reason))
		host.warnIf("initial show post", postWindowMessage(host.window(), wmNativeShow))
	})
}

func (host *Host) stopStartupShowGate() {
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupShowTimer != nil {
		host.startupShowTimer.Stop()
		host.startupShowTimer = nil
	}
}
