//go:build windows

package host

import (
	"time"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// startStartupShowGate begins the show gate for the current Run. A shell-ready
// signal may arrive from the embed pump before this point; that signal is
// latched, but no HWND post is attempted until the gate has started.
func (host *Host) startStartupShowGate() {
	admission := host.enterRun()
	defer host.leaveRun()
	host.startStartupShowGateForRun(admission)
}

func (host *Host) startStartupShowGateForRun(admission runAdmission) {
	if host.config.StartHidden || !host.runMatches(admission) {
		return
	}

	host.startupMu.Lock()
	if host.startupShowStarted || host.startupShowReleased {
		host.startupMu.Unlock()
		return
	}
	host.startupShowStarted = true
	reason := ""
	if host.config.ShowTimeout < 0 {
		host.startupShowRequested = true
		reason = "show_gate_disabled"
	} else {
		host.armStartupShowTimerLocked(admission)
		if host.startupShowRequested {
			reason = "frontend_shell_ready"
		}
	}
	host.startupMu.Unlock()

	if reason == "" {
		host.log.Debug("mullion: initial show gated")
		return
	}
	host.releaseStartupShowGateForRun(admission, nil, reason, false)
}
func (host *Host) requestStartupShow(reason string) {
	host.requestStartupShowForRun(host.currentRun(), reason)
}

func (host *Host) requestStartupShowForRun(admission runAdmission, reason string) {
	if host.config.StartHidden {
		return
	}
	host.startupMu.Lock()
	if host.startupShowReleased {
		host.startupMu.Unlock()
		return
	}
	host.startupShowRequested = true
	started := host.startupShowStarted
	host.startupMu.Unlock()
	if started {
		host.releaseStartupShowGateForRun(admission, nil, reason, false)
	}
}

func (host *Host) armStartupShowTimerLocked(admission runAdmission) {
	if host.startupShowTimer != nil || host.config.ShowTimeout < 0 {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(host.config.ShowTimeout, func() {
		host.fireStartupShowGate(timer, admission)
	})
	host.startupShowTimer = timer
}

// The timer re-enters only its captured Run. Winning teardown keeps it silent;
// winning counted admission keeps its post and fallback log in that Run without
// holding a non-reentrant mutex across Logger calls.
func (host *Host) fireStartupShowGate(timer *time.Timer, admission runAdmission) {
	if !host.enterOriginatingRun(admission) {
		return
	}
	defer host.leaveRun()
	host.releaseStartupShowGateForRun(admission, timer, "frontend_shell_timeout", true)
}

func (host *Host) releaseStartupShowGateForRun(admission runAdmission, expected *time.Timer, reason string, timedOut bool) {
	host.startupMu.Lock()
	if !host.startupShowStarted ||
		(expected != nil && host.startupShowTimer != expected) ||
		host.startupShowReleased || host.startupShowApplying {
		host.startupMu.Unlock()
		return
	}
	host.startupShowApplying = true
	host.startupShowReleased = true
	host.startupShowRequested = false
	timer := host.startupShowTimer
	host.startupShowTimer = nil
	host.startupMu.Unlock()

	postErr := error(windows.ERROR_INVALID_WINDOW_HANDLE)
	if host.runMatches(admission) {
		postErr = host.postRunCommand(admission, wmNativeShow, 0)
	}

	host.startupMu.Lock()
	host.startupShowApplying = false
	if postErr != nil && host.runMatches(admission) {
		host.startupShowReleased = false
		host.startupShowRequested = true
		// A failed queue application must not consume the fallback. This also
		// re-arms a timer that had already fired. A destroyed/replaced Run keeps
		// stopStartupShowGate's poison instead.
		host.armStartupShowTimerLocked(admission)
	}
	host.startupMu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if postErr != nil {
		host.warnIf("initial show post", postErr)
		return
	}
	if timedOut {
		host.log.Warn("mullion: initial show fallback, reason=frontend_shell_timeout")
	}
	host.log.Debug("mullion: initial show gate released, reason=" + logsafe.Message(reason))
}

// A queued wmNativeShow is only an attempt. If embedding or visibility
// application fails, reopen the gate and restore its fallback instead of
// consuming the one mechanism that can make the session visible.
func (host *Host) retryStartupShowAfterFailedApplication(hwnd windowHandle, token uintptr) {
	admission := runAdmission{token: token, hwnd: hwnd, running: true}
	if !host.runMatches(admission) {
		return
	}
	host.startupMu.Lock()
	if !host.runMatches(admission) ||
		!host.startupShowStarted || !host.startupShowReleased {
		host.startupMu.Unlock()
		return
	}
	host.startupShowReleased = false
	host.startupShowRequested = true
	host.armStartupShowTimerLocked(admission)
	host.startupMu.Unlock()
	host.log.Debug("mullion: initial show gate retry armed, reason=show_application_failed")
}

func (host *Host) stopStartupShowGate() {
	host.startupMu.Lock()
	timer := host.startupShowTimer
	host.startupShowTimer = nil
	host.startupShowStarted = true
	host.startupShowRequested = false
	host.startupShowApplying = false
	host.startupShowReleased = true
	host.startupMu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}
