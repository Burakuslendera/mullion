//go:build windows

package host

import (
	"strconv"
	"time"
)

// startupTiming records the four moments that describe a cold start, so a slow
// launch can be attributed rather than guessed at: process launch, the window
// becoming visible, the frontend's shell being ready, and the frontend being
// fully painted.
//
// It is disabled for a hidden start, where "launch to visible" is meaningless:
// the window may not be shown for hours.
type startupTiming struct {
	enabled            bool
	startedAt          time.Time
	windowVisible      time.Time
	frontendShellReady time.Time
	frontendReady      time.Time
	logged             bool
	warnBase           int64
	errorBase          int64
}

func newStartupTiming(startHidden bool) *startupTiming {
	return &startupTiming{
		enabled:   !startHidden,
		startedAt: time.Now(),
	}
}

func (host *Host) recordStartupFrontendShellReady() {
	host.recordStartupTiming(func(timing *startupTiming, now time.Time) {
		if timing.frontendShellReady.IsZero() {
			timing.frontendShellReady = now
		}
	})
}

func (host *Host) recordStartupWindowVisible() {
	host.recordStartupTiming(func(timing *startupTiming, now time.Time) {
		if timing.windowVisible.IsZero() {
			timing.windowVisible = now
		}
	})
}

func (host *Host) recordStartupFrontendReady() {
	host.recordStartupTiming(func(timing *startupTiming, now time.Time) {
		if timing.frontendReady.IsZero() {
			timing.frontendReady = now
		}
	})
	host.logStartupTimingSummary()
}

func (host *Host) recordStartupTiming(update func(*startupTiming, time.Time)) {
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	if host.startupTiming == nil || !host.startupTiming.enabled {
		return
	}
	update(host.startupTiming, time.Now())
}

// startupTimingSummary is the state the summary line is built from, copied out
// from under startupMu so the Logger call can happen without it.
type startupTimingSummary struct {
	startedAt          time.Time
	windowVisible      time.Time
	frontendShellReady time.Time
	frontendReady      time.Time
	warnBase           int64
	errorBase          int64
}

// logStartupTimingSummary emits one line, once, when the frontend reports it is
// ready. The warning and error counts ride along because a start that took twice
// as long usually also logged something on the way.
func (host *Host) logStartupTimingSummary() {
	host.startupMu.Lock()
	summary, emit := host.snapshotStartupTimingSummaryLocked()
	host.startupMu.Unlock()
	if !emit {
		return
	}
	// The Logger runs without startupMu: a Logger callback can re-enter Host
	// methods that take startupMu, which would deadlock this goroutine on the
	// non-reentrant mutex (issue #140).
	host.emitStartupTimingSummary(summary)
}

func (host *Host) snapshotStartupTimingSummaryLocked() (startupTimingSummary, bool) {
	timing := host.startupTiming
	if timing == nil || !timing.enabled || timing.logged || timing.frontendReady.IsZero() {
		return startupTimingSummary{}, false
	}
	timing.logged = true
	return startupTimingSummary{
		startedAt:          timing.startedAt,
		windowVisible:      timing.windowVisible,
		frontendShellReady: timing.frontendShellReady,
		frontendReady:      timing.frontendReady,
		warnBase:           timing.warnBase,
		errorBase:          timing.errorBase,
	}, true
}

func (host *Host) emitStartupTimingSummary(summary startupTimingSummary) {
	host.log.Info("mullion: startup timing summary" +
		", LaunchToWindowVisibleMs=" + formatTimingMs(summary.startedAt, summary.windowVisible) +
		", LaunchToFrontendShellReadyMs=" + formatTimingMs(summary.startedAt, summary.frontendShellReady) +
		", LaunchToFrontendReadyMs=" + formatTimingMs(summary.startedAt, summary.frontendReady) +
		", WindowVisibleToFrontendReadyMs=" + formatTimingMs(summary.windowVisible, summary.frontendReady) +
		", SessionWarnCount=" + strconv.FormatInt(host.log.WarnCount()-summary.warnBase, 10) +
		", SessionErrorCount=" + strconv.FormatInt(host.log.ErrorCount()-summary.errorBase, 10))
}

func formatTimingMs(start time.Time, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "missing"
	}
	elapsed := end.Sub(start)
	if elapsed < 0 {
		return "missing"
	}
	return strconv.FormatInt(elapsed.Milliseconds(), 10)
}
