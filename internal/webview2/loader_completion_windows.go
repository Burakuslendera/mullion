//go:build windows

package webview2

// Completion handlers, implemented in Go: the COM objects the runtime calls
// back into when environment or controller creation finishes. Split from
// loader_windows.go, which keeps the creation entry points.

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

// The IIDs below are the two callback interfaces we implement. They are
// taken from the WebView2 SDK's WebView2.h (MIDL_INTERFACE declarations),
// which is the authoritative source: an interface ID is an identity, and a
// wrong one means the runtime silently refuses to call us back.
var (
	// ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	iidEnvironmentCompletedHandler = windows.GUID{
		Data1: 0x4e8a3389, Data2: 0xc9d8, Data3: 0x4bd2,
		Data4: [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d},
	}
	// ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	iidControllerCompletedHandler = windows.GUID{
		Data1: 0x6c4819f3, Data2: 0xc9b7, Data3: 0x4260,
		Data4: [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c},
	}
)

type completionVtbl struct {
	IUnknownVtbl
	Invoke ComProc
}

// completion carries what an Invoke received. The environment and controller
// handlers are the same shape, so they share one type.
type completion struct {
	hr     uintptr
	result *IUnknown
}

type completedHandler struct {
	server comServer // must stay first
	this   uintptr
	done   chan completion

	// mu orders invoked against abandon. Both run on the STA thread in practice,
	// but the lock makes the no-strand guarantee hold by construction rather
	// than by apartment rules: a completion is either sent while a waiter still
	// exists, or released.
	mu        sync.Mutex
	abandoned bool
}

var (
	environmentCompletedVtable = completionVtbl{
		IUnknownVtbl: iunknownVtbl,
		Invoke:       ComProc(windows.NewCallback(environmentCompletedInvoke)),
	}
	controllerCompletedVtable = completionVtbl{
		IUnknownVtbl: iunknownVtbl,
		Invoke:       ComProc(windows.NewCallback(controllerCompletedInvoke)),
	}
)

func newCompletedHandler(vtable uintptr, iid windows.GUID) *completedHandler {
	// Buffered, so Invoke never blocks. Invoke runs on the UI thread inside our
	// own message pump: a send that blocked would deadlock the thread that is
	// supposed to receive it.
	handler := &completedHandler{done: make(chan completion, 1)}
	handler.this = handler.server.register(vtable, iid, handler)
	return handler
}

func (h *completedHandler) release() {
	serverRelease(h.this)
}

// abandon records that the waiter gave up, and reclaims a completion that
// already reached the buffer.
//
// After a timeout nobody will ever drain done, so a completion left there would
// hold the AddRef the handler took and the GC would eventually free the channel
// without a Release - stranding the freshly created controller or environment
// and its browser processes (#37). Raising the flag makes a later invoked
// release instead of send; the drain below catches the other ordering, where
// the completion landed before the flag went up.
func (h *completedHandler) abandon() {
	h.mu.Lock()
	h.abandoned = true
	h.mu.Unlock()
	select {
	case late := <-h.done:
		if late.result != nil {
			late.result.Release()
		}
	default:
	}
}

// invoked is the body shared by both Invoke callbacks.
func invoked(this, errorCode, result uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		return eFail
	}
	handler, ok := server.self.(*completedHandler)
	if !ok {
		return eFail
	}
	object := unknownFromAddress(result)
	if object != nil {
		// The reference handed to a completion handler is borrowed: the runtime
		// releases it as soon as Invoke returns. Keeping it without an AddRef
		// leaves a pointer to a freed object.
		object.AddRef()
	}
	delivered := false
	handler.mu.Lock()
	if !handler.abandoned {
		select {
		case handler.done <- completion{hr: errorCode, result: object}:
			delivered = true
		default:
			// Invoke fired twice, which the interface forbids. Drop the extra
			// rather than leak the reference we just took.
		}
	}
	handler.mu.Unlock()
	if !delivered {
		// Either the waiter timed out and abandoned the handler - a late
		// completion sent into the buffer would never be drained, and the GC
		// would free it without a Release (#37) - or this was a forbidden
		// second fire. Both drop the reference taken above.
		object.Release()
	}
	return sOK
}

func environmentCompletedInvoke(this, errorCode, result uintptr) uintptr {
	return invoked(this, errorCode, result)
}

func controllerCompletedInvoke(this, errorCode, result uintptr) uintptr {
	return invoked(this, errorCode, result)
}

// completionResult unwraps what a completion handler delivered, deciding the
// fate of the reference invoked took. A failing HRESULT releases the result if
// one arrived anyway - the completion contract does not promise a null object
// on failure, and keeping it would leak; a nil result on success is an error;
// otherwise ownership passes to the caller.
func completionResult(result completion, what string) (*IUnknown, error) {
	if err := hres(result.hr); err != nil {
		if result.result != nil {
			result.result.Release()
		}
		return nil, fmt.Errorf("webview2: %s creation failed: %w", what, err)
	}
	if result.result == nil {
		return nil, fmt.Errorf("webview2: %s creation reported success but returned nothing", what)
	}
	return result.result, nil
}
