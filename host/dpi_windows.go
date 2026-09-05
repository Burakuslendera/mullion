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
	success, err := classifyDPIAwarenessResult(result, alreadyPerMonitorV2DPIAware(), callErr)
	if success {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("set process dpi awareness context")
}

// classifyDPIAwarenessResult is the allocation-free, deterministic portion of
// enablePerMonitorV2DPIAwareness. Success is separate from err so the caller
// retains the existing generic fallback while native errors keep their identity.
func classifyDPIAwarenessResult(result uintptr, alreadyAware bool, callErr error) (success bool, err error) {
	if result != 0 || alreadyAware {
		return true, nil
	}
	if err := syscallError(callErr); err != nil {
		return false, err
	}
	return false, nil
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
