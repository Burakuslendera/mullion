//go:build windows

package webview2

// Event handlers: the four COM objects WebView2 calls back into.
//
// Everything in interfaces_windows.go is outbound - Go calling the runtime.
// This file is the other direction. add_WebMessageReceived and friends take a
// COM object that *we* implement, and the runtime invokes it later, on its own
// schedule, from its own stack. That inversion is where the sharp edges are:
//
//   - A Go panic that escapes into the runtime's stack kills the process. There
//     is no recovering across the C boundary, so every Invoke recovers on its
//     own. See eventHandler.dispatch.
//   - The object must stay alive for as long as the runtime holds it, and must
//     NOT stay alive longer. comServer's reference count decides that; see the
//     ownership note on ReleaseHandler.
//   - The Go pointers the runtime hands us (sender, args) are borrowed for the
//     duration of the call only.
//
// # Why one vtable for four interfaces
//
// All four interfaces have the same COM shape: IUnknown plus a single
// Invoke(this, sender, args) slot, and the two arguments are interface pointers
// in every case. They differ only in their IID and in the Go type the caller
// wants to see. comServer already stores the IID per instance and answers
// QueryInterface from it, so the vtable - which holds nothing but function
// pointers - can be shared.
//
// This is not just tidiness. windows.NewCallback allocates from a small, fixed,
// never-freed table; a vtable per interface would burn four entries, and a
// callback per handler *instance* would exhaust the table outright. One shared
// vtable means exactly one NewCallback for all event handling, created once at
// package init.
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

// Handler interface IDs, transcribed from the MIDL_INTERFACE attributes in the
// WebView2 SDK header (build/native/include/WebView2.h). Each of these
// interfaces is IUnknown + Invoke: 4 slots, Invoke at index 3.
var (
	// IIDICoreWebView2WebMessageReceivedEventHandler = {57213f19-00e6-49fa-8e07-898ea01ecbd2}
	IIDICoreWebView2WebMessageReceivedEventHandler = windows.GUID{
		Data1: 0x57213f19, Data2: 0x00e6, Data3: 0x49fa,
		Data4: [8]byte{0x8e, 0x07, 0x89, 0x8e, 0xa0, 0x1e, 0xcb, 0xd2},
	}
	// IIDICoreWebView2WebResourceRequestedEventHandler = {ab00b74c-15f1-4646-80e8-e76341d25d71}
	IIDICoreWebView2WebResourceRequestedEventHandler = windows.GUID{
		Data1: 0xab00b74c, Data2: 0x15f1, Data3: 0x4646,
		Data4: [8]byte{0x80, 0xe8, 0xe7, 0x63, 0x41, 0xd2, 0x5d, 0x71},
	}
	// IIDICoreWebView2NavigationCompletedEventHandler = {d33a35bf-1c49-4f98-93ab-006e0533fe1c}
	IIDICoreWebView2NavigationCompletedEventHandler = windows.GUID{
		Data1: 0xd33a35bf, Data2: 0x1c49, Data3: 0x4f98,
		Data4: [8]byte{0x93, 0xab, 0x00, 0x6e, 0x05, 0x33, 0xfe, 0x1c},
	}
	// IIDICoreWebView2ProcessFailedEventHandler = {79e0aea4-990b-42d9-aa1d-0fcc2e5bc7f1}
	IIDICoreWebView2ProcessFailedEventHandler = windows.GUID{
		Data1: 0x79e0aea4, Data2: 0x990b, Data3: 0x42d9,
		Data4: [8]byte{0xaa, 0x1d, 0x0f, 0xcc, 0x2e, 0x5b, 0xc7, 0xf1},
	}
)

// eventHandlerVtbl is the vtable shape all four event handler interfaces share.
// ABI-critical: Invoke must sit at slot 3, immediately after IUnknown.
type eventHandlerVtbl struct {
	IUnknownVtbl
	Invoke ComProc
}

// eventHandlerVtable is the single vtable instance behind every event handler.
// Package-level, so its address is stable for the life of the process - the
// runtime keeps this pointer and dereferences it on every event.
var eventHandlerVtable = eventHandlerVtbl{
	IUnknownVtbl: iunknownVtbl,
	Invoke:       ComProc(windows.NewCallback(eventHandlerInvoke)),
}

// eventHandler is one Go-implemented event handler COM object.
type eventHandler struct {
	// server must stay first: a COM interface pointer is the address of the
	// word holding the vtable, and comServer's first word is that vtable.
	server comServer
	this   uintptr
	event  string // for panic reports; never shown to the page
	invoke func(sender, args uintptr)
}

// newEventHandler builds the COM object and publishes it with a reference count
// of one, which the caller owns. See ReleaseHandler for what to do with it.
func newEventHandler(iid windows.GUID, event string, invoke func(sender, args uintptr)) unsafe.Pointer {
	handler := &eventHandler{event: event, invoke: invoke}
	handler.this = handler.server.register(
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
// procRtlMoveMemory in com_windows.go: `go vet`'s unsafeptr check rejects
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

// --- constructors -----------------------------------------------------------
//
// Ownership, once for all four:
//
// The returned pointer carries ONE reference, which the caller owns. Pass it to
// the matching add_* method and then hand it to ReleaseHandler:
//
//	handler := webview2.NewWebMessageReceivedHandler(onMessage)
//	token, err := view.AddWebMessageReceived(handler)
//	webview2.ReleaseHandler(handler)
//
// add_* takes its own reference, so the object survives the ReleaseHandler call
// and lives until the WebView drops it (on remove_* or when the WebView itself
// is destroyed). Skipping ReleaseHandler is not a crash, but the handler then
// outlives the WebView - a small, permanent leak per WebView created. Calling it
// twice IS a crash: the runtime would be left holding a freed object.
//
// The sender and args pointers a callback receives are borrowed for the duration
// of the call. Do not retain them; if you need the data, copy it out (the
// Get*/TryGet* wrappers already return Go strings).

// NewWebMessageReceivedHandler wraps fn as an
// ICoreWebView2WebMessageReceivedEventHandler.
func NewWebMessageReceivedHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2WebMessageReceivedEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2WebMessageReceivedEventHandler,
		"WebMessageReceived",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2WebMessageReceivedEventArgs](args),
			)
		},
	)
}

// NewWebResourceRequestedHandler wraps fn as an
// ICoreWebView2WebResourceRequestedEventHandler.
//
// This one is synchronous by contract: the response must be set on args before
// the callback returns, or the runtime proceeds without it. There is a deferral
// API for the async case, which this binding does not expose.
func NewWebResourceRequestedHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2WebResourceRequestedEventHandler,
		"WebResourceRequested",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2WebResourceRequestedEventArgs](args),
			)
		},
	)
}

// NewNavigationCompletedHandler wraps fn as an
// ICoreWebView2NavigationCompletedEventHandler.
func NewNavigationCompletedHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2NavigationCompletedEventHandler,
		"NavigationCompleted",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2NavigationCompletedEventArgs](args),
			)
		},
	)
}

// NewProcessFailedHandler wraps fn as an ICoreWebView2ProcessFailedEventHandler.
func NewProcessFailedHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2ProcessFailedEventHandler,
		"ProcessFailed",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2ProcessFailedEventArgs](args),
			)
		},
	)
}

// ReleaseHandler drops the reference that a New*Handler constructor returned.
//
// Call it exactly once, after the handler has been registered with its add_*
// method (or immediately, if registration failed). See the ownership note above.
func ReleaseHandler(handler unsafe.Pointer) {
	if handler == nil {
		return
	}
	serverRelease(uintptr(handler))
}
