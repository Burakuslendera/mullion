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
// here. When enabling fails - the user-toggleable legacy console accepts
// GetConsoleMode but refuses the VT bit - this returns false, so the logger
// degrades to plain text instead of printing the escapes verbatim (issue #28).
// A failure is never a crash, and never raw escapes.
func isTerminal(w io.Writer) bool {
	fd, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	handle := windows.Handle(fd.Fd())
	var mode uint32
	if err := getConsoleMode(handle, &mode); err != nil {
		return false // a file or a pipe, not a console
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	return setConsoleMode(handle, mode|enableVirtualTerminalProcessing) == nil
}
