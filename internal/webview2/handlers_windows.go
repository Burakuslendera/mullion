//go:build windows

package webview2

// Event handlers: the six COM objects WebView2 calls back into.
//
// Everything in the interfaces_* files is outbound - Go calling the runtime.
// This file is the other direction. add_WebMessageReceived and friends take a
// COM object that *we* implement, and the runtime invokes it later, on its own
// schedule, from its own stack. That inversion is where the sharp edges are:
//
//   - A Go panic that escapes into the runtime's stack kills the process. There
//     is no recovering across the C boundary, so every Invoke recovers on its
//     own. See eventHandler.dispatch.
//   - The object must stay alive for as long as the runtime holds it, and must
//     NOT stay alive longer. comServer's reference count decides that; see the
//     ownership note on ReleaseHandler, in handlers_events_windows.go.
//   - The Go pointers the runtime hands us (sender, args) are borrowed for the
//     duration of the call only.
//
// # Why one vtable for six interfaces
//
// All six interfaces have the same COM shape: IUnknown plus a single
// Invoke(this, sender, args) slot, and the two arguments are interface pointers
// in every case. They differ only in their IID and in the Go type the caller
// wants to see. comServer already stores the IID per instance and answers
// QueryInterface from it, so the vtable - which holds nothing but function
// pointers - can be shared.
//
// windows.NewCallback allocates from a small, fixed, never-freed table. One
// shared vtable means exactly one Invoke callback for all event handling, and
// ensureCOMVtables builds it only after the supported-architecture gate.
//
// # Threading
//
// Events arrive on the UI thread, inside the host's message loop - the same
// thread that created the WebView and that the host has already locked with
// runtime.LockOSThread. So Invoke must not lock or unlock the OS thread itself:
// the thread is already the right one, and touching the lock here would only
// risk unpinning it out from under the message pump.

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// eventHandlerVtbl is the vtable shape all six event handler interfaces share.
// ABI-critical: Invoke must sit at slot 3, immediately after IUnknown.
type eventHandlerVtbl struct {
	IUnknownVtbl
	Invoke ComProc
}

// eventHandler is one Go-implemented event handler COM object.
type eventHandler struct {
	// server must stay first: a COM interface pointer is the address of the
	// word holding the vtable, and comServer's first word is that vtable.
	server comServer
	event  string // for panic reports; never shown to the page
	invoke func(sender, args uintptr)
}

// newEventHandler builds the COM object and publishes it with a reference count
// of one, which the caller owns. See ReleaseHandler (handlers_events_windows.go)
// for what to do with it.
func newEventHandler(iid windows.GUID, event string, invoke func(sender, args uintptr)) unsafe.Pointer {
	if !ensureCOMVtables() {
		return nil
	}
	handler := &eventHandler{event: event, invoke: invoke}
	handler.server.register(
		uintptr(unsafe.Pointer(&eventHandlerVtable)),
		iid,
		handler,
	)
	// comServer is the first field, so the handler's address IS the interface
	// pointer. This stays a real Go pointer the whole way, so the GC keeps
	// tracking it (the servers map is what keeps it reachable once the runtime
	// holds the address).
	return unsafe.Pointer(handler)
}

// eventHandlerInvoke is the one callback behind every handler's Invoke slot.
//
// It never lets a Go panic reach the caller: the caller is Chromium, and an
// unrecovered panic crossing that boundary takes the process with it.
func eventHandlerInvoke(this, sender, args uintptr) uintptr {
	server := serverFor(this)
	if server == nil {
		// `this` is not one of ours. Nothing was handled and nothing can be, so
		// there is no event outcome to protect - report the truth. This is the
		// one case that does not return S_OK, and it is unreachable short of
		// memory corruption or a foreign caller.
		return eFail
	}
	handler, ok := server.self.(*eventHandler)
	if !ok {
		return eFail
	}
	handler.dispatch(sender, args)

	// Always S_OK once we have a handler, even if the callback panicked.
	//
	// A failing HRESULT out of an event handler is not a no-op: for
	// WebResourceRequested the runtime treats it as "the handler did not produce
	// a response", which cancels the request and blanks the asset - so a bug in
	// one Go callback would turn into a dead window. The panic has already been
	// reported through the hook; the frame should keep running. S_OK means "the
	// event was delivered", which is true regardless of what the callback did
	// with it.
	return sOK
}

// dispatch runs the user callback with panics contained.
//
// The recover has to live in a function on the callback's own stack: recover()
// only stops a panic unwinding through the frame that deferred it. Putting it
// here rather than in eventHandlerInvoke keeps the recovered region as tight as
// the user code itself.
func (h *eventHandler) dispatch(sender, args uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			reportHandlerPanic(h.event, recovered, debug.Stack())
		}
	}()
	if h.invoke == nil {
		return
	}
	h.invoke(sender, args)
}

// --- panic reporting --------------------------------------------------------

var (
	panicHookMu sync.RWMutex
	panicHook   func(event string, recovered any, stack []byte)
)

// SetHandlerPanicHook installs the reporter for panics recovered inside an
// event handler.
//
// Swallowing a panic silently would be worse than crashing: the window keeps
// running with a callback that never completed, and nothing says so. The host
// should route this into its logger. Set it before creating any handler; a nil
// hook falls back to a one-line note on stderr, because a lost panic is a bug
// that has to be visible somewhere.
func SetHandlerPanicHook(hook func(event string, recovered any, stack []byte)) {
	panicHookMu.Lock()
	panicHook = hook
	panicHookMu.Unlock()
}

// HandlerPanicHook returns the reporter currently installed, or nil if there is
// none and recovered panics are still going to stderr.
//
// It exists for the host's wiring test. "A recovered handler panic reaches
// Config.Logger" is only true if the hook this package will actually call routes
// there, and the installed hook is otherwise unobservable from outside the
// package - a test that called the host's own reporter directly would keep
// passing with the SetHandlerPanicHook call deleted, which is exactly the state
// this hook spent its whole life in.
func HandlerPanicHook() func(event string, recovered any, stack []byte) {
	panicHookMu.RLock()
	defer panicHookMu.RUnlock()
	return panicHook
}

func reportHandlerPanic(event string, recovered any, stack []byte) {
	panicHookMu.RLock()
	hook := panicHook
	panicHookMu.RUnlock()

	if hook == nil {
		fmt.Fprintf(os.Stderr, "webview2: panic in %s handler: %v\n%s\n", event, recovered, stack)
		return
	}
	// A panicking hook must not re-enter the panic path and take the process
	// down along the way it was installed to prevent.
	defer func() { _ = recover() }()
	hook(event, recovered, stack)
}

// --- pointer laundering -----------------------------------------------------

// interfaceFromAddress reinterprets an interface pointer that COM passed to us
// as an integer into a typed Go pointer.
//
// The bit pattern is copied rather than cast for the reason spelled out on
// procRtlMoveMemory in com_memory_windows.go: `go vet`'s unsafeptr check rejects
// turning an untracked uintptr into an unsafe.Pointer, and it is right to. The
// address points into the runtime's memory, never the Go heap, so the GC scans
// the resulting word, finds it outside every heap span, and leaves it alone.
func interfaceFromAddress[T any](addr uintptr) *T {
	if addr == 0 {
		return nil
	}
	var out *T
	source := addr
	_, _, _ = procRtlMoveMemory.Call(
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&source)),
		unsafe.Sizeof(source),
	)
	return out
}
