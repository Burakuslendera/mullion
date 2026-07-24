//go:build !windows

package host

import "os/exec"

// The non-Windows twins of the test seams (issue #76). There is no console
// window to suppress here, and Host has no system-browser routing to stub - the
// tests that run on this build are the pure-logic and leak-scan locks.

func hideChildConsole(*exec.Cmd) {}

func stubExternalOpen(*Host) {}
