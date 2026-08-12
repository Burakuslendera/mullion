//go:build windows

package host

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Burakuslendera/mullion/internal/logsafe"
)

func setWindowTextPointer(hwnd windowHandle, text uintptr) error {
	if hwnd == 0 {
		return windows.ERROR_INVALID_WINDOW_HANDLE
	}
	result, _, callErr := procSetWindowText.Call(uintptr(hwnd), text)
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

// prepareWindowStrings performs every fallible string conversion before
// createWin32Window acquires ownership of a registered class. In particular, a
// caller-supplied title containing NUL must fail here: returning after
// RegisterClassEx would strand that class for the rest of the process.
func prepareWindowStrings(className, title string) (*uint16, *uint16, error) {
	class, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return nil, nil, err
	}
	windowTitle, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return nil, nil, err
	}
	return class, windowTitle, nil
}

// createWin32Window registers the class and creates the HWND. It is named apart
// from (*Host).createWindow, which is the caller: the two would otherwise shadow
// each other confusingly on the same receiver.
//
// x and y are uintptr, not int32, because one of their legal values is
// CW_USEDEFAULT (0x80000000) - the fallback when placement could not be
// resolved - and that constant does not fit an int32.
func (host *Host) createWin32Window(className, title string, instance windowHandle, wndProc, token, x, y uintptr, width, height int32) (windowHandle, error) {
	class, windowTitle, err := prepareWindowStrings(className, title)
	if err != nil {
		return 0, err
	}
	cursor, _, _ := procLoadCursor.Call(0, 32512)
	if err := registerWindowClass(className, instance, windowHandle(cursor), wndProc); err != nil {
		return 0, fmt.Errorf("RegisterClassEx: %w", err)
	}
	result, _, callErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(windowTitle)),
		nativeInitialWindowStyle(),
		x, y,
		uintptr(width), uintptr(height),
		0, 0, uintptr(instance), token,
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
				// fallback runs in the same spent-recover region and is trusted
				// not to panic: production passes defWindowProc, a plain user32
				// call.
				result = fallback(hwnd, message, wParam, lParam)
			}
		}()
		return proc(hwnd, message, wParam, lParam)
	}
}

// windowProcRegistry keeps only scalar callback identity in the process-wide
// callback. A pending token becomes active during WM_NCCREATE, is validated
// against both GWLP_USERDATA and HWND for every later dispatch, then is evicted
// during WM_NCDESTROY. This avoids both callback-table growth and recycled-HWND
// misdispatch.
type windowProcRegistry struct {
	mu      sync.RWMutex
	next    uintptr
	pending map[uintptr]*Host
	active  map[uintptr]windowProcTarget
}

type windowProcTarget struct {
	host *Host
	hwnd windowHandle
}

func newWindowProcRegistry() *windowProcRegistry {
	return &windowProcRegistry{
		pending: make(map[uintptr]*Host),
		active:  make(map[uintptr]windowProcTarget),
	}
}

// reserve creates an identity that is absent from both states. The caller owns a
// pending entry until WM_NCCREATE promotes it or the failed creation rolls it
// back; never reuse an active token when uintptr wraps.
func (registry *windowProcRegistry) reserve(host *Host) uintptr {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for {
		registry.next++
		if registry.next == 0 {
			continue
		}
		if _, pending := registry.pending[registry.next]; pending {
			continue
		}
		if _, active := registry.active[registry.next]; active {
			continue
		}
		registry.pending[registry.next] = host
		return registry.next
	}
}

func (registry *windowProcRegistry) rollback(token uintptr, host *Host) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.pending[token] == host {
		delete(registry.pending, token)
	}
}

func (registry *windowProcRegistry) promote(hwnd windowHandle, token uintptr) *Host {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	host := registry.pending[token]
	if host == nil || hwnd == 0 {
		return nil
	}
	delete(registry.pending, token)
	registry.active[token] = windowProcTarget{host: host, hwnd: hwnd}
	return host
}

func (registry *windowProcRegistry) resolve(hwnd windowHandle, token uintptr) *Host {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	target, ok := registry.active[token]
	if !ok || target.hwnd != hwnd {
		return nil
	}
	return target.host
}

func (registry *windowProcRegistry) evict(hwnd windowHandle, token uintptr) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if target, ok := registry.active[token]; ok && target.hwnd == hwnd {
		delete(registry.active, token)
	}
}

func (registry *windowProcRegistry) evictWindow(hwnd windowHandle) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for token, target := range registry.active {
		if target.hwnd == hwnd {
			delete(registry.active, token)
		}
	}
}

var sharedWindowProcHosts = newWindowProcRegistry()

var sharedWindowProcOnce sync.Once
var sharedWindowProc uintptr

// sharedWindowProcCallback allocates the one process-lifetime trampoline only
// after Run passed its architecture gate and createWindow passed all fallible
// caller-input validation. It captures no Host, so later windows neither consume
// another callback-table slot nor retain their Host graph.
func sharedWindowProcCallback() uintptr {
	sharedWindowProcOnce.Do(func() {
		sharedWindowProc = windows.NewCallback(sharedWindowProcTrampoline)
	})
	return sharedWindowProc
}

func createWindowProcToken(lParam uintptr) (uintptr, bool) {
	if lParam == 0 {
		return 0, false
	}
	var create struct{ createParams uintptr }
	copyFromWindowPointer(unsafe.Pointer(&create), lParam, unsafe.Sizeof(create))
	return create.createParams, create.createParams != 0
}

func setWindowProcToken(hwnd windowHandle, token uintptr) error {
	result, _, err := procSetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlpUserData), token)
	if result == 0 && err != windows.ERROR_SUCCESS {
		return syscallError(err)
	}
	return nil
}

func windowProcToken(hwnd windowHandle) (uintptr, error) {
	result, _, err := procGetWindowLongPtr.Call(uintptr(hwnd), windowLongIndex(gwlpUserData))
	if result == 0 && err != windows.ERROR_SUCCESS {
		return 0, syscallError(err)
	}
	return result, nil
}

func sharedWindowProcTrampoline(rawHWND uintptr, message uint32, wParam, lParam uintptr) uintptr {
	hwnd := windowHandle(rawHWND)
	if message == wmNCCreate {
		token, ok := createWindowProcToken(lParam)
		if !ok {
			return 0
		}
		host := sharedWindowProcHosts.promote(hwnd, token)
		if host == nil {
			return 0
		}
		if err := setWindowProcToken(hwnd, token); err != nil {
			sharedWindowProcHosts.evict(hwnd, token)
			return 0
		}
		result := invokeWindowProc(host, hwnd, message, wParam, lParam)
		if result == 0 {
			_ = setWindowProcToken(hwnd, 0)
			sharedWindowProcHosts.evict(hwnd, token)
		}
		return result
	}

	token, err := windowProcToken(hwnd)
	if err != nil {
		if message == wmNCDestroy {
			sharedWindowProcHosts.evictWindow(hwnd)
		}
		return defWindowProc(hwnd, message, wParam, lParam)
	}
	host := sharedWindowProcHosts.resolve(hwnd, token)
	if host == nil {
		if message == wmNCDestroy {
			// Userdata may have been replaced before destruction. Evict by HWND
			// rather than leaving the original active Host reachable forever.
			sharedWindowProcHosts.evictWindow(hwnd)
		}
		return defWindowProc(hwnd, message, wParam, lParam)
	}
	result := invokeWindowProc(host, hwnd, message, wParam, lParam)
	if message == wmNCDestroy {
		_ = setWindowProcToken(hwnd, 0)
		sharedWindowProcHosts.evictWindow(hwnd)
	}
	return result
}

func invokeWindowProc(host *Host, hwnd windowHandle, message uint32, wParam, lParam uintptr) (result uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			func() {
				defer func() { _ = recover() }()
				host.reportWindowProcPanic(recovered, message)
			}()
			result = defWindowProc(hwnd, message, wParam, lParam)
		}
	}()
	return host.windowProc(hwnd, message, wParam, lParam)
}

// reportWindowProcPanic logs a panic that guardedWindowProc caught before it
// could unwind into the native message-dispatch frame. Caller code runs in two
// places here, each with its own containment: fmt.Sprint(recovered) may invoke
// a String or Error method on the recovered value, which fmt itself contains
// and renders as %!v(PANIC=...); and the log line ends at the caller's Logger,
// which logSink contains. guardedWindowProc still runs this reporter behind
// its own recover as the backstop for whatever neither layer catches
// (issue #26).
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
