//go:build windows

package host

import (
	"io"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether w is a Windows console and, if so, makes sure the
// console interprets ANSI (virtual terminal) sequences. Windows Terminal and
// Win10+ conhost support it; a classic console may start without it, so the mode
// is enabled here. The call is idempotent and harmless, and a failure just means
// colour will not render - never a crash.
func isTerminal(w io.Writer) bool {
	fd, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	handle := windows.Handle(fd.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false // a file or a pipe, not a console
	}
	const enableVirtualTerminalProcessing = 0x0004
	if mode&enableVirtualTerminalProcessing == 0 {
		_ = windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing)
	}
	return true
}
