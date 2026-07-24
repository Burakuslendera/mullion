//go:build windows

package webview2

// One constructor per WebView2 event: the IID each handler answers
// QueryInterface with, the Go signature the caller sees, and the ownership
// rules governing the returned pointer. Split from handlers_windows.go, which
// keeps the shared vtable, the dispatch path and the panic containment every
// constructor here stands on.
//
// Binding a new event means adding an IID and a constructor here; nothing in
// handlers_windows.go changes.

import (
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
	// IIDICoreWebView2NavigationStartingEventHandler = {9adbe429-f36d-432b-9ddc-f8881fbd76e3}
	IIDICoreWebView2NavigationStartingEventHandler = windows.GUID{
		Data1: 0x9adbe429, Data2: 0xf36d, Data3: 0x432b,
		Data4: [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3},
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
	// IIDICoreWebView2NewWindowRequestedEventHandler = {d4c185fe-c81c-4989-97af-2d3fa7ab5651}
	IIDICoreWebView2NewWindowRequestedEventHandler = windows.GUID{
		Data1: 0xd4c185fe, Data2: 0xc81c, Data3: 0x4989,
		Data4: [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51},
	}
)

// --- constructors -----------------------------------------------------------
//
// Ownership, once for all six:
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

// NewNavigationStartingHandler wraps fn as an
// ICoreWebView2NavigationStartingEventHandler.
func NewNavigationStartingHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2NavigationStartingEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2NavigationStartingEventHandler,
		"NavigationStarting",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2NavigationStartingEventArgs](args),
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

// NewNewWindowRequestedHandler wraps fn as an
// ICoreWebView2NewWindowRequestedEventHandler.
func NewNewWindowRequestedHandler(fn func(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs)) unsafe.Pointer {
	return newEventHandler(
		IIDICoreWebView2NewWindowRequestedEventHandler,
		"NewWindowRequested",
		func(sender, args uintptr) {
			fn(
				interfaceFromAddress[ICoreWebView2](sender),
				interfaceFromAddress[ICoreWebView2NewWindowRequestedEventArgs](args),
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
