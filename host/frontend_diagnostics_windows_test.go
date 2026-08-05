//go:build windows

package host

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/Burakuslendera/mullion/internal/logsafe"
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
		{"resource load failure", "resource-css", "https://mullion.localhost/style.css", "level=WARN", "frontend resource load failed, kind=resource-css, asset=style.css"},
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

func TestFrontendDiagnosticsBoundValuesAndKeepFirstURL(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	detail := strings.Repeat("prefix.:;, ", logsafe.DiagnosticLimit*2) +
		"https://mullion.localhost/app/main.js?token=secret"

	host.MarkFrontendDiagnostic("error", detail)

	logText := logger.String()
	if !strings.Contains(logText, "https://mullion.localhost/app/main.js?") {
		t.Fatalf("frontend error lost its first URL:\n%s", logText)
	}
	if strings.Contains(logText, "token=secret") {
		t.Fatalf("frontend error retained a query value:\n%s", logText)
	}
	if strings.Contains(logText, detail) {
		t.Fatal("logger received the unbounded frontend detail")
	}
}

func TestFrontendPhaseKindAndRetainedStateAreBounded(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	phase := "ready" + strings.Repeat(" ", logsafe.DiagnosticLimit*4)
	host.MarkFrontendPhase(phase)

	host.diagnostics.mu.Lock()
	retainedPhase := host.diagnostics.lastFrontendPhase
	host.diagnostics.mu.Unlock()
	if len(retainedPhase) > logsafe.DiagnosticLimit {
		t.Fatalf("retained phase len = %d, limit = %d", len(retainedPhase), logsafe.DiagnosticLimit)
	}
	phaseStart := uintptr(unsafe.Pointer(unsafe.StringData(phase)))
	retainedStart := uintptr(unsafe.Pointer(unsafe.StringData(retainedPhase)))
	if retainedStart >= phaseStart && retainedStart < phaseStart+uintptr(len(phase)) {
		t.Fatal("retained frontend phase shares the large input's backing storage")
	}

	kind := "resource-" + strings.Repeat(".:;,", logsafe.DiagnosticLimit*2)
	host.MarkFrontendDiagnostic(kind, "https://mullion.localhost/app.js")
	if strings.Contains(logger.String(), kind) {
		t.Fatal("logger received an unbounded frontend phase or diagnostic kind")
	}
}

func TestFrontendResourceDiagnosticKeepsLongURLFinalName(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	detail := "https://mullion.localhost/" + strings.Repeat("dir/", logsafe.DiagnosticLimit) +
		"missing.js?cache=secret"

	host.MarkFrontendDiagnostic("resource-script", detail)

	logText := logger.String()
	if !strings.Contains(logText, "asset=missing.js") {
		t.Fatalf("long resource diagnostic lost its final name:\n%s", logText)
	}
	if strings.Contains(logText, "cache=secret") || strings.Contains(logText, detail) {
		t.Fatalf("long resource diagnostic retained unbounded or query text:\n%s", logText)
	}
}
