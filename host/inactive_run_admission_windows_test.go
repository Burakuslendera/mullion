//go:build windows

package host

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The admission probe schedule: a public window-control call made while no Run
// is active logs synchronously, and that Logger callback re-enters public Run.
// The host carries a source-plan preflight error, so a healthy Run returns
// deterministically before runtime discovery, COM, class registration, or HWND
// creation. If the outer call still owns an admission that Run waits on, the
// re-entry never returns and the bounded selects below fail instead of hanging.
const inactiveAdmissionBound = 2 * time.Second

// reentrantRunLogger answers its first Debug by re-entering public Run once and
// recording the result.
type reentrantRunLogger struct {
	host    *Host
	once    atomic.Bool
	entered chan struct{}
	runErr  chan error
}

func newReentrantRunLogger() *reentrantRunLogger {
	return &reentrantRunLogger{
		entered: make(chan struct{}),
		runErr:  make(chan error, 1),
	}
}

func (logger *reentrantRunLogger) Debug(string) {
	if logger.once.CompareAndSwap(false, true) {
		close(logger.entered)
		logger.runErr <- logger.host.Run()
	}
}
func (*reentrantRunLogger) Info(string)  {}
func (*reentrantRunLogger) Warn(string)  {}
func (*reentrantRunLogger) Error(string) {}

func newInactiveAdmissionHost(t *testing.T, logger Logger) *Host {
	t.Helper()
	return New(Config{URL: "https://mullion.invalid/frontend", Logger: logger})
}

func probeInactiveHideWithReentrantRun(t *testing.T, host *Host, logger *reentrantRunLogger) error {
	t.Helper()
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error { return nil }
	hideDone := make(chan struct{})
	go func() {
		host.Hide()
		close(hideDone)
	}()
	select {
	case <-logger.entered:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("Hide never reached its Logger callback")
	}
	var runErr error
	select {
	case runErr = <-logger.runErr:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("a Logger callback re-entering Run from an inactive Hide did not return; the outer Hide owns the admission Run waits on")
	}
	select {
	case <-hideDone:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("Hide did not return after its Logger-reentrant Run completed")
	}
	return runErr
}

func TestInactiveHideSurvivesLoggerReentrantRunBeforeFirstRun(t *testing.T) {
	logger := newReentrantRunLogger()
	host := newInactiveAdmissionHost(t, logger)
	logger.host = host

	if runErr := probeInactiveHideWithReentrantRun(t, host, logger); runErr == nil || !strings.Contains(runErr.Error(), "loopback") {
		t.Fatalf("re-entrant Run error = %v, want the deterministic source-plan preflight error", runErr)
	}
}

func TestInactiveHideSurvivesLoggerReentrantRunAfterCompletedRun(t *testing.T) {
	logger := newReentrantRunLogger()
	host := newInactiveAdmissionHost(t, logger)
	logger.host = host

	beginHeadlessLifecycleRun(t, host, windowHandle(0xb1b1))
	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
	host.endRun()

	if runErr := probeInactiveHideWithReentrantRun(t, host, logger); runErr == nil || !strings.Contains(runErr.Error(), "loopback") {
		t.Fatalf("re-entrant Run error = %v, want the deterministic source-plan preflight error", runErr)
	}
}

// A Logger that returns normally is the matched control: the same inactive Hide
// completes, and the sequential Run it precedes still reaches its preflight
// error with the hide command effect intact.
func TestInactiveHideKeepsSequentialRunReachableWithPlainLogger(t *testing.T) {
	host := newInactiveAdmissionHost(t, &captureLogger{})
	var posts atomic.Int32
	host.postNativeCommand = func(windowHandle, uint32, uintptr, uintptr) error {
		posts.Add(1)
		return nil
	}

	host.Hide()
	if err := host.Run(); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("Run after an inactive Hide = %v, want the source-plan preflight error", err)
	}
	if posts.Load() != 1 {
		t.Fatalf("inactive Hide posted %d commands, want 1", posts.Load())
	}
}

// While a Run is active the same re-entry must return the documented
// concurrent-Run rejection immediately, and the outer Hide must keep its
// counted admission's command effect through teardown.
func TestActiveRunLoggerReentrantRunIsRejectedAsConcurrent(t *testing.T) {
	logger := newReentrantRunLogger()
	host := newInactiveAdmissionHost(t, logger)
	logger.host = host
	const hwnd = windowHandle(0xb2b2)
	run := beginHeadlessLifecycleRun(t, host, hwnd)

	var posts atomic.Int32
	host.postNativeCommand = func(gotHWND windowHandle, message uint32, _, token uintptr) error {
		if gotHWND != hwnd || token != run.token {
			t.Errorf("hide post escaped the active Run: hwnd=%#x token=%#x", gotHWND, token)
		}
		posts.Add(1)
		return nil
	}

	hideDone := make(chan struct{})
	go func() {
		host.Hide()
		close(hideDone)
	}()
	select {
	case <-logger.entered:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("Hide never reached its Logger callback")
	}
	var runErr error
	select {
	case runErr = <-logger.runErr:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("a Run re-entered from an active Run's Logger waited instead of being rejected as concurrent")
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "already running") {
		t.Fatalf("re-entrant Run error = %v, want host is already running", runErr)
	}
	select {
	case <-hideDone:
	case <-time.After(inactiveAdmissionBound):
		t.Fatal("Hide did not return after its Logger-reentrant Run was rejected")
	}
	if posts.Load() != 1 {
		t.Fatalf("active-Run Hide posted %d commands, want 1", posts.Load())
	}

	host.mu.Lock()
	host.hwnd = 0
	host.mu.Unlock()
	host.endRun()
}
