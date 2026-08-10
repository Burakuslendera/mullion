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
// Test-fake trampolines are created once at test-package init because they are
// isolated fixture plumbing, not a production entry. Production differs
// deliberately: comserver_windows.go initializes shared callbacks lazily after
// architecture validation. Per-object state here still
// lives in a registry keyed by the object's address, so one set of trampolines
// serves every fake object in the suite. The callbacks run
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
	releases                int
	addRefs                 int
	getRequest              int
	request                 uintptr // what a fake event args writes out from GetRequest; 0 fails the call
	queryTarget             uintptr // what a fake controller's QueryInterface hands out; 0 answers E_NOINTERFACE
	puts                    int     // Put* setter calls received by a fake controller3
	putResult               uintptr // HRESULT a fake controller3 returns from its Put* setters
	message                 string
	source                  string
	uri                     string
	messageResult           uintptr
	sourceResult            uintptr
	uriResult               uintptr
	navigationID            uint64
	navigationIDResult      uintptr
	userInitiated           bool
	userInitiatedResult     uintptr
	success                 bool
	successResult           uintptr
	redirected              bool
	redirectedResult        uintptr
	status                  WebErrorStatus
	statusResult            uintptr
	kind                    ProcessFailedKind
	kindResult              uintptr
	operationResult         uintptr
	operationCalls          int
	registeredEventHandlers [61]uintptr
	registeredEventAdds     [61]int
	registeredEventRefs     []uintptr
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

func fakeStringResult(out uintptr, value string, result uintptr) uintptr {
	if result != sOK {
		writeAddress(out, 0)
		return result
	}
	address, err := coTaskMemString(value)
	if err != nil {
		writeAddress(out, 0)
		return eFail
	}
	writeAddress(out, address)
	return sOK
}

func fakeWriteUint64(out uintptr, value uint64) {
	if out != 0 {
		_, _, _ = procRtlMoveMemory.Call(out, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	}
}

func fakeWriteInt32(out uintptr, value int32) {
	if out != 0 {
		_, _, _ = procRtlMoveMemory.Call(out, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	}
}

var fakeWebMessageArgsVtbl = ICoreWebView2WebMessageReceivedEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetSource: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		return fakeStringResult(out, state.source, state.sourceResult)
	})),
	TryGetWebMessageAsString: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		return fakeStringResult(out, state.message, state.messageResult)
	})),
}

var fakeNavigationStartingArgsVtbl = ICoreWebView2NavigationStartingEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetUri: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		return fakeStringResult(out, state.uri, state.uriResult)
	})),
	GetIsUserInitiated: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		writeBOOL(out, state.userInitiated)
		return state.userInitiatedResult
	})),
	GetIsRedirected: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		writeBOOL(out, state.redirected)
		return state.redirectedResult
	})),
	PutCancel: ComProc(windows.NewCallback(func(this, value uintptr) uintptr {
		state := fakeComStateFor(this)
		state.puts++
		return state.putResult
	})),
	GetNavigationID: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		fakeWriteUint64(out, state.navigationID)
		return state.navigationIDResult
	})),
}

var fakeNavigationCompletedArgsVtbl = ICoreWebView2NavigationCompletedEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetIsSuccess: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		writeBOOL(out, state.success)
		return state.successResult
	})),
	GetWebErrorStatus: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		fakeWriteInt32(out, int32(state.status))
		return state.statusResult
	})),
	GetNavigationID: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		fakeWriteUint64(out, state.navigationID)
		return state.navigationIDResult
	})),
}

var fakeNewWindowArgsVtbl = ICoreWebView2NewWindowRequestedEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetUri: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		return fakeStringResult(out, state.uri, state.uriResult)
	})),
	PutHandled: ComProc(windows.NewCallback(func(this, value uintptr) uintptr {
		state := fakeComStateFor(this)
		state.puts++
		return state.putResult
	})),
	GetIsUserInitiated: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		writeBOOL(out, state.userInitiated)
		return state.userInitiatedResult
	})),
}

var fakeProcessFailedArgsVtbl = ICoreWebView2ProcessFailedEventArgsVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	GetProcessFailedKind: ComProc(windows.NewCallback(func(this, out uintptr) uintptr {
		state := fakeComStateFor(this)
		fakeWriteInt32(out, int32(state.kind))
		return state.kindResult
	})),
}

const (
	fakeAddNavigationStartingSlot   = 7
	fakeAddNavigationCompletedSlot  = 15
	fakeAddProcessFailedSlot        = 25
	fakeAddWebMessageReceivedSlot   = 34
	fakeAddNewWindowRequestedSlot   = 44
	fakeAddWebResourceRequestedSlot = 55
)

func fakeCaptureRegisteredEvent(this, handler uintptr, slot int) uintptr {
	state := fakeComStateFor(this)
	if state == nil || handler == 0 {
		return eFail
	}
	state.registeredEventAdds[slot]++
	state.registeredEventHandlers[slot] = handler
	state.registeredEventRefs = append(state.registeredEventRefs, handler)
	serverAddRef(handler)
	return sOK
}

var (
	fakeAddNavigationStarting = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddNavigationStartingSlot)
	}))
	fakeAddNavigationCompleted = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddNavigationCompletedSlot)
	}))
	fakeAddProcessFailed = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddProcessFailedSlot)
	}))
	fakeAddWebMessageReceived = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddWebMessageReceivedSlot)
	}))
	fakeAddNewWindowRequested = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddNewWindowRequestedSlot)
	}))
	fakeAddWebResourceRequested = ComProc(windows.NewCallback(func(this, handler, token uintptr) uintptr {
		return fakeCaptureRegisteredEvent(this, handler, fakeAddWebResourceRequestedSlot)
	}))
)

func newFakeRegisterEventsCore(t *testing.T) (*ICoreWebView2, *fakeComState) {
	t.Helper()
	vtbl := &ICoreWebView2Vtbl{}
	slots := unsafe.Slice((*ComProc)(unsafe.Pointer(vtbl)), 61)
	slots[fakeAddNavigationStartingSlot] = fakeAddNavigationStarting
	slots[fakeAddNavigationCompletedSlot] = fakeAddNavigationCompleted
	slots[fakeAddProcessFailedSlot] = fakeAddProcessFailed
	slots[fakeAddWebMessageReceivedSlot] = fakeAddWebMessageReceived
	slots[fakeAddNewWindowRequestedSlot] = fakeAddNewWindowRequested
	slots[fakeAddWebResourceRequestedSlot] = fakeAddWebResourceRequested

	core := &ICoreWebView2{Vtbl: vtbl}
	state := &fakeComState{}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(core)), state))
	return core, state
}

var fakeSurfaceCoreVtbl = ICoreWebView2Vtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	Navigate: ComProc(windows.NewCallback(func(this, uri uintptr) uintptr {
		state := fakeComStateFor(this)
		state.operationCalls++
		return state.operationResult
	})),
	AddScriptToExecuteOnDocumentCreated: ComProc(windows.NewCallback(func(this, script, handler uintptr) uintptr {
		state := fakeComStateFor(this)
		state.operationCalls++
		return state.operationResult
	})),
	ExecuteScript: ComProc(windows.NewCallback(func(this, script, handler uintptr) uintptr {
		state := fakeComStateFor(this)
		state.operationCalls++
		return state.operationResult
	})),
	PostWebMessageAsString: ComProc(windows.NewCallback(func(this, message uintptr) uintptr {
		state := fakeComStateFor(this)
		state.operationCalls++
		return state.operationResult
	})),
}

var fakeSurfaceControllerVtbl = ICoreWebView2ControllerVtbl{
	IUnknownVtbl: fakeComIUnknownVtbl,
	PutIsVisible: ComProc(windows.NewCallback(func(this, visible uintptr) uintptr {
		state := fakeComStateFor(this)
		state.operationCalls++
		return state.operationResult
	})),
}

// fakeControllerVtbl backs a fake ICoreWebView2Controller whose QueryInterface
// answers from the object's registered queryTarget: a real controller3 address
// on a runtime that has the interface, or E_NOINTERFACE - the clean "no" an old
// runtime gives (query_windows.go) - when none is registered. Handing out the
// target AddRefs it, as real QueryInterface does, so release-balance assertions
// hold. Only the IUnknown slots are populated; a zero slot crashes loudly if a
// test ever reaches one.
var fakeControllerVtbl = ICoreWebView2ControllerVtbl{
	IUnknownVtbl: IUnknownVtbl{
		QueryInterface: ComProc(windows.NewCallback(func(this, riid, ppv uintptr) uintptr {
			state := fakeComStateFor(this)
			if state == nil || state.queryTarget == 0 {
				writeAddress(ppv, 0)
				return eNoInterface
			}
			if target := fakeComStateFor(state.queryTarget); target != nil {
				target.addRefs++
			}
			writeAddress(ppv, state.queryTarget)
			return sOK
		})),
		AddRef:  fakeComIUnknownVtbl.AddRef,
		Release: fakeComIUnknownVtbl.Release,
	},
}

// fakeController3Vtbl backs a fake ICoreWebView2Controller3 whose two policy
// setters count into puts and return the object's registered putResult, so a
// test can play both a healthy runtime (sOK) and one whose setters fail (eFail).
var fakeController3Vtbl = ICoreWebView2Controller3Vtbl{
	ICoreWebView2Controller2Vtbl: ICoreWebView2Controller2Vtbl{
		ICoreWebView2ControllerVtbl: ICoreWebView2ControllerVtbl{
			IUnknownVtbl: fakeComIUnknownVtbl,
		},
	},
	PutShouldDetectMonitorScaleChanges: fakeController3Put,
	PutBoundsMode:                      fakeController3Put,
}

var fakeController3Put = ComProc(windows.NewCallback(func(this, value uintptr) uintptr {
	state := fakeComStateFor(this)
	if state == nil {
		return eFail
	}
	state.puts++
	return state.putResult
}))

// newFakeController returns a fake controller whose QueryInterface hands out
// target, or answers E_NOINTERFACE when target is nil - the shape of an older
// runtime that does not implement ICoreWebView2Controller3.
func newFakeController(t *testing.T, target *ICoreWebView2Controller3) (*ICoreWebView2Controller, *fakeComState) {
	t.Helper()
	controller := &ICoreWebView2Controller{Vtbl: &fakeControllerVtbl}
	state := &fakeComState{queryTarget: uintptr(unsafe.Pointer(target))}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(controller)), state))
	return controller, state
}

// newFakeController3 returns a fake controller3 whose Put* setters return
// putResult, and its call counters.
func newFakeController3(t *testing.T, putResult uintptr) (*ICoreWebView2Controller3, *fakeComState) {
	t.Helper()
	controller3 := &ICoreWebView2Controller3{Vtbl: &fakeController3Vtbl}
	state := &fakeComState{putResult: putResult}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(controller3)), state))
	return controller3, state
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

func newFakeWebMessageArgs(t *testing.T, state *fakeComState) *ICoreWebView2WebMessageReceivedEventArgs {
	t.Helper()
	args := &ICoreWebView2WebMessageReceivedEventArgs{Vtbl: &fakeWebMessageArgsVtbl}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args
}

func newFakeNavigationStartingArgs(t *testing.T, state *fakeComState) *ICoreWebView2NavigationStartingEventArgs {
	t.Helper()
	args := &ICoreWebView2NavigationStartingEventArgs{Vtbl: &fakeNavigationStartingArgsVtbl}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args
}

func newFakeNavigationCompletedArgs(t *testing.T, state *fakeComState) *ICoreWebView2NavigationCompletedEventArgs {
	t.Helper()
	args := &ICoreWebView2NavigationCompletedEventArgs{Vtbl: &fakeNavigationCompletedArgsVtbl}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args
}

func newFakeNewWindowArgs(t *testing.T, state *fakeComState) *ICoreWebView2NewWindowRequestedEventArgs {
	t.Helper()
	args := &ICoreWebView2NewWindowRequestedEventArgs{Vtbl: &fakeNewWindowArgsVtbl}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args
}

func newFakeProcessFailedArgs(t *testing.T, state *fakeComState) *ICoreWebView2ProcessFailedEventArgs {
	t.Helper()
	args := &ICoreWebView2ProcessFailedEventArgs{Vtbl: &fakeProcessFailedArgsVtbl}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(args)), state))
	return args
}

func newFakeSurfaceCore(t *testing.T, result uintptr) (*ICoreWebView2, *fakeComState) {
	t.Helper()
	core := &ICoreWebView2{Vtbl: &fakeSurfaceCoreVtbl}
	state := &fakeComState{operationResult: result}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(core)), state))
	return core, state
}

func newFakeSurfaceController(t *testing.T, result uintptr) (*ICoreWebView2Controller, *fakeComState) {
	t.Helper()
	controller := &ICoreWebView2Controller{Vtbl: &fakeSurfaceControllerVtbl}
	state := &fakeComState{operationResult: result}
	t.Cleanup(registerFakeCom(uintptr(unsafe.Pointer(controller)), state))
	return controller, state
}
