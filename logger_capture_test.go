package mullion

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// captureLogger collects log lines in memory. The suite deliberately never
// writes a log file or touches the file system: the tests must stay runnable on
// a headless machine with no WebView2 runtime and no writable app data
// directory.
type captureLogger struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (logger *captureLogger) Debug(message string) { logger.write("DEBUG", message) }
func (logger *captureLogger) Info(message string)  { logger.write("INFO", message) }
func (logger *captureLogger) Warn(message string)  { logger.write("WARN", message) }
func (logger *captureLogger) Error(message string) { logger.write("ERROR", message) }

func (logger *captureLogger) write(level, message string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.lines.WriteString("level=" + level + " msg=" + message + "\n")
}

func (logger *captureLogger) String() string {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.lines.String()
}

// newTestHost builds a Host wired to a capture logger. It never creates a window
// and never starts a message loop.
func newTestHost(t *testing.T, config Config) (*Host, *captureLogger) {
	t.Helper()
	logger := &captureLogger{}
	config.Logger = logger
	return New(config), logger
}

// waitForLog polls until the wanted substring shows up. The host writes some
// lines from timers on their own goroutines, so an immediate read would race.
func waitForLog(t *testing.T, logger *captureLogger, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		text := logger.String()
		if strings.Contains(text, want) {
			return text
		}
		if time.Now().After(deadline) {
			t.Fatalf("log did not contain %q within %s\n%s", want, timeout, text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
