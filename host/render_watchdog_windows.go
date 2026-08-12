//go:build windows

package host

import (
	"time"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

// startRenderWatchdog arms the timer that catches the worst failure mode of this
// architecture: WebView2 embeds, navigates, reports no error, and paints
// nothing. Without the watchdog the user sees a blank window and the process
// looks healthy. When it fires it dumps the collected diagnostics, which say
// whether the document arrived, whether its stylesheets and scripts arrived, and
// what the last bridge call was.
//
// Config.RenderTimeout < 0 disables it.
func (host *Host) startRenderWatchdog() {
	host.startRenderWatchdogForRun(host.currentRun())
}

func (host *Host) startRenderWatchdogForRun(admission runAdmission) {
	host.renderMu.Lock()
	defer host.renderMu.Unlock()

	host.frontendReady = false
	host.frontendShellReady = false
	if host.renderTimer != nil {
		host.renderTimer.Stop()
		host.renderTimer = nil
	}
	if host.config.RenderTimeout < 0 {
		return
	}
	// Identity is a lock-protected generation rather than the timer pointer.
	// time.AfterFunc may run a zero-duration callback before it returns the
	// *Timer to its caller; closing over a variable assigned from that return is
	// both a data race and a stale-timer admission hole.
	host.renderGeneration++
	generation := host.renderGeneration
	host.renderTimer = time.AfterFunc(host.config.RenderTimeout, func() {
		host.fireRenderWatchdog(generation, admission)
	})
}

func (host *Host) fireRenderWatchdog(generation uint64, admission runAdmission) {
	if !host.enterOriginatingRun(admission) {
		return
	}
	defer host.leaveRun()
	host.renderMu.Lock()
	if host.renderGeneration != generation {
		host.renderMu.Unlock()
		return
	}
	host.renderTimer = nil
	ready := host.frontendReady
	host.renderMu.Unlock()
	if ready {
		return
	}
	host.log.Error("mullion: frontend render timeout, " + host.diagnostics.timeoutSummary())
}

func (host *Host) stopRenderWatchdog() {
	host.renderMu.Lock()
	defer host.renderMu.Unlock()

	if host.renderTimer != nil {
		host.renderTimer.Stop()
		host.renderGeneration++
		host.renderTimer = nil
	}
}

// MarkFrontendReady records that the frontend has painted. The injected bridge
// calls this for you when the frontend calls window.<ns>.ready(); it is exported
// so an application that drives its own readiness signal can too.
//
// Safe from any goroutine: the bounds sync it triggers is posted to the UI
// thread rather than run here, because the caller may be a background goroutine
// (Run blocks the UI thread, so an application driving its own readiness signal
// is off-thread by construction) and the sync touches the STA-bound WebView2
// controller.
func (host *Host) MarkFrontendReady() {
	admission := host.enterRun()
	defer host.leaveRun()

	host.renderMu.Lock()
	if host.frontendReady {
		host.renderMu.Unlock()
		return
	}
	host.frontendReady = true
	if host.renderTimer != nil {
		host.renderTimer.Stop()
		host.renderGeneration++
		host.renderTimer = nil
	}
	host.renderMu.Unlock()

	host.recordStartupFrontendReady()
	host.log.Info("mullion: frontend ready")
	host.postBoundsSyncForRun(admission, "frontend ready bounds post", boundsSyncWParamFrontendReady)
}

// MarkFrontendShellReady records that the frontend has rendered enough to be
// shown, and releases the startup show gate. Corresponds to
// window.<ns>.shellReady(). Safe from any goroutine, exactly as
// MarkFrontendReady. Once the gate has started its bounds sync and show are
// posted; an earlier signal is latched without a pre-window post.
//
// Idempotent, exactly as MarkFrontendReady: shellReady() is a reserved bridge
// method reachable from any page the bridge trusts, so a frontend calling it
// in a loop must not spam the log and the message queue (issue #47). The flag
// shares frontendReady's lifecycle - startRenderWatchdog resets both - while
// the startup timing record and show gate keep their own per-Run guards.
func (host *Host) MarkFrontendShellReady() {
	admission := host.enterRun()
	defer host.leaveRun()

	host.renderMu.Lock()
	if host.frontendShellReady {
		host.renderMu.Unlock()
		return
	}
	host.frontendShellReady = true
	host.renderMu.Unlock()

	host.recordStartupFrontendShellReady()
	host.log.Info("mullion: frontend shell ready")
	// During the embed pump the gate has not started and the controller may not
	// yet be committed. Latch readiness without posting anything; the eventual
	// tagged show applies a bounds sync after the gate starts.
	host.startupMu.Lock()
	gateStarted := host.startupShowStarted
	host.startupMu.Unlock()
	if gateStarted {
		host.postBoundsSyncForRun(admission, "frontend shell ready bounds post", boundsSyncWParamFrontendShellReady)
	}
	host.requestStartupShowForRun(admission, "frontend_shell_ready")
}

// MarkFrontendPhase records a free-form progress marker from the frontend. It
// appears in the render-watchdog summary as the last phase reached.
func (host *Host) MarkFrontendPhase(phase string) {
	host.enterRun()
	defer host.leaveRun()
	phase = logsafe.Field(phase)
	host.diagnostics.recordFrontendPhase(phase)
	host.log.Debug("mullion: frontend phase, phase=" + phase)
}

// MarkFrontendDiagnostic records a frontend diagnostic event (a script error, a
// failed resource, a DOM snapshot).
func (host *Host) MarkFrontendDiagnostic(kind string, detail string) {
	host.enterRun()
	defer host.leaveRun()
	host.recordFrontendDiagnostic(kind, detail)
}
