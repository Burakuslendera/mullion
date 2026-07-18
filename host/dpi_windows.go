//go:build windows

package host

import (
	"errors"
)

var (
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetThreadDpiAwarenessContext  = user32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual  = user32.NewProc("AreDpiAwarenessContextsEqual")
)

const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

func enablePerMonitorV2DPIAwareness() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return err
	}
	result, _, callErr := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
	if result != 0 {
		return nil
	}
	// Awareness latches once per process, so the set call answers
	// ERROR_ACCESS_DENIED for a second Host in the same process - or for an
	// application that declared PMv2 itself before constructing the host - even
	// though the process is already in exactly the state this function exists
	// to establish. Ask the thread (it inherits the process default) and treat
	// an already-correct context as success; a genuinely different or unknown
	// context stays the error it always was (issue #48, found live by the
	// second-Run check).
	if alreadyPerMonitorV2DPIAware() {
		return nil
	}
	if err := syscallError(callErr); err != nil {
		return err
	}
	return errors.New("set process dpi awareness context")
}

// alreadyPerMonitorV2DPIAware reports whether this thread - and, absent a
// thread override, the process - is already per-monitor-v2 DPI aware.
func alreadyPerMonitorV2DPIAware() bool {
	if procGetThreadDpiAwarenessContext.Find() != nil || procAreDpiAwarenessContextsEqual.Find() != nil {
		return false
	}
	current, _, _ := procGetThreadDpiAwarenessContext.Call()
	if current == 0 {
		return false
	}
	equal, _, _ := procAreDpiAwarenessContextsEqual.Call(current, dpiAwarenessContextPerMonitorAwareV2)
	return equal != 0
}
