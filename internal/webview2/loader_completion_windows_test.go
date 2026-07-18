//go:build windows

package webview2

import (
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// Lifetime tests for the loader's completion handlers. invoked() AddRefs the
// borrowed result before parking it in the handler's one-slot buffer for
// waitFor to consume; every test below is about who drops that reference when
// the normal hand-off breaks. The completions are driven directly - invoked is
// exactly what the runtime's Invoke trampoline calls - against fake IUnknowns,
// so no WebView2 runtime and no window are involved.

func newTestCompletedHandler(t *testing.T) *completedHandler {
	t.Helper()
	handler := newCompletedHandler(uintptr(unsafe.Pointer(&controllerCompletedVtable)), iidControllerCompletedHandler)
	t.Cleanup(handler.release)
	return handler
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
