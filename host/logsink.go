package host

import "sync/atomic"

// logSink wraps the caller's Logger and counts warnings and errors. The counts
// are per-Host, which lets the startup summary report "this window came up with
// N warnings" without putting counters on the public Logger interface.
type logSink struct {
	out    Logger
	warns  atomic.Int64
	errors atomic.Int64
}

func newLogSink(logger Logger) *logSink {
	if logger == nil {
		logger = NopLogger{}
	}
	return &logSink{out: logger}
}

func (sink *logSink) Debug(message string) { sink.out.Debug(message) }
func (sink *logSink) Info(message string)  { sink.out.Info(message) }

func (sink *logSink) Warn(message string) {
	sink.warns.Add(1)
	sink.out.Warn(message)
}

func (sink *logSink) Error(message string) {
	sink.errors.Add(1)
	sink.out.Error(message)
}

func (sink *logSink) WarnCount() int64  { return sink.warns.Load() }
func (sink *logSink) ErrorCount() int64 { return sink.errors.Load() }
