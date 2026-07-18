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

// Every hand-off to the caller's Logger runs behind its own recover. The
// Logger is embedder code, and the sink is invoked from places where a fresh
// panic aborts the process: goroutines with no recover above them (the render
// watchdog and the startup show gate fire from timers) and the window
// procedure's panic reporter, which runs after the guard's recover has already
// been spent (issue #26). There is nowhere to report the swallowed panic - the
// Logger IS the sink - so the line is lost and the process stays alive, which
// is the better half of that trade. The counts land before the hand-off, so
// the startup summary stays truthful even about lines the Logger dropped.
// The containment covers a synchronous panic; runtime.Goexit, and a panic on
// a goroutine the Logger itself spawned, are beyond any recover's reach here
// as everywhere.

func (sink *logSink) Debug(message string) {
	defer func() { _ = recover() }()
	sink.out.Debug(message)
}

func (sink *logSink) Info(message string) {
	defer func() { _ = recover() }()
	sink.out.Info(message)
}

func (sink *logSink) Warn(message string) {
	sink.warns.Add(1)
	defer func() { _ = recover() }()
	sink.out.Warn(message)
}

func (sink *logSink) Error(message string) {
	sink.errors.Add(1)
	defer func() { _ = recover() }()
	sink.out.Error(message)
}

func (sink *logSink) WarnCount() int64  { return sink.warns.Load() }
func (sink *logSink) ErrorCount() int64 { return sink.errors.Load() }
