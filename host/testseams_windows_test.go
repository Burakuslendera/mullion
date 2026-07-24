//go:build windows

package host

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// The two seams that keep the headless suite from reaching out of the process
// (issue #76). Both have a no-op twin for the non-Windows build, because
// leak_test.go and the pure-logic tests run on the Linux CI job as well.

// hideChildConsole keeps a child process from flashing a console window.
//
// The leak-scan locks (issues #29 and #71) have to run the real script against a
// real throwaway repository, so they start git and pwsh for real - and a console
// process started from a test binary gets its own console, which appears and
// disappears on the developer's desktop several times per `go test` run. That is
// only noise, but noise during a test run is the kind of thing that gets a suite
// run less often. CREATE_NO_WINDOW suppresses the console without detaching the
// process or touching its pipes, so CombinedOutput still captures everything.
func hideChildConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

// stubExternalOpen makes the system-browser hand-off a no-op for a test host, so
// no test can launch a browser whatever URL it drives the routing with. A test
// that wants to assert the routing overwrites host.openExternal with a recorder.
func stubExternalOpen(host *Host) {
	host.openExternal = func(string) {}
}
