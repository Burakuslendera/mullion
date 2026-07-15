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

// logStartupTimingSummary emits one line, once, when the frontend reports it is
// ready. The warning and error counts ride along because a start that took twice
// as long usually also logged something on the way.
func (host *Host) logStartupTimingSummary() {
	host.startupMu.Lock()
	defer host.startupMu.Unlock()
	timing := host.startupTiming
	if timing == nil || !timing.enabled || timing.logged || timing.frontendReady.IsZero() {
		return
	}
	timing.logged = true
	host.log.Info("mullion: startup timing summary" +
		", LaunchToWindowVisibleMs=" + formatTimingMs(timing.startedAt, timing.windowVisible) +
		", LaunchToFrontendShellReadyMs=" + formatTimingMs(timing.startedAt, timing.frontendShellReady) +
		", LaunchToFrontendReadyMs=" + formatTimingMs(timing.startedAt, timing.frontendReady) +
		", WindowVisibleToFrontendReadyMs=" + formatTimingMs(timing.windowVisible, timing.frontendReady) +
		", SessionWarnCount=" + strconv.FormatInt(host.log.WarnCount(), 10) +
		", SessionErrorCount=" + strconv.FormatInt(host.log.ErrorCount(), 10))
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
