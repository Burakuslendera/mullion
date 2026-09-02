//go:build windows

package host

import (
	"strings"
	"testing"
	"time"
)

// showReentryLogger runs a hook from inside Info, which is what an embedder's
// Logger is: arbitrary code that may call back into Host.
type showReentryLogger struct {
	*captureLogger
	onInfo func()
}

func (logger *showReentryLogger) Info(message string) {
	logger.captureLogger.Info(message)
	if hook := logger.onInfo; hook != nil {
		logger.onInfo = nil // once: the hook itself logs
		hook()
	}
}

// TestStartupTimingSummarySurvivesLoggerReentrantShow drives the issue #140
// chain headlessly: recordStartupFrontendReady emits the summary line, its
// Logger re-enters Show, and the queued show's apply path - applyShowAfterEnsure,
// replayed here by the sendNativeCommand seam - records the window-visible
// moment under startupMu. Holding startupMu across the Logger call deadlocks
// this goroutine on the non-reentrant mutex.
func TestStartupTimingSummarySurvivesLoggerReentrantShow(t *testing.T) {
	logger := &showReentryLogger{captureLogger: &captureLogger{}}
	host := New(Config{Logger: logger})
	stubExternalOpen(host)
	beginHeadlessLifecycleRun(t, host, windowHandle(0x1414))
	// applyShowAfterEnsure records the visible moment when a queued show
	// succeeds; the seam performs that same re-entry synchronously.
	host.sendNativeCommand = func(windowHandle, uint32, uintptr, uintptr) (uintptr, error) {
		host.recordStartupWindowVisible()
		return 1, nil
	}
	logger.onInfo = func() { _ = host.Show() }

	recordFrontendReady := func() {
		done := make(chan struct{})
		go func() {
			host.recordStartupFrontendReady()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("recordStartupFrontendReady deadlocked behind a Logger re-entry while startupMu was held")
		}
	}
	host.recordStartupWindowVisible()
	host.recordStartupFrontendShellReady()
	recordFrontendReady()
	// The once-flag is latched under startupMu even though the line is emitted
	// outside it, so a repeated readiness record must stay silent.
	recordFrontendReady()

	logText := logger.String()
	if got := strings.Count(logText, "mullion: startup timing summary"); got != 1 {
		t.Fatalf("startup timing summary count = %d, want 1:\n%s", got, logText)
	}
	if strings.Contains(logText, "missing") {
		t.Fatalf("summary lost a timing field across the emit split:\n%s", logText)
	}
}

// The non-reentrant control: with a Logger that never calls back, the summary
// is still emitted exactly once with every timing field intact.
func TestStartupTimingSummaryEmitsOnceForPlainLogger(t *testing.T) {
	host, logger := newTestHost(t, Config{})
	host.recordStartupWindowVisible()
	host.recordStartupFrontendShellReady()
	host.recordStartupFrontendReady()
	host.recordStartupFrontendReady()

	logText := logger.String()
	if got := strings.Count(logText, "mullion: startup timing summary"); got != 1 {
		t.Fatalf("startup timing summary count = %d, want 1:\n%s", got, logText)
	}
	for _, expected := range []string{"SessionWarnCount=0", "SessionErrorCount=0"} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("log text missing %q:\n%s", expected, logText)
		}
	}
	if strings.Contains(logText, "missing") {
		t.Fatalf("summary lost a timing field across the emit split:\n%s", logText)
	}
}
