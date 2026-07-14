//go:build windows

package mullion

import (
	"strings"
	"testing"
	"time"
)

func TestStartupTimingSummaryLogsOnceWithSanitizedMetrics(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	base := time.Unix(100, 0)
	host.startupTiming = &startupTiming{
		enabled:            true,
		startedAt:          base,
		windowVisible:      base.Add(450 * time.Millisecond),
		frontendShellReady: base.Add(400 * time.Millisecond),
		frontendReady:      base.Add(1200 * time.Millisecond),
	}

	host.logStartupTimingSummary()
	host.logStartupTimingSummary()

	logText := logger.String()
	if got := strings.Count(logText, "mullion: startup timing summary"); got != 1 {
		t.Fatalf("startup timing summary count = %d, want 1:\n%s", got, logText)
	}
	for _, expected := range []string{
		"LaunchToWindowVisibleMs=450",
		"LaunchToFrontendShellReadyMs=400",
		"LaunchToFrontendReadyMs=1200",
		"WindowVisibleToFrontendReadyMs=750",
		"SessionWarnCount=0",
		"SessionErrorCount=0",
	} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("log text missing %q:\n%s", expected, logText)
		}
	}
	// The summary is a timing line, not a handle dump: it must never carry a
	// window handle or a file system path into someone's log aggregator.
	for _, forbidden := range []string{"hwnd=", `C:\`, "WindowHandle"} {
		if strings.Contains(logText, forbidden) {
			t.Fatalf("log text leaked %q:\n%s", forbidden, logText)
		}
	}
}

func TestStartupTimingSummarySkipsHiddenStartup(t *testing.T) {
	host, logger := newTestHost(t, Config{StartHidden: true})
	host.recordStartupWindowVisible()
	host.recordStartupFrontendShellReady()
	host.recordStartupFrontendReady()

	if strings.Contains(logger.String(), "mullion: startup timing summary") {
		t.Fatalf("hidden startup logged a timing summary:\n%s", logger.String())
	}
}

// TestStartupTimingCountsWarnings proves the counters are per-Host: two hosts in
// one process must not see each other's warnings.
func TestStartupTimingCountsWarnings(t *testing.T) {
	first, _ := newTestHost(t, Config{})
	second, secondLogger := newTestHost(t, Config{})

	first.log.Warn("mullion: first host warning")
	second.log.Warn("mullion: second host warning")
	second.log.Error("mullion: second host error")

	base := time.Unix(100, 0)
	second.startupTiming = &startupTiming{
		enabled:       true,
		startedAt:     base,
		windowVisible: base.Add(10 * time.Millisecond),
		frontendReady: base.Add(20 * time.Millisecond),
	}
	second.logStartupTimingSummary()

	logText := secondLogger.String()
	if !strings.Contains(logText, "SessionWarnCount=1") || !strings.Contains(logText, "SessionErrorCount=1") {
		t.Fatalf("counters are not per-host:\n%s", logText)
	}
}
