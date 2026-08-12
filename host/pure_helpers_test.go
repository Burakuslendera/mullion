//go:build windows

package host

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// Locks for small pure helpers an audit found untested. Each is a genuine
// fails-before: reverting the helper's guarantee fails the corresponding case.

func TestRuntimeArchitectureGatePrecedesNativeStartupAndReleasesRunGuard(t *testing.T) {
	host := &Host{}
	unsupported := fmt.Errorf("discovery failed: %w", webview2.ErrUnsupportedArchitecture)
	var steps []string

	err := host.withRunGuard(func() error {
		return continueAfterRuntimeDiscovery(
			func() (string, string, error) {
				steps = append(steps, "discovery")
				return "", "", unsupported
			},
			func(string) { steps = append(steps, "version observed") },
			func() error {
				steps = append(steps, "COM", "class", "HWND")
				return nil
			},
		)
	})
	if !errors.Is(err, ErrUnsupportedArchitecture) || !errors.Is(err, webview2.ErrUnsupportedArchitecture) {
		t.Fatalf("unsupported attempt error = %v, want public sentinel and internal cause", err)
	}
	if got := strings.Join(steps, ","); got != "discovery,version observed" {
		t.Fatalf("unsupported attempt steps = %q; native startup must not run", got)
	}

	// A second sequential attempt proves the first error released beginRun's
	// guard. An ordinary missing runtime is not fatal here: the embed path owns
	// that error, so native startup must continue.
	steps = nil
	err = host.withRunGuard(func() error {
		return continueAfterRuntimeDiscovery(
			func() (string, string, error) {
				steps = append(steps, "discovery")
				return "", "", fmt.Errorf("runtime missing")
			},
			func(string) { steps = append(steps, "version observed") },
			func() error {
				steps = append(steps, "COM", "class", "HWND")
				return nil
			},
		)
	})
	if err != nil {
		t.Fatalf("second sequential attempt: %v", err)
	}
	if got := strings.Join(steps, ","); got != "discovery,version observed,COM,class,HWND" {
		t.Fatalf("missing-runtime attempt steps = %q, want native startup to continue", got)
	}
}

// isHotBoundsSyncSource gates both the deferred-webview early return in
// syncWebViewBounds and the log dedup, so which sources are "hot" (the
// high-frequency resize/move messages) is load-bearing: adding or dropping a
// member silently changes hot-path behaviour.
func TestIsHotBoundsSyncSource(t *testing.T) {
	for _, source := range []string{"wm_size", "wm_move"} {
		if !isHotBoundsSyncSource(source) {
			t.Errorf("isHotBoundsSyncSource(%q) = false, want true", source)
		}
	}
	for _, source := range []string{"wm_moving", "wm_dpi_changed", "wm_windowpos_changing", "wm_windowpos_changed", "show", "restore", "maximize", "deferred_restore", "frontend_ready", ""} {
		if isHotBoundsSyncSource(source) {
			t.Errorf("isHotBoundsSyncSource(%q) = true, want false", source)
		}
	}
}

// shouldLogBoundsSync dedups the high-frequency bounds log: a hot source with
// unchanged dimensions is suppressed, but the first log, a dimension change, a
// mismatch, or any non-hot source always logs. Collapsing the dedup would flood
// the log on every WM_SIZE of a resize drag, and nothing else would catch it.
func TestShouldLogBoundsSyncDedupesHotSources(t *testing.T) {
	host, _ := newTestHost(t, Config{StartHidden: true})

	if !host.shouldLogBoundsSync("wm_size", 800, 600, 800, 600, false) {
		t.Fatal("the first hot bounds log must not be suppressed")
	}
	if host.shouldLogBoundsSync("wm_size", 800, 600, 800, 600, false) {
		t.Fatal("a repeated hot bounds log with identical dims must be suppressed")
	}
	if !host.shouldLogBoundsSync("wm_size", 801, 600, 801, 600, false) {
		t.Fatal("a hot bounds log with changed dims must not be suppressed")
	}
	if !host.shouldLogBoundsSync("wm_size", 801, 600, 801, 600, true) {
		t.Fatal("a bounds mismatch must always log, even when the dims are unchanged")
	}
	if !host.shouldLogBoundsSync("restore", 801, 600, 801, 600, false) {
		t.Fatal("a non-hot source must always log")
	}
}

// cssColour renders alpha in CSS's 0..1 range so a translucent BackgroundColour
// stays translucent; emitting the raw 0..255 byte would read as fully opaque.
func TestCssColourRendersFractionalAlpha(t *testing.T) {
	if got := cssColour(Colour{R: 10, G: 20, B: 30, A: 128}); got != "rgba(10,20,30,0.5019607843137255)" {
		t.Fatalf("cssColour(half alpha) = %q", got)
	}
	if got := cssColour(Colour{R: 0, G: 0, B: 0, A: 255}); got != "rgba(0,0,0,1)" {
		t.Fatalf("cssColour(opaque) = %q, want rgba(0,0,0,1)", got)
	}
}

// contrastColour picks legible text for a background of unknown brightness. Only
// the dark-background branch is exercised elsewhere; the default white background
// must resolve to dark text or the error message renders unreadably light-on-white.
func TestContrastColourForLightBackground(t *testing.T) {
	if got := contrastColour(Colour{R: 255, G: 255, B: 255, A: 255}); got != "#1a1a1a" {
		t.Fatalf("contrastColour(white) = %q, want #1a1a1a", got)
	}
	if got := contrastColour(Colour{R: 30, G: 30, B: 30, A: 255}); got != "#f2f2f2" {
		t.Fatalf("contrastColour(dark) = %q, want #f2f2f2", got)
	}
}
