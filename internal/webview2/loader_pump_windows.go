//go:build windows

package webview2

// Waiting on the UI thread: the message pump that keeps dispatching while a
// COM completion is outstanding. Split from loader_windows.go, which keeps the
// creation entry points.

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	procPeekMessage               = user32.NewProc("PeekMessageW")
	procTranslateMessage          = user32.NewProc("TranslateMessage")
	procDispatchMessage           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procMsgWaitForMultipleObjects = user32.NewProc("MsgWaitForMultipleObjectsEx")
)

const (
	wmQuit             = 0x0012
	pmRemove           = 0x0001
	qsAllInput         = 0x04FF
	mwmoInputAvailable = 0x0004

	// How long a single wait blocks before the deadline is re-checked. Long
	// enough not to spin, short enough that a timeout is reported promptly.
	pumpSliceMS = 20
)

// win32Msg mirrors MSG.
type win32Msg struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// pump dispatches window messages while we wait for a COM completion handler.
//
// It cannot simply sleep: WebView2 delivers the callback *through* the message
// queue, so a caller that blocks without dispatching waits for a message it is
// itself preventing from being delivered.
type pump struct {
	quitSeen bool
	quitCode uintptr
}

func (p *pump) step() {
	var message win32Msg
	for {
		got, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmRemove)
		if got == 0 {
			break
		}
		if message.message == wmQuit {
			// WM_QUIT is not dispatchable, and swallowing it would strand an
			// application that asked to exit while the WebView was still
			// starting. Remember it and put it back once the wait is over.
			p.quitSeen = true
			p.quitCode = message.wParam
			continue
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	// Block until something arrives rather than spinning. MWMO_INPUTAVAILABLE
	// makes the wait return at once if a message was posted between the drain
	// above and this call - the race a bare WaitMessage would lose.
	_, _, _ = procMsgWaitForMultipleObjects.Call(0, 0, pumpSliceMS, qsAllInput, mwmoInputAvailable)
}

// finish re-posts a quit that arrived while we were waiting.
func (p *pump) finish() {
	if p.quitSeen {
		_, _, _ = procPostQuitMessage.Call(p.quitCode)
	}
}

// waitFor pumps the message queue until the handler reports, or the deadline
// passes.
func waitFor[T any](done <-chan T, timeout time.Duration, what string) (T, error) {
	var zero T
	var messages pump
	defer messages.finish()

	deadline := time.Now().Add(timeout)
	for {
		select {
		case value := <-done:
			return value, nil
		default:
		}
		if time.Now().After(deadline) {
			// One last look: the handler may have fired inside the final step.
			select {
			case value := <-done:
				return value, nil
			default:
			}
			return zero, fmt.Errorf("webview2: gave up after %s waiting for %s", timeout, what)
		}
		messages.step()
	}
}
