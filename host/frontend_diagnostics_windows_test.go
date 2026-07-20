//go:build windows

package host

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

// The failure branches are the signals the render-watchdog summary reports to
// tell "scripts threw" / "a resource 404'd" apart from "never rendered", so they
// must escalate: error and unhandledrejection at ERROR, a resource load failure
// at WARN with the sanitized asset name. Only resize-cursor was covered before.
func TestFrontendDiagnosticFailureBranches(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		detail     string
		wantLevel  string
		wantSubstr string
	}{
		{"window error", "error", "boom", "level=ERROR", "frontend diagnostic error, message=boom"},
		{"unhandled rejection", "unhandledrejection", "nope", "level=ERROR", "frontend diagnostic unhandled rejection, message=nope"},
		{"resource load failure", "resource-css", "https://mullion.local/style.css", "level=WARN", "frontend resource load failed, kind=resource-css, asset=style.css"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host, logger := newTestHost(t, Config{})
			host.MarkFrontendDiagnostic(test.kind, test.detail)
			log := logger.String()
			if !strings.Contains(log, test.wantLevel) || !strings.Contains(log, test.wantSubstr) {
				t.Fatalf("MarkFrontendDiagnostic(%q) log missing %q / %q:\n%s", test.kind, test.wantLevel, test.wantSubstr, log)
			}
		})
	}
}
