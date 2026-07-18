package host

import "testing"

// panickingLogger is an embedder Logger whose every method panics - the
// degenerate caller the sink must survive (issue #26). The sink is invoked
// from timer goroutines with no recover above them (the render watchdog and
// the startup show gate) and from the window procedure's panic reporter, where
// a fresh panic aborts the process.
type panickingLogger struct{}

func (panickingLogger) Debug(string) { panic("logger boom") }
func (panickingLogger) Info(string)  { panic("logger boom") }
func (panickingLogger) Warn(string)  { panic("logger boom") }
func (panickingLogger) Error(string) { panic("logger boom") }

// TestLogSinkSurvivesAPanickingLogger locks the contract that no panic out of
// the caller-supplied Logger escapes the sink, and that the warn and error
// counts still record the lines the Logger failed to deliver - the startup
// summary must stay truthful about how noisy the run was even when the Logger
// itself is the broken part.
func TestLogSinkSurvivesAPanickingLogger(t *testing.T) {
	sink := newLogSink(panickingLogger{})

	sink.Debug("d")
	sink.Info("i")
	sink.Warn("w")
	sink.Error("e")

	if got := sink.WarnCount(); got != 1 {
		t.Fatalf("WarnCount = %d, want 1: the count must land even when the Logger dies", got)
	}
	if got := sink.ErrorCount(); got != 1 {
		t.Fatalf("ErrorCount = %d, want 1: the count must land even when the Logger dies", got)
	}
}
