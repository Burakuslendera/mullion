//go:build windows

package host

import (
	"fmt"
	"sync/atomic"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

// --- recovered handler panics ------------------------------------------------

// Every WebView2 event handler recovers its own panics, because an escaping one
// would unwind into a Chromium stack frame and take the process down
// (internal/webview2/handlers_windows.go). Recovering is only half of it: the
// callback did not finish, the window keeps running as if it had, and with no
// reporter installed internal/webview2 falls back to a line on os.Stderr - which
// an embedder who captures Config.Logger, or ships a windowed binary with no
// console at all, never sees. These three declarations are the route from that
// recover to Config.Logger.
//
// The reporter is process-global, because the hook it feeds is: one package-level
// slot in internal/webview2, which is the right shape there (the handler vtable
// is process-global too, and the report carries an event name, not a sender). The
// wiring is per-host, so the two have to be reconciled:
//
//   - handlerPanicTarget is the sink the newest embed published. A second host in
//     the same process is a sequential one - the window class is unregistered when
//     Run returns so a later host can reuse it (docs/architecture.md) - and by the
//     time it embeds, the previous host's handlers are gone and can no longer
//     report, so "newest embed wins" is exactly right for it. Two hosts running
//     concurrently (two Run goroutines, distinct ClassName values) is the case
//     that stays approximate: the older host's panics land in the newer host's
//     logger, named by event but not by window. That is the same process and
//     usually the same log, and the alternative - the report vanishing to stderr -
//     is what this wiring exists to end.
//   - The target is a published *logSink rather than a closure over the Host, so
//     the process-global hook cannot pin a torn-down Host, its Browser and its
//     timers alive for the life of the process.
//   - reportWebViewHandlerPanic is a stable function value, so re-installing on
//     every embed is idempotent and nothing accumulates.
var handlerPanicTarget atomic.Pointer[logSink]

// installHandlerPanicLogging points internal/webview2's panic reporter at this
// host's logger. The target is published before the hook is installed, so the
// hook can never fire against an empty slot.
func (host *Host) installHandlerPanicLogging() {
	handlerPanicTarget.Store(host.log)
	webview2.SetHandlerPanicHook(reportWebViewHandlerPanic)
}

// reportWebViewHandlerPanic logs a panic that an event handler recovered before
// it could unwind into the runtime's stack. It runs on the UI thread, inside the
// handler's spent recover, so it is written not to panic on its own: fmt.Sprint
// may invoke a String or Error method on the recovered value, which fmt itself
// contains and renders as %!v(PANIC=...), and the hand-off to the caller's
// Logger is contained by logSink - the same two layers reportWindowProcPanic
// relies on (issue #26). internal/webview2 keeps its own recover around this
// call as the backstop for whatever neither catches.
//
// The panic value and the stack are untrusted text: a panic message carries
// whatever a callback put in it, and a stack carries the developer's paths.
// Both go through logsafe, which folds control bytes and newlines - no report
// may inject a line or a terminal escape into the log - and reduces paths to
// their base name.
func reportWebViewHandlerPanic(event string, recovered any, stack []byte) {
	sink := handlerPanicTarget.Load()
	if sink == nil {
		// Unreachable: the hook is only installed after a target is published.
		return
	}
	sink.Error("mullion: webview2 handler recovered from panic, event=" + logsafe.Message(event) +
		", reason=" + logsafe.Message(fmt.Sprint(recovered)))
	if len(stack) == 0 {
		return
	}
	// The stack is what turns "a navigation callback died" into a line number, so
	// it is worth carrying - at Debug, and clamped, because a goroutine dump is
	// unbounded and the ERROR above has to stay readable.
	sink.Debug("mullion: webview2 handler panic stack, event=" + logsafe.Message(event) +
		", stack=" + logsafe.Message(clampStackForLog(stack)))
}

// clampStackForLog bounds a recovered handler's stack before it is reduced for
// the log. debug.Stack writes the innermost frames first, so the head is the
// part that names the callback that panicked; the cut can land mid-line and
// mid-rune, which logsafe's reduction tolerates.
func clampStackForLog(stack []byte) string {
	const limit = 2000
	if len(stack) <= limit {
		return string(stack)
	}
	return string(stack[:limit])
}
