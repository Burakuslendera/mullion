package host

import (
	"io"
	"os"
	"sync"
)

// ANSI SGR sequences ColourLogger uses, one per level. They are emitted only
// when the sink is an interactive terminal (see enableColour); a file or a pipe
// receives plain text. Error is the loudest - bold red - so the rarest and most
// serious lines (a recovered window-procedure panic among them) stand out at a
// glance in a live log.
const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[2m"
	ansiYellow  = "\x1b[33m"
	ansiBoldRed = "\x1b[1;31m"
)

// ColourLogger returns a Logger that writes level-coloured lines to w. It is an
// opt-in convenience for an application whose log goes to a terminal:
//
//	host.New(host.Config{Assets: assets, Logger: host.ColourLogger(os.Stderr)})
//
// Colour is emitted only when w is an interactive terminal that can render
// ANSI sequences and the NO_COLOR environment variable
// (https://no-color.org) is unset; a redirected file or a pipe receives plain,
// unescaped text, so a captured log never carries stray escape sequences. The
// one degraded case is announced: a console that refuses virtual-terminal
// processing (the legacy conhost mode) gets a single plain WARN line saying
// colour is disabled, because the colours would otherwise vanish with no
// trace of why (issue #28). NO_COLOR and non-terminal sinks stay silent -
// the first is explicit intent, the second is normal redirection. Every
// message reaches the Logger already sanitised by the host -
// internal/logsafe strips control bytes out of user-supplied strings - so the
// only escape sequences in the output are the ones this logger adds.
//
// A nil w yields a NopLogger.
func ColourLogger(w io.Writer) Logger {
	if w == nil {
		return NopLogger{}
	}
	colour, vtRefused := enableColour(w)
	logger := &colourLogger{w: w, colour: colour}
	if vtRefused {
		logger.Warn("mullion: colour disabled, reason=console cannot enable virtual terminal processing")
	}
	return logger
}

type colourLogger struct {
	mu     sync.Mutex
	w      io.Writer
	colour bool
}

func (l *colourLogger) write(code, message string) {
	line := colourLine(code, message, l.colour)
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, line)
}

func (l *colourLogger) Debug(msg string) { l.write(ansiDim, msg) }
func (l *colourLogger) Info(msg string)  { l.write("", msg) }
func (l *colourLogger) Warn(msg string)  { l.write(ansiYellow, msg) }
func (l *colourLogger) Error(msg string) { l.write(ansiBoldRed, msg) }

// colourLine wraps message in an SGR colour code and a reset, or returns it
// plain when colour is off or no code applies. It is separate from the writer so
// the rendering can be unit-tested without a terminal.
func colourLine(code, message string, colour bool) string {
	if !colour || code == "" {
		return message + "\n"
	}
	return code + message + ansiReset + "\n"
}

// enableColour decides whether to emit ANSI for w: never when NO_COLOR is set,
// otherwise only when w is an interactive terminal that renders VT sequences.
// vtRefused passes through isTerminal's degradation signal so ColourLogger can
// announce it; NO_COLOR short-circuits before any console probe and is never a
// refusal. isTerminal is platform specific; off Windows it is always false,
// because the window host does not run there anyway (Run returns
// ErrUnsupportedPlatform).
func enableColour(w io.Writer) (colour, vtRefused bool) {
	if _, noColour := os.LookupEnv("NO_COLOR"); noColour {
		return false, false
	}
	return isTerminal(w)
}
