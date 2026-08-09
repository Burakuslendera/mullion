//go:build windows

package webview2

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Lifetime tests for the loader's completion handlers. The first is about the
// handler's own reference count; the rest are about the result it carries:
// invoked() AddRefs the borrowed result before parking it in the handler's
// one-slot buffer for waitFor to consume, and each test asks who drops that
// reference when the normal hand-off breaks. The completions are driven directly
// - invoked is exactly what the runtime's Invoke trampoline calls - against fake
// IUnknowns, so no WebView2 runtime and no window are involved.

func TestCompletionVtblLayout(t *testing.T) {
	var v completionVtbl
	checkVtbl(t, "completionVtbl", unsafe.Sizeof(v), 4, []slot{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"Invoke", unsafe.Offsetof(v.Invoke), 3},
	})
}

func TestCompletedHandlerObjectStartsWithItsVtable(t *testing.T) {
	var object completedHandler
	var server comServer
	if got := unsafe.Offsetof(object.server); got != 0 {
		t.Fatalf("completedHandler.server offset = %d, want 0", got)
	}
	if got := unsafe.Offsetof(server.vtbl); got != 0 {
		t.Fatalf("comServer.vtbl offset = %d, want 0", got)
	}
}

func TestCompletionConstructorsRegisterSemanticIID(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func() *completedHandler
		iid  windows.GUID
	}{
		{"environment", newEnvironmentCompletedHandler, iidEnvironmentCompletedHandler},
		{"controller", newControllerCompletedHandler, iidControllerCompletedHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := tc.new()
			defer handler.release()
			server := serverFor(handler.this)
			if server == nil {
				t.Fatal("completion handler was not registered")
			}
			if server.vtbl != uintptr(unsafe.Pointer(&completedVtable)) {
				t.Errorf("vtable = %#x, want shared completion vtable %#x", server.vtbl, uintptr(unsafe.Pointer(&completedVtable)))
			}
			if server.iid != tc.iid {
				t.Errorf("IID = %+v, want %+v", server.iid, tc.iid)
			}
		})
	}
}

func TestCompletionQueryInterfaceArgumentsAndRefcount(t *testing.T) {
	handler := newEnvironmentCompletedHandler()
	defer handler.release()
	server := serverFor(handler.this)
	if server == nil {
		t.Fatal("completion handler was not registered")
	}
	unknown := (*IUnknown)(unsafe.Pointer(handler))

	for _, iid := range []windows.GUID{IIDIUnknown, iidEnvironmentCompletedHandler} {
		pointer, err := unknown.QueryInterface(&iid)
		if err != nil {
			t.Fatalf("QueryInterface(%s): %v", iid.String(), err)
		}
		if uintptr(pointer) != handler.this {
			t.Errorf("QueryInterface(%s) returned %#x, want %#x", iid.String(), uintptr(pointer), handler.this)
		}
		unknown.Release()
	}
	unsupported := windows.GUID{Data1: 0xdeadbeef}
	if pointer, err := unknown.QueryInterface(&unsupported); err == nil || pointer != nil {
		t.Fatalf("QueryInterface(unsupported) = %p, %v; want nil, error", pointer, err)
	}

	before := atomic.LoadInt32(&server.refs)
	var out uintptr = 1
	if hr := callCOMSlot(
		unsafe.Pointer(&completedVtable),
		0,
		handler.this,
		0,
		uintptr(unsafe.Pointer(&out)),
	); hr != ePointer {
		t.Errorf("QueryInterface(null IID) = %#x, want E_POINTER", hr)
	}
	if out != 0 {
		t.Errorf("QueryInterface(null IID) wrote %#x, want null", out)
	}
	if hr := callCOMSlot(
		unsafe.Pointer(&completedVtable),
		0,
		handler.this,
		uintptr(unsafe.Pointer(&IIDIUnknown)),
		0,
	); hr != ePointer {
		t.Errorf("QueryInterface(null out) = %#x, want E_POINTER", hr)
	}
	if got := atomic.LoadInt32(&server.refs); got != before {
		t.Errorf("refcount after invalid QueryInterface calls = %d, want %d", got, before)
	}
	if got := unknown.AddRef(); got != 2 {
		t.Fatalf("AddRef = %d, want 2", got)
	}
	if got := unknown.Release(); got != 1 {
		t.Fatalf("Release = %d, want 1", got)
	}
}

func TestCompletionInvokeDispatchesAtLiteralSlotThree(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	if hr := callCOMSlot(
		unsafe.Pointer(&completedVtable),
		3,
		handler.this,
		sOK,
		uintptr(unsafe.Pointer(object)),
	); hr != sOK {
		t.Fatalf("slot 3 (Invoke) = %#x, want S_OK", hr)
	}
	result := <-handler.done
	if result.result != object {
		t.Fatalf("slot 3 delivered %p, want %p", result.result, object)
	}
	if state.addRefs != 1 {
		t.Fatalf("slot 3 AddRefs = %d, want 1", state.addRefs)
	}
	result.result.Release()
	runtime.KeepAlive(object)
}

func TestCompletionInvokeHandlesNullArguments(t *testing.T) {
	handler := newTestCompletedHandler(t)
	if hr := callCOMSlot(unsafe.Pointer(&completedVtable), 3, handler.this, sOK, 0); hr != sOK {
		t.Fatalf("slot 3 (Invoke, null result) = %#x, want S_OK", hr)
	}
	result := <-handler.done
	if result.result != nil {
		t.Fatalf("slot 3 (Invoke, null result) delivered %p, want nil", result.result)
	}
	if _, err := completionResult(result, "controller"); err == nil {
		t.Fatal("null result reported with S_OK must be rejected")
	}
	if hr := callCOMSlot(unsafe.Pointer(&completedVtable), 3, 0, sOK, 0); hr != eFail {
		t.Fatalf("slot 3 (Invoke, null this) = %#x, want E_FAIL", hr)
	}
}

func newTestCompletedHandler(t *testing.T) *completedHandler {
	t.Helper()
	handler := newControllerCompletedHandler()
	t.Cleanup(handler.release)
	return handler
}

func TestCompletedHandlerIsReleasedNotLeaked(t *testing.T) {
	before := liveServerCount()
	handler := newEnvironmentCompletedHandler()
	if liveServerCount() != before+1 {
		t.Fatal("the handler was not registered")
	}

	// The runtime takes its own reference while the call is outstanding.
	unknown := (*IUnknown)(unsafe.Pointer(handler))
	if got := unknown.AddRef(); got != 2 {
		t.Fatalf("AddRef = %d, want 2", got)
	}
	if got := unknown.Release(); got != 1 {
		t.Fatalf("Release = %d, want 1", got)
	}
	if liveServerCount() != before+1 {
		t.Fatal("the handler was freed while a reference was still outstanding")
	}

	handler.release()
	if got := liveServerCount(); got != before {
		t.Fatalf("live COM objects = %d, want %d: handlers must not accumulate for the life of the process", got, before)
	}
}

// TestLateCompletionAfterAbandonReleasesTheResult locks the #37 fix: once the
// waiter has timed out and abandoned the handler, a late completion must drop
// the reference it took instead of parking it in a buffer nobody will ever
// drain - the GC frees an abandoned channel without calling COM Release, which
// stranded the freshly created controller and its browser processes.
func TestLateCompletionAfterAbandonReleasesTheResult(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	handler.abandon() // the waiter gave up: the timeout path

	if hr := invoked(handler.this, sOK, uintptr(unsafe.Pointer(object))); hr != sOK {
		t.Fatalf("invoked = %#x, want S_OK", hr)
	}
	if state.addRefs != 1 {
		t.Fatalf("addRefs = %d, want 1: the borrowed result must still be AddRef'd before the delivery decision", state.addRefs)
	}
	if state.releases != 1 {
		t.Fatalf("releases = %d, want 1: a completion with no waiter must drop its reference, not strand it", state.releases)
	}
	select {
	case <-handler.done:
		t.Fatal("nothing may be buffered on an abandoned handler")
	default:
	}
	runtime.KeepAlive(object)
}

// TestAbandonDrainsABufferedCompletion covers the other ordering: the
// completion landed in the buffer just before the waiter gave up. abandon must
// reclaim it, or the same reference strands the same way.
func TestAbandonDrainsABufferedCompletion(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	if hr := invoked(handler.this, sOK, uintptr(unsafe.Pointer(object))); hr != sOK {
		t.Fatalf("invoked = %#x, want S_OK", hr)
	}
	if state.releases != 0 {
		t.Fatalf("releases before abandon = %d, want 0: the buffered completion legitimately holds the reference", state.releases)
	}

	handler.abandon()

	if state.releases != 1 {
		t.Fatalf("releases after abandon = %d, want 1: abandon must drain and release a completion that beat the flag", state.releases)
	}
	runtime.KeepAlive(object)
}

// TestCompletionDeliveredToTheWaiterKeepsTheReference is the success-path
// regression guard: with a live waiter the reference must survive the hand-off
// - an over-eager release here would free the controller the caller is about
// to use.
func TestCompletionDeliveredToTheWaiterKeepsTheReference(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	if hr := invoked(handler.this, sOK, uintptr(unsafe.Pointer(object))); hr != sOK {
		t.Fatalf("invoked = %#x, want S_OK", hr)
	}
	result, err := waitFor(handler.done, time.Second, "the test completion")
	if err != nil {
		t.Fatalf("waitFor err = %v, want nil", err)
	}
	if got := uintptr(unsafe.Pointer(result.result)); got != uintptr(unsafe.Pointer(object)) {
		t.Fatalf("waitFor delivered %#x, want the fake object %#x", got, uintptr(unsafe.Pointer(object)))
	}
	if state.addRefs != 1 || state.releases != 0 {
		t.Fatalf("addRefs/releases = %d/%d, want 1/0: ownership passes to the waiter", state.addRefs, state.releases)
	}
	runtime.KeepAlive(object)
}

// TestSecondInvokeReleasesTheExtraReference pins the pre-existing double-fire
// defence, which the abandon flag must not have broken: the forbidden second
// completion drops its reference, the first stays buffered.
func TestSecondInvokeReleasesTheExtraReference(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	invoked(handler.this, sOK, uintptr(unsafe.Pointer(object)))
	invoked(handler.this, sOK, uintptr(unsafe.Pointer(object)))

	if state.addRefs != 2 {
		t.Fatalf("addRefs = %d, want 2", state.addRefs)
	}
	if state.releases != 1 {
		t.Fatalf("releases = %d, want 1: the second fire must drop its reference, the first keeps the buffer's", state.releases)
	}
	runtime.KeepAlive(object)
}

// TestCompletionResultReleasesAResultDeliveredWithAFailingHR locks the sibling
// leak found while fixing #37: the completion contract does not promise a null
// object on failure, so a failing HRESULT that still carried a result must
// release it rather than return an error with the reference in the wind.
func TestCompletionResultReleasesAResultDeliveredWithAFailingHR(t *testing.T) {
	object, state := newFakeUnknown(t)

	unknown, err := completionResult(completion{hr: eFail, result: object}, "controller")

	if err == nil {
		t.Fatal("a failing HRESULT must be an error")
	}
	if unknown != nil {
		t.Fatal("a failing HRESULT must not hand out the result")
	}
	if state.releases != 1 {
		t.Fatalf("releases = %d, want 1: the result delivered alongside the failure must be released", state.releases)
	}
	runtime.KeepAlive(object)
}

func TestCompletionResultHandsOwnershipToTheCaller(t *testing.T) {
	object, state := newFakeUnknown(t)

	unknown, err := completionResult(completion{hr: sOK, result: object}, "controller")

	if err != nil {
		t.Fatalf("completionResult err = %v, want nil", err)
	}
	if unknown != object {
		t.Fatalf("completionResult = %p, want %p", unknown, object)
	}
	if state.releases != 0 {
		t.Fatalf("releases = %d, want 0: ownership passes to the caller untouched", state.releases)
	}
	runtime.KeepAlive(object)
}

func TestCompletionResultRejectsASuccessWithNoResult(t *testing.T) {
	if _, err := completionResult(completion{hr: sOK, result: nil}, "environment"); err == nil {
		t.Fatal("success with a nil result must be an error")
	}
}

// abandon is called from two exits (timeout and the synchronous-failure
// defence); a repeated call must stay a no-op so no ordering of those exits
// can double-release anything.
func TestAbandonIsIdempotent(t *testing.T) {
	handler := newTestCompletedHandler(t)
	object, state := newFakeUnknown(t)

	invoked(handler.this, sOK, uintptr(unsafe.Pointer(object)))
	handler.abandon()
	handler.abandon()

	if state.releases != 1 {
		t.Fatalf("releases after two abandons = %d, want 1: the drain must fire once, never twice", state.releases)
	}
	runtime.KeepAlive(object)
}
