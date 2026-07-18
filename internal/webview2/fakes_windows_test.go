//go:build windows

package webview2

// Fake COM objects for headless reference-lifetime tests.
//
// A COM object is one machine word pointing at a vtable, so a test can stand a
// real one up out of Go memory: the vtable slots point at windows.NewCallback
// trampolines that count calls. That lets a test prove "this code released the
// reference it owned" without a WebView2 runtime, the same way
// handlers_windows_test.go plays the runtime's part when it calls Invoke.
//
// Every trampoline is created once, at package init, because NewCallback
// allocates from a small fixed table that is never freed (see com_windows.go).
// Per-object state lives in a registry keyed by the object's address, so one
// set of trampolines serves every fake object in the suite. The callbacks run
// synchronously on the calling goroutine - a vtable call is just an indirect
// function call - so the counters need no atomics.

import (
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fakeComState counts the calls a fake COM object receives.
type fakeComState struct {
	releases   int
	addRefs    int
	getRequest int
	request    uintptr // what a fake event args writes out from GetRequest; 0 fails the call
}

var (
	fakeComMu     sync.Mutex
	fakeComStates = map[uintptr]*fakeComState{}
)

func fakeComStateFor(this uintptr) *fakeComState {
	fakeComMu.Lock()
	defer fakeComMu.Unlock()
	return fakeComStates[this]
}

// registerFakeCom publishes state for the object at this and returns the
// cleanup that withdraws it, for t.Cleanup.
func registerFakeCom(this uintptr, state *fakeComState) func() {
	fakeComMu.Lock()
	fakeComStates[this] = state
	fakeComMu.Unlock()
	return func() {
		fakeComMu.Lock()
		delete(fakeComStates, this)
		fakeComMu.Unlock()
	}
}

// fakeComIUnknownVtbl is the IUnknown prefix shared by every fake object.
// AddRef and Release count into the object's registered state; QueryInterface
// refuses everything, because no lifetime test should depend on it.
var fakeComIUnknownVtbl = IUnknownVtbl{
	QueryInterface: ComProc(windows.NewCallback(func(this, riid, ppv uintptr) uintptr {
		writeAddress(ppv, 0)
		return eNoInterface
	})),
	AddRef: ComProc(windows.NewCallback(func(this uintptr) uintptr {
		if state := fakeComStateFor(this); state != nil {
			state.addRefs++
		}
		return 2
	})),
	Release: ComProc(windows.NewCallback(func(this uintptr) uintptr {
		if state := fakeComStateFor(this); state != nil {
			state.releases++
		}
		return 1
	})),
}

// fakeWebResourceRequestVtbl backs a fake ICoreWebView2WebResourceRequest. Only
// the IUnknown slots are populated: lifetime tests touch nothing else, and a
// zero slot crashes loudly if one ever does.
var fakeWebResourceRequestVtbl = ICoreWebView2WebResourceRequestVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
}

// fakeWebResourceArgsVtbl backs a fake ICoreWebView2WebResourceRequestedEventArgs
// whose GetRequest hands out the request registered for the object - taking the
// reference the real runtime would - and counts the call. With no request
// registered it fails the way a runtime failure would, writing a null
// out-pointer.
var fakeWebResourceArgsVtbl = ICoreWebView2WebResourceRequestedEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetRequest: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		if state == nil || state.request == 0 {
			writeAddress(out, 0)
			return eFail
		}
		state.getRequest++
		writeAddress(out, state.request)
		return sOK
	})),
}

// newFakeUnknown returns a bare fake IUnknown and its call counters, for
// lifetime tests that need nothing but AddRef/Release accounting.
func newFakeUnknown(t *testing.T) (*IUnknown, *fakeComState) {
	t.Helper()
	object := &IUnknown{Vtbl: &fakeComIUnknownVtbl}
	state := &fakeComState{}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(object)), state))
	return object, state
}

// newFakeWebResourceRequest returns a fake request and its call counters. The
// object stays registered until the test ends.
func newFakeWebResourceRequest(t *testing.T) (*ICoreWebView2WebResourceRequest, *fakeComState) {
	t.Helper()
	request := &ICoreWebView2WebResourceRequest{Vtbl: &fakeWebResourceRequestVtbl}
	state := &fakeComState{}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(request)), state))
	return request, state
}

// newFakeWebResourceArgs returns fake event args whose GetRequest yields
// request. Pass nil to make GetRequest fail.
func newFakeWebResourceArgs(t *testing.T, request *ICoreWebView2WebResourceRequest) (*ICoreWebView2WebResourceRequestedEventArgs, *fakeComState) {
	t.Helper()
	args := &ICoreWebView2WebResourceRequestedEventArgs{Vtbl: &fakeWebResourceArgsVtbl}
	state := &fakeComState{request: uintptr(unsafe.Pointer(request))}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args, state
}
