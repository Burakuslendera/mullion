//go:build windows

package mullion

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
	host.renderMu.Lock()
	defer host.renderMu.Unlock()

	host.frontendReady = false
	if host.renderTimer != nil {
		host.renderTimer.Stop()
	}
	if host.config.RenderTimeout < 0 {
		return
	}
	host.renderTimer = time.AfterFunc(host.config.RenderTimeout, func() {
		host.renderMu.Lock()
		defer host.renderMu.Unlock()
		if host.frontendReady {
			return
		}
		host.log.Error("mullion: frontend render timeout, " + host.diagnostics.timeoutSummary())
	})
}

func (host *Host) stopRenderWatchdog() {
	host.renderMu.Lock()
	defer host.renderMu.Unlock()

	if host.renderTimer != nil {
		host.renderTimer.Stop()
	}
}

// MarkFrontendReady records that the frontend has painted. The injected bridge
// calls this for you when the frontend calls window.<ns>.ready(); it is exported
// so an application that drives its own readiness signal can too.
func (host *Host) MarkFrontendReady() {
	host.renderMu.Lock()
	if host.frontendReady {
		host.renderMu.Unlock()
		return
	}
	host.frontendReady = true
	if host.renderTimer != nil {
		host.renderTimer.Stop()
	}
	host.renderMu.Unlock()

	host.recordStartupFrontendReady()
	host.log.Info("mullion: frontend ready")
	host.syncWebViewBounds("frontend_ready")
}

// MarkFrontendShellReady records that the frontend has rendered enough to be
// shown, and releases the startup show gate. Corresponds to
// window.<ns>.shellReady().
func (host *Host) MarkFrontendShellReady() {
	host.recordStartupFrontendShellReady()
	host.log.Info("mullion: frontend shell ready")
	host.syncWebViewBounds("frontend_shell_ready")
	host.requestStartupShow("frontend_shell_ready")
}

// MarkFrontendPhase records a free-form progress marker from the frontend. It
// appears in the render-watchdog summary as the last phase reached.
func (host *Host) MarkFrontendPhase(phase string) {
	host.diagnostics.recordFrontendPhase(phase)
	host.log.Debug("mullion: frontend phase, phase=" + logsafe.Message(phase))
}

// MarkFrontendDiagnostic records a frontend diagnostic event (a script error, a
// failed resource, a DOM snapshot).
func (host *Host) MarkFrontendDiagnostic(kind string, detail string) {
	host.recordFrontendDiagnostic(kind, detail)
}
