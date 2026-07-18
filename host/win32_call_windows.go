//go:build windows

package host

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func setWindowText(hwnd windowHandle, text string) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return err
	}
	result, _, callErr := procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ptr)))
	if result == 0 {
		return syscallError(callErr)
	}
	return nil
}

func getModuleHandle() (windowHandle, error) {
	result, _, err := procGetModuleHandle.Call(0)
	if result == 0 {
		return 0, syscallError(err)
	}
	return windowHandle(result), nil
}

func registerWindowClass(className string, instance, cursor windowHandle, wndProc uintptr) error {
	name, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return err
	}
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   wndProc,
		Instance:  instance,
		Cursor:    cursor,
		ClassName: name,
	}
	result, _, callErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	if result == 0 {
		err := syscallError(callErr)
		if err == nil {
			err = windows.ERROR_INVALID_PARAMETER
		}
		return err
	}
	return nil
}

func unregisterWindowClass(className string, instance windowHandle) {
	name, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return
	}
	procUnregisterClass.Call(uintptr(unsafe.Pointer(name)), uintptr(instance))
}

// createWin32Window registers the class and creates the HWND. It is named apart
// from (*Host).createWindow, which is the caller: the two would otherwise shadow
// each other confusingly on the same receiver.
func (host *Host) createWin32Window(className, title string, instance windowHandle, wndProc uintptr, width, height int32) (windowHandle, error) {
	cursor, _, _ := procLoadCursor.Call(0, 32512)
	if err := registerWindowClass(className, instance, windowHandle(cursor), wndProc); err != nil {
		return 0, fmt.Errorf("RegisterClassEx: %w", err)
	}
	class, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0, err
	}
	windowTitle, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	result, _, callErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(windowTitle)),
		nativeInitialWindowStyle(),
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		uintptr(width), uintptr(height),
		0, 0, uintptr(instance), 0,
	)
	if result == 0 {
		unregisterWindowClass(className, instance)
		err := syscallError(callErr)
		if err == nil {
			err = windows.ERROR_INVALID_WINDOW_HANDLE
		}
		return 0, fmt.Errorf("CreateWindow: %w", err)
	}
	hwnd := windowHandle(result)
	if err := host.applyNativeWindowStyle(hwnd); err != nil {
		host.log.Warn("mullion: native titlebar style clear failed, reason=" + logsafe.Reason(err))
	}
	return hwnd, nil
}

func defWindowProc(hwnd windowHandle, message uint32, wParam, lParam uintptr) uintptr {
	result, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return result
}

func postWindowMessage(hwnd windowHandle, message uint32) error {
	return postWindowMessageArgs(hwnd, message, 0, 0)
}

func postWindowMessageArgs(hwnd windowHandle, message uint32, wParam, lParam uintptr) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procPostMessage.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	if result == 0 {
		return syscallError(err)
	}
	return nil
}

func showWindow(hwnd windowHandle, command int32) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	procShowWindow.Call(uintptr(hwnd), uintptr(command))
	return nil
}

func updateWindow(hwnd windowHandle) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procUpdateWindow.Call(uintptr(hwnd))
	if result == 0 {
		return syscallError(err)
	}
	return nil
}

func setForegroundWindow(hwnd windowHandle) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, err := procSetForegroundWindow.Call(uintptr(hwnd))
	if result == 0 {
		return syscallError(err)
	}
	return nil
}

func getClientRect(hwnd windowHandle) (rect, error) {
	if hwnd == 0 {
		return rect{}, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	var client rect
	result, _, err := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&client)))
	if result == 0 {
		return rect{}, syscallError(err)
	}
	return client, nil
}

func getWindowRectWithError(hwnd windowHandle) (rect, error) {
	if hwnd == 0 {
		return rect{}, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	var window rect
	result, _, err := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&window)))
	if result == 0 {
		return rect{}, syscallError(err)
	}
	return window, nil
}

func releaseCapture() error {
	result, _, err := procReleaseCapture.Call()
	if result == 0 {
		return syscallError(err)
	}
	return nil
}

func getCursorPos() (point, error) {
	var cursor point
	result, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	if result == 0 {
		return point{}, syscallError(err)
	}
	return cursor, nil
}

func sendWindowMessage(hwnd windowHandle, message uint32, wParam, lParam uintptr) error {
	_, err := sendWindowMessageResult(hwnd, message, wParam, lParam)
	return err
}

func sendWindowMessageResult(hwnd windowHandle, message uint32, wParam, lParam uintptr) (uintptr, error) {
	if hwnd == 0 {
		return 0, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, _ := procSendMessage.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return result, nil
}

// warnIf logs a failed Win32 call and swallows it. Most of the calls it guards
// are advisory (a redraw hint, a cursor update): failing them degrades the
// window, it does not invalidate it, so the host keeps running and leaves a
// trace instead of tearing down.
func (host *Host) warnIf(action string, err error) {
	if err != nil {
		host.log.Warn("mullion: " + action + " failed, reason=" + logsafe.Reason(err))
	}
}

// guardedWindowProc wraps a window procedure so a Go panic cannot escape into
// the native DispatchMessage frame that invoked it - which would abort the
// process, taking the orderly WM_DESTROY teardown with it. On a panic it reports
// through onPanic - behind its own recover, because the reporter runs after this
// guard's recover has been spent, and a reporter that panics would otherwise
// re-abort the process the guard exists to protect (issue #26) - and returns
// fallback's result, keeping the window alive. This mirrors the recover the COM
// event handlers already have (internal/webview2); the window procedure is the
// other native -> Go boundary and needs the same guarantee - not least because
// it invokes Config.OnClose, which is caller code.
func guardedWindowProc(
	proc func(windowHandle, uint32, uintptr, uintptr) uintptr,
	fallback func(windowHandle, uint32, uintptr, uintptr) uintptr,
	onPanic func(recovered any, message uint32),
) func(windowHandle, uint32, uintptr, uintptr) uintptr {
	return func(hwnd windowHandle, message uint32, wParam, lParam uintptr) (result uintptr) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if onPanic != nil {
					// A panic here has no catcher above it: this defer's own
					// recover has already fired. The report is lost, the
					// window survives - the right half of that trade.
					func() {
						defer func() { _ = recover() }()
						onPanic(recovered, message)
					}()
				}
				result = fallback(hwnd, message, wParam, lParam)
			}
		}()
		return proc(hwnd, message, wParam, lParam)
	}
}

func newWindowCallback(
	callback func(windowHandle, uint32, uintptr, uintptr) uintptr,
	onPanic func(recovered any, message uint32),
) uintptr {
	guarded := guardedWindowProc(callback, defWindowProc, onPanic)
	return windows.NewCallback(func(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
		return guarded(windowHandle(hwnd), message, wParam, lParam)
	})
}

// reportWindowProcPanic logs a panic that guardedWindowProc caught before it
// could unwind into the native message-dispatch frame. It can itself panic in
// two ways, both caller code: fmt.Sprint(recovered) may invoke a String or
// Error method on the recovered value, and the log line ends at the caller's
// Logger. Both are contained - logSink swallows a panicking Logger, and
// guardedWindowProc runs this reporter behind its own recover (issue #26).
func (host *Host) reportWindowProcPanic(recovered any, message uint32) {
	host.log.Error(fmt.Sprintf("mullion: window procedure recovered from panic, message=0x%x, reason=%s",
		message, logsafe.Message(fmt.Sprint(recovered))))
}

func syscallError(err error) error {
	if err == windows.ERROR_SUCCESS {
		return nil
	}
	return err
}
