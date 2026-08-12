//go:build windows

package host

import (
	"strings"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func (host *Host) recordFrontendDiagnostic(kind string, detail string) {
	kind = logsafe.Field(kind)
	switch kind {
	case "phase":
		phase := logsafe.Field(detail)
		host.diagnostics.recordFrontendPhase(phase)
		host.log.Debug("mullion: frontend diagnostic phase, phase=" + phase)
	case "dom":
		host.log.Debug("mullion: frontend dom snapshot, detail=" + logsafe.Diagnostic(detail))
	case "resize-edge":
		host.log.Debug("mullion: frontend resize edge, edge=" + logsafe.Field(detail))
	case "resize-cursor":
		host.log.Debug("mullion: frontend resize cursor, state=" + logsafe.Field(detail))
	case "error":
		host.diagnostics.recordFrontendPhase("mullion: frontend window error")
		host.log.Error("mullion: frontend diagnostic error, message=" + logsafe.Diagnostic(detail))
	case "unhandledrejection":
		host.diagnostics.recordFrontendPhase("mullion: frontend unhandled rejection")
		host.log.Error("mullion: frontend diagnostic unhandled rejection, message=" + logsafe.Diagnostic(detail))
	default:
		if strings.HasPrefix(kind, "resource-") {
			host.diagnostics.recordFrontendPhase("mullion: frontend resource load failed")
			host.log.Warn("mullion: frontend resource load failed, kind=" + kind + ", asset=" + frontendDiagnosticAsset(detail))
		}
	}
}

func frontendDiagnosticAsset(raw string) string {
	if index := strings.IndexAny(raw, "?#"); index >= 0 {
		raw = raw[:index]
	}
	return logsafe.FieldFileName(raw)
}
