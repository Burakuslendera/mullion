package host

import "log/slog"

// The Logger surface an embedder implements or picks: the interface itself, the
// no-op default Config.Logger falls back to, and the slog adapter. Split from
// config.go, which keeps the Config struct and its derivation.
//
// This is the public half of the host's logging only. The counting sink the
// host actually calls through is logsink.go; the terminal implementation is
// colour.go. Portable on purpose: every platform's build refers to these types,
// so nothing here may reach for a Win32 shim.

// Logger receives the host's diagnostic output. Every message is already
// sanitised: file system paths are reduced to their base name before they reach
// the logger, so a Logger implementation may forward messages verbatim without
// leaking user paths.
//
// Messages are pre-formatted single strings on purpose. Some of them are emitted
// from hot paths (WM_SIZE, WM_MOVE), and a variadic signature would push
// formatting work into those paths for no benefit.
//
// An implementation must be safe to call from more than one goroutine. Most
// lines come from the UI thread, but not all: the render watchdog and the
// startup show gate write from timers, and handing a URL to the system browser
// writes from the worker it runs on (decisions/0029). A Logger holding state - a
// buffer, a file handle, a counter of its own - needs its own lock.
//
// A Logger that panics is contained, not fatal: the line is dropped, the
// process keeps running, and the warn/error counts still record the attempt
// (issue #26). The host calls the Logger from goroutines with no recover above
// them, so an uncontained panic there would abort the whole process.
type Logger interface {
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// NopLogger discards every message. It is the default when Config.Logger is nil.
type NopLogger struct{}

func (NopLogger) Debug(string) {}
func (NopLogger) Info(string)  {}
func (NopLogger) Warn(string)  {}
func (NopLogger) Error(string) {}

type slogLogger struct{ logger *slog.Logger }

func (l slogLogger) Debug(msg string) { l.logger.Debug(msg) }
func (l slogLogger) Info(msg string)  { l.logger.Info(msg) }
func (l slogLogger) Warn(msg string)  { l.logger.Warn(msg) }
func (l slogLogger) Error(msg string) { l.logger.Error(msg) }

// SlogLogger adapts a *slog.Logger to the Logger interface.
func SlogLogger(logger *slog.Logger) Logger {
	if logger == nil {
		return NopLogger{}
	}
	return slogLogger{logger: logger}
}
