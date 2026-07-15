//go:build windows

package host

import (
	"errors"
)

var procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")

const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

func enablePerMonitorV2DPIAwareness() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return err
	}
	result, _, callErr := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
	if result != 0 {
		return nil
	}
	if err := syscallError(callErr); err != nil {
		return err
	}
	return errors.New("set process dpi awareness context")
}
