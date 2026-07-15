//go:build windows

package host

import (
	"net/url"
	"strings"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func (host *Host) recordFrontendDiagnostic(kind string, detail string) {
	kind = logsafe.Message(kind)
	switch kind {
	case "phase":
		phase := logsafe.Message(detail)
		host.diagnostics.recordFrontendPhase(phase)
		host.log.Debug("mullion: frontend diagnostic phase, phase=" + phase)
	case "dom":
		host.log.Debug("mullion: frontend dom snapshot, detail=" + logsafe.Message(detail))
	case "resize-edge":
		host.log.Debug("mullion: frontend resize edge, edge=" + logsafe.Message(detail))
	case "resize-cursor":
		host.log.Debug("mullion: frontend resize cursor, state=" + logsafe.Message(detail))
	case "error":
		host.diagnostics.recordFrontendPhase("mullion: frontend window error")
		host.log.Error("mullion: frontend diagnostic error, message=" + logsafe.Message(detail))
	case "unhandledrejection":
		host.diagnostics.recordFrontendPhase("mullion: frontend unhandled rejection")
		host.log.Error("mullion: frontend diagnostic unhandled rejection, message=" + logsafe.Message(detail))
	default:
		if strings.HasPrefix(kind, "resource-") {
			host.diagnostics.recordFrontendPhase("mullion: frontend resource load failed")
			host.log.Warn("mullion: frontend resource load failed, kind=" + kind + ", asset=" + frontendDiagnosticAsset(detail))
		}
	}
}

func frontendDiagnosticAsset(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		return logsafe.FileName(parsed.Path)
	}
	return logsafe.FileName(raw)
}
