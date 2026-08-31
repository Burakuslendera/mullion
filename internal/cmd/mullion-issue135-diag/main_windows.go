//go:build windows && mullion_script_completion_delay_diag

// Command mullion-issue135-diag drives the supported-Runtime half of Issue
// #135's exact-tree slow-start protocol. It is absent from ordinary builds.
package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Burakuslendera/mullion/host"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

//go:embed index.html
var diagnosticAssets embed.FS

type captureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (logger *captureLogger) Debug(message string) { logger.record("DEBUG", message) }
func (logger *captureLogger) Info(message string)  { logger.record("INFO", message) }
func (logger *captureLogger) Warn(message string)  { logger.record("WARN", message) }
func (logger *captureLogger) Error(message string) { logger.record("ERROR", message) }

func (logger *captureLogger) record(level, message string) {
	line := "level=" + level + " " + message
	logger.mu.Lock()
	logger.lines = append(logger.lines, line)
	logger.mu.Unlock()
	fmt.Fprintln(os.Stderr, line)
}

func (logger *captureLogger) String() string {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return strings.Join(logger.lines, "\n")
}

func (logger *captureLogger) waitContains(fragment string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(logger.String(), fragment) {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return strings.Contains(logger.String(), fragment)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mullion: issue135 diagnostic failed, reason="+err.Error())
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "mullion: issue135 diagnostic passed")
}

func run() error {
	logger := &captureLogger{}
	diagnostic, err := webview2.StartIssue135ScriptCompletionDelayDiagnostic(func(marker string) {
		logger.record("DIAG", marker)
	})
	if err != nil {
		return err
	}
	defer diagnostic.Close()

	readyCalls := 0
	hostInstance := host.New(host.Config{
		Assets: diagnosticAssets,
		Logger: logger,
		OnReady: func() {
			readyCalls++
		},
	})

	sequenceDone := make(chan error, 1)
	go func() {
		fail := func(sequenceErr error) {
			diagnostic.Close()
			hostInstance.Quit()
			sequenceDone <- sequenceErr
		}
		first, err := diagnostic.WaitHeld(15 * time.Second)
		if err != nil {
			fail(err)
			return
		}
		if first.RequiredIndex != 1 {
			fail(fmt.Errorf("first held required index = %d, want 1", first.RequiredIndex))
			return
		}
		if !logger.waitContains("state=real_callback_held, required_index=1", time.Second) {
			fail(errors.New("first held callback marker was not delivered"))
			return
		}
		if err := hostInstance.Show(); err == nil {
			fail(errors.New("re-entrant Show unexpectedly succeeded"))
			return
		}
		if err := diagnostic.Release(first.RequiredIndex); err != nil {
			fail(err)
			return
		}

		second, err := diagnostic.WaitHeld(15 * time.Second)
		if err != nil {
			fail(err)
			return
		}
		if second.RequiredIndex != 2 {
			fail(fmt.Errorf("second held required index = %d, want 2", second.RequiredIndex))
			return
		}
		if !logger.waitContains("state=real_callback_held, required_index=2", time.Second) {
			fail(errors.New("second held callback marker was not delivered"))
			return
		}
		hostInstance.Quit()
		if err := diagnostic.Release(second.RequiredIndex); err != nil {
			diagnostic.Close()
			sequenceDone <- err
			return
		}
		sequenceDone <- nil
	}()

	runErr := hostInstance.Run()
	sequenceErr := <-sequenceDone
	if sequenceErr != nil {
		return sequenceErr
	}
	if runErr == nil {
		return errors.New("Run returned success after diagnostic cancellation")
	}
	logged := logger.String()
	if !strings.Contains(logged, "show failed, reason=webview embed already in flight") {
		return errors.New("missing re-entrant Show rejection")
	}
	if !strings.Contains(logged, "quit requested") {
		return errors.New("missing diagnostic Quit request")
	}
	for _, forbidden := range forbiddenPostCancellationSuccessMarkers() {
		if strings.Contains(logged, forbidden) {
			return fmt.Errorf("forbidden post-cancellation success marker %q", forbidden)
		}
	}
	if readyCalls != 0 {
		return fmt.Errorf("OnReady calls = %d, want 0", readyCalls)
	}
	if !strings.Contains(runErr.Error(), "document-created script") {
		return fmt.Errorf("Run error does not identify required-script barrier: %w", runErr)
	}
	for _, requiredMarker := range []string{
		"state=real_callback_held, required_index=1",
		"state=real_callback_published_explicit_release, required_index=1",
		"state=real_callback_held, required_index=2",
	} {
		if !logger.waitContains(requiredMarker, time.Second) {
			return fmt.Errorf("missing required diagnostic marker %q", requiredMarker)
		}
	}
	if !logger.waitContains("state=real_callback_published_explicit_release, required_index=2", time.Second) &&
		!logger.waitContains("state=publication_suppressed_explicit_release, required_index=2", time.Second) {
		return errors.New("missing honest second-completion publication or teardown-suppression marker")
	}
	if dropped, timedOut := diagnostic.MarkerDeliveryStats(); dropped != 0 || timedOut != 0 {
		return fmt.Errorf("diagnostic marker delivery dropped=%d timed_out=%d, want 0 and 0", dropped, timedOut)
	}
	fmt.Fprintln(os.Stderr, "mullion: issue135 diagnostic assertion, state=no_registration_asset_watchdog_navigate_frontend_or_host_ready")
	return nil
}

func forbiddenPostCancellationSuccessMarkers() []string {
	return []string{
		"asset serving ready",
		"injected scripts registered",
		"frontend render timeout",
		"navigate requested",
		"frontend ready",
		"native host ready",
	}
}
