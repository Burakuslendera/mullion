//go:build windows

package host

import (
	"io"

	"golang.org/x/sys/windows"
)

// enableVirtualTerminalProcessing is the console output mode bit that makes
// conhost interpret ANSI (virtual terminal) sequences instead of printing them.
const enableVirtualTerminalProcessing = 0x0004

// getConsoleMode and setConsoleMode are the two Win32 calls behind isTerminal.
// They are variables only so the headless tests can play a legacy console -
// GetConsoleMode succeeds but the VT bit is refused - which cannot be produced
// on a modern conhost (issue #28, decision 0006); production code never
// reassigns them.
var (
	getConsoleMode = windows.GetConsoleMode
	setConsoleMode = windows.SetConsoleMode
)

// isTerminal reports whether w is a Windows console that will actually render
// ANSI (virtual terminal) sequences. Windows Terminal and Win10+ conhost
// support VT; a classic console may start without it, so the mode is enabled
// here.
//
// vtRefused singles out the one degraded case worth telling the user about:
// a console - the user-toggleable legacy conhost - that answered
// GetConsoleMode but refused the VT bit. Colour is disabled there so the
// escapes are not printed verbatim (issue #28), and ColourLogger announces the
// degradation once, because a user who toggled the legacy console would
// otherwise never learn why the colours vanished. A file or a pipe is not a
// refusal - it is simply not a terminal - and stays silent.
func isTerminal(w io.Writer) (colour, vtRefused bool) {
	fd, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false, false
	}
	handle := windows.Handle(fd.Fd())
	var mode uint32
	if err := getConsoleMode(handle, &mode); err != nil {
		return false, false // a file or a pipe, not a console
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true, false
	}
	if setConsoleMode(handle, mode|enableVirtualTerminalProcessing) != nil {
		return false, true
	}
	return true, false
}
