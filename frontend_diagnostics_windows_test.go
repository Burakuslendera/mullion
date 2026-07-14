//go:build windows

package mullion

import (
	"strings"
	"testing"
)

func TestFrontendResizeCursorDiagnosticLogsState(t *testing.T) {
	host, logger := newTestHost(t, Config{})

	host.MarkFrontendDiagnostic("resize-cursor", "enabled")

	if !strings.Contains(logger.String(), "mullion: frontend resize cursor, state=enabled") {
		t.Fatalf("log text missing resize cursor diagnostic:\n%s", logger.String())
	}
}
