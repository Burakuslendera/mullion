//go:build windows

package webview2

// Tests for the Go-implemented event handler COM objects.
//
// These run headless: no WebView2 runtime, no browser process, no window. That
// is possible because a COM object is just a struct whose first word points at
// a table of function pointers - so the tests can play the runtime's part and
// call straight through the vtable, exactly as Chromium would, and observe what
// comes back.
//
// The three things worth proving here all fail catastrophically rather than
// gracefully in production: a wrong Invoke offset calls the wrong function
// pointer, a wrong reference count is either a leak or a use-after-free, and an
// escaping panic kills the process from inside a Chromium stack frame.

import (
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// captureHandlerPanics redirects the panic hook for one test and restores it
// afterwards, so a deliberate panic does not print to stderr during the run.
func captureHandlerPanics(t *testing.T) *[]string {
	t.Helper()
	var events []string
	SetHandlerPanicHook(func(event string, recovered any, stack []byte) {
		events = append(events, event)
	})
	t.Cleanup(func() { SetHandlerPanicHook(nil) })
	return &events
}

// TestEventHandlerVtblLayout pins the one offset that matters: Invoke sits
// immediately after IUnknown, at slot 3. All five handler interfaces share this
// vtable, so this single assertion covers all five.
func TestEventHandlerVtblLayout(t *testing.T) {
	var v eventHandlerVtbl
	if got, want := unsafe.Sizeof(v), 4*slotSize; got != want {
		t.Fatalf("eventHandlerVtbl = %d bytes (%d slots), want %d (4 slots: IUnknown + Invoke)",
			got, got/slotSize, want)
	}
	for _, tc := range []struct {
		name  string
		off   uintptr
		index uintptr
	}{
		{"QueryInterface", unsafe.Offsetof(v.QueryInterface), 0},
		{"AddRef", unsafe.Offsetof(v.AddRef), 1},
		{"Release", unsafe.Offsetof(v.Release), 2},
		{"Invoke", unsafe.Offsetof(v.Invoke), 3},
	} {
		if want := tc.index * slotSize; tc.off != want {
			t.Errorf("eventHandlerVtbl.%s: offset %d (slot %d), want %d (slot %d)",
				tc.name, tc.off, tc.off/slotSize, want, tc.index)
		}
	}
}

// TestEventHandlerIIDs re-parses each handler IID from its canonical string and
// compares it against the hand-transcribed literal. A single swapped nibble
// would compile fine and only show up as the runtime refusing to register the
// handler.
func TestEventHandlerIIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		got  windows.GUID
	}{
		{"ICoreWebView2WebMessageReceivedEventHandler", "{57213f19-00e6-49fa-8e07-898ea01ecbd2}", IIDICoreWebView2WebMessageReceivedEventHandler},
		{"ICoreWebView2WebResourceRequestedEventHandler", "{ab00b74c-15f1-4646-80e8-e76341d25d71}", IIDICoreWebView2WebResourceRequestedEventHandler},
		{"ICoreWebView2NavigationStartingEventHandler", "{9adbe429-f36d-432b-9ddc-f8881fbd76e3}", IIDICoreWebView2NavigationStartingEventHandler},
		{"ICoreWebView2NavigationCompletedEventHandler", "{d33a35bf-1c49-4f98-93ab-006e0533fe1c}", IIDICoreWebView2NavigationCompletedEventHandler},
		{"ICoreWebView2ProcessFailedEventHandler", "{79e0aea4-990b-42d9-aa1d-0fcc2e5bc7f1}", IIDICoreWebView2ProcessFailedEventHandler},
	} {
		want, err := windows.GUIDFromString(tc.text)
		if err != nil {
			t.Fatalf("%s: cannot parse reference IID %s: %v", tc.name, tc.text, err)
		}
		if tc.got != want {
			t.Errorf("%s: IID = %+v, want %+v (%s)", tc.name, tc.got, want, tc.text)
		}
	}
}

// Every constructor must produce an object whose first word is the shared
// vtable, and must record its own interface's IID - that pairing is what makes
// QueryInterface answer correctly for five different interfaces off one vtable.
func TestConstructorsRegisterSharedVtableAndOwnIID(t *testing.T) {
	wantVtbl := uintptr(unsafe.Pointer(&eventHandlerVtable))

	for _, tc := range []struct {
		name    string
		handler unsafe.Pointer
		iid     windows.GUID
	}{
		{"WebMessageReceived",
			NewWebMessageReceivedHandler(func(*ICoreWebView2, *ICoreWebView2WebMessageReceivedEventArgs) {}),
			IIDICoreWebView2WebMessageReceivedEventHandler},
		{"WebResourceRequested",
			NewWebResourceRequestedHandler(func(*ICoreWebView2, *ICoreWebView2WebResourceRequestedEventArgs) {}),
			IIDICoreWebView2WebResourceRequestedEventHandler},
		{"NavigationStarting",
			NewNavigationStartingHandler(func(*ICoreWebView2, *ICoreWebView2NavigationStartingEventArgs) {}),
			IIDICoreWebView2NavigationStartingEventHandler},
		{"NavigationCompleted",
			NewNavigationCompletedHandler(func(*ICoreWebView2, *ICoreWebView2NavigationCompletedEventArgs) {}),
			IIDICoreWebView2NavigationCompletedEventHandler},
		{"ProcessFailed",
			NewProcessFailedHandler(func(*ICoreWebView2, *ICoreWebView2ProcessFailedEventArgs) {}),
			IIDICoreWebView2ProcessFailedEventHandler},
	} {
		server := serverFor(uintptr(tc.handler))
		if server == nil {
			t.Fatalf("%s: handler was not registered", tc.name)
		}
		if server.vtbl != wantVtbl {
			t.Errorf("%s: vtbl = %#x, want the shared eventHandlerVtable at %#x", tc.name, server.vtbl, wantVtbl)
		}
		if server.iid != tc.iid {
			t.Errorf("%s: iid = %+v, want %+v", tc.name, server.iid, tc.iid)
		}
		if got := atomic.LoadInt32(&server.refs); got != 1 {
			t.Errorf("%s: refcount after construction = %d, want 1 (the caller's reference)", tc.name, got)
		}
		ReleaseHandler(tc.handler)
	}
}

// QueryInterface must answer for its own interface and for IUnknown, and refuse
// everything else.
//
// The refusal is the load-bearing half: WebView2 probes for newer interfaces it
// might be able to use. A handler that answered S_OK for an unknown IID would be
// handed calls to methods it never implemented, dispatched off the end of a
// 4-slot vtable.
func TestHandlerQueryInterface(t *testing.T) {
	handler := NewNavigationCompletedHandler(func(*ICoreWebView2, *ICoreWebView2NavigationCompletedEventArgs) {})
	defer ReleaseHandler(handler)
	this := uintptr(handler)

	server := serverFor(this)
	if server == nil {
		t.Fatal("handler was not registered")
	}

	queryInterface := func(iid windows.GUID) (uintptr, uintptr) {
		var out uintptr
		hr, _, _ := eventHandlerVtable.QueryInterface.Call(
			this,
			uintptr(unsafe.Pointer(&iid)),
			uintptr(unsafe.Pointer(&out)),
		)
		return hr, out
	}

	before := atomic.LoadInt32(&server.refs)

	// Own IID: succeeds, hands back the same pointer, and takes a reference.
	hr, out := queryInterface(IIDICoreWebView2NavigationCompletedEventHandler)
	if hr != sOK {
		t.Errorf("QueryInterface(own IID) = %#x, want S_OK", hr)
	}
	if out != this {
		t.Errorf("QueryInterface(own IID) wrote %#x, want the interface pointer %#x", out, this)
	}
	if got, want := atomic.LoadInt32(&server.refs), before+1; got != want {
		t.Errorf("refcount after QueryInterface(own IID) = %d, want %d", got, want)
	}

	// IUnknown: same.
	hr, out = queryInterface(IIDIUnknown)
	if hr != sOK {
		t.Errorf("QueryInterface(IUnknown) = %#x, want S_OK", hr)
	}
	if out != this {
		t.Errorf("QueryInterface(IUnknown) wrote %#x, want %#x", out, this)
	}
	if got, want := atomic.LoadInt32(&server.refs), before+2; got != want {
		t.Errorf("refcount after QueryInterface(IUnknown) = %d, want %d", got, want)
	}

	// A different handler's IID is still a foreign interface: it must be refused,
	// and must not take a reference or write a pointer.
	hr, out = queryInterface(IIDICoreWebView2ProcessFailedEventHandler)
	if hr != eNoInterface {
		t.Errorf("QueryInterface(foreign IID) = %#x, want E_NOINTERFACE (%#x)", hr, eNoInterface)
	}
	if out != 0 {
		t.Errorf("QueryInterface(foreign IID) wrote %#x, want a null out-pointer", out)
	}
	if got, want := atomic.LoadInt32(&server.refs), before+2; got != want {
		t.Errorf("refcount after a refused QueryInterface = %d, want %d (unchanged)", got, want)
	}

	// Give back the two references QueryInterface handed out.
	eventHandlerVtable.Release.Call(this)
	eventHandlerVtable.Release.Call(this)
}

// TestHandlerRefcountLifecycle walks the exact sequence a real registration
// goes through, and proves the object is reclaimed at the end - neither leaked
// nor freed early.
//
//	New*Handler   -> refs 1   (ours)
//	add_* AddRefs -> refs 2   (ours + the runtime's)
//	ReleaseHandler-> refs 1   (the runtime's; object MUST still be alive here -
//	                           this is the step that would be a use-after-free
//	                           if the runtime did not hold its own reference)
//	WebView drops -> refs 0   (object reclaimed, map entry gone)
func TestHandlerRefcountLifecycle(t *testing.T) {
	baseline := liveServerCount()

	handler := NewWebMessageReceivedHandler(func(*ICoreWebView2, *ICoreWebView2WebMessageReceivedEventArgs) {})
	this := uintptr(handler)

	if got, want := liveServerCount(), baseline+1; got != want {
		t.Fatalf("live servers after construction = %d, want %d", got, want)
	}
	server := serverFor(this)
	if server == nil {
		t.Fatal("handler was not registered")
	}
	if got := atomic.LoadInt32(&server.refs); got != 1 {
		t.Fatalf("refs after construction = %d, want 1", got)
	}

	// The runtime takes its own reference inside add_*.
	eventHandlerVtable.AddRef.Call(this)
	if got := atomic.LoadInt32(&server.refs); got != 2 {
		t.Fatalf("refs after the runtime's AddRef = %d, want 2", got)
	}

	// We drop ours. The object must survive: the runtime is still holding it and
	// will call Invoke on it later.
	ReleaseHandler(handler)
	if got := atomic.LoadInt32(&server.refs); got != 1 {
		t.Fatalf("refs after ReleaseHandler = %d, want 1 (the runtime's)", got)
	}
	if serverFor(this) == nil {
		t.Fatal("handler was reclaimed while the runtime still held a reference: this is the use-after-free")
	}
	if got, want := liveServerCount(), baseline+1; got != want {
		t.Fatalf("live servers while the runtime holds the handler = %d, want %d", got, want)
	}

	// The WebView is destroyed and releases its reference.
	eventHandlerVtable.Release.Call(this)

	if serverFor(this) != nil {
		t.Error("handler still registered after its last reference was dropped: leak")
	}
	if got := liveServerCount(); got != baseline {
		t.Errorf("live servers after teardown = %d, want %d (back to baseline: no leak)", got, baseline)
	}
}

// Invoke must reach the Go callback with the arguments typed, and report S_OK.
// Calling straight through the vtable is exactly what the runtime does, so this
// exercises the real windows.NewCallback trampoline without a browser.
func TestHandlerInvokeDispatch(t *testing.T) {
	var called int
	var gotSender *ICoreWebView2
	var gotArgs *ICoreWebView2ProcessFailedEventArgs

	handler := NewProcessFailedHandler(func(sender *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) {
		called++
		gotSender = sender
		gotArgs = args
	})
	defer ReleaseHandler(handler)

	hr, _, _ := eventHandlerVtable.Invoke.Call(uintptr(handler), 0, 0)
	if hr != sOK {
		t.Errorf("Invoke = %#x, want S_OK", hr)
	}
	if called != 1 {
		t.Fatalf("callback ran %d times, want 1", called)
	}
	// Null interface pointers must arrive as nil rather than as a pointer to
	// address zero, or the callback would crash the moment it touched them.
	if gotSender != nil {
		t.Errorf("sender = %p, want nil for a null interface pointer", gotSender)
	}
	if gotArgs != nil {
		t.Errorf("args = %p, want nil for a null interface pointer", gotArgs)
	}
}

// TestHandlerInvokePanicIsContained pins the recover in dispatch.
//
// Note what this test can and cannot reproduce. It drives Invoke synchronously
// from the test goroutine, so without the recover the panic unwinds back out of
// the callback into this goroutine and `testing` catches it: the test fails
// loudly (verified by deleting the recover), but the binary survives. In
// production the call arrives on a Chromium stack instead, and a panic unwinding
// out of a windows.NewCallback trampoline has no Go frame above it to land in -
// that is the case that kills the process, and it is not reachable from a unit
// test. So the failing assertion here stands in for a crash that only happens
// for real.
//
// S_OK on top of containment is the other half: see eventHandlerInvoke.
func TestHandlerInvokePanicIsContained(t *testing.T) {
	events := captureHandlerPanics(t)

	handler := NewWebResourceRequestedHandler(func(*ICoreWebView2, *ICoreWebView2WebResourceRequestedEventArgs) {
		panic("handler exploded")
	})
	defer ReleaseHandler(handler)

	hr, _, _ := eventHandlerVtable.Invoke.Call(uintptr(handler), 0, 0)

	// Reaching this line at all is the point of the test.
	if hr != sOK {
		t.Errorf("Invoke after a panicking callback = %#x, want S_OK: a failing HRESULT would cancel the "+
			"resource request and blank the asset, turning a Go bug into a dead window", hr)
	}
	if len(*events) != 1 {
		t.Fatalf("panic hook fired %d times, want 1 - a swallowed panic is invisible", len(*events))
	}
	if (*events)[0] != "WebResourceRequested" {
		t.Errorf("panic reported for event %q, want %q", (*events)[0], "WebResourceRequested")
	}

	// The handler must still be usable: one bad event does not poison it.
	hr, _, _ = eventHandlerVtable.Invoke.Call(uintptr(handler), 0, 0)
	if hr != sOK {
		t.Errorf("second Invoke = %#x, want S_OK", hr)
	}
	if len(*events) != 2 {
		t.Errorf("panic hook fired %d times in total, want 2", len(*events))
	}
}

// A panicking panic hook must not defeat the purpose of the panic hook.
func TestHandlerPanicHookThatPanicsIsContained(t *testing.T) {
	SetHandlerPanicHook(func(event string, recovered any, stack []byte) {
		panic("the hook exploded too")
	})
	t.Cleanup(func() { SetHandlerPanicHook(nil) })

	handler := NewProcessFailedHandler(func(*ICoreWebView2, *ICoreWebView2ProcessFailedEventArgs) {
		panic("handler exploded")
	})
	defer ReleaseHandler(handler)

	hr, _, _ := eventHandlerVtable.Invoke.Call(uintptr(handler), 0, 0)
	if hr != sOK {
		t.Errorf("Invoke = %#x, want S_OK even when the panic hook itself panics", hr)
	}
}

// The panic report must name the event, so a crash log points at which callback
// misbehaved rather than just "somewhere in COM".
func TestHandlerPanicReportCarriesEventAndStack(t *testing.T) {
	var (
		gotEvent     string
		gotRecovered any
		gotStack     []byte
	)
	SetHandlerPanicHook(func(event string, recovered any, stack []byte) {
		gotEvent, gotRecovered, gotStack = event, recovered, stack
	})
	t.Cleanup(func() { SetHandlerPanicHook(nil) })

	handler := NewNavigationCompletedHandler(func(*ICoreWebView2, *ICoreWebView2NavigationCompletedEventArgs) {
		panic("nav boom")
	})
	defer ReleaseHandler(handler)

	eventHandlerVtable.Invoke.Call(uintptr(handler), 0, 0)

	if gotEvent != "NavigationCompleted" {
		t.Errorf("event = %q, want %q", gotEvent, "NavigationCompleted")
	}
	if text, ok := gotRecovered.(string); !ok || text != "nav boom" {
		t.Errorf("recovered = %v, want the panic value \"nav boom\"", gotRecovered)
	}
	if !strings.Contains(string(gotStack), "dispatch") {
		t.Errorf("stack does not mention the dispatch frame, so it will not lead back to the callback:\n%s", gotStack)
	}
}

// Invoke on a `this` that is not one of ours must fail rather than dereference
// it. Unreachable in practice, but it is the difference between an error return
// and a wild jump if anything ever goes wrong upstream.
func TestHandlerInvokeUnknownThis(t *testing.T) {
	hr, _, _ := eventHandlerVtable.Invoke.Call(0, 0, 0)
	if hr != eFail {
		t.Errorf("Invoke on an unregistered `this` = %#x, want E_FAIL (%#x)", hr, eFail)
	}
}

// ReleaseHandler must tolerate nil and must not blow up if it is somehow called
// after the object is gone - the second call finds no server and does nothing.
func TestReleaseHandlerIsDefensive(t *testing.T) {
	ReleaseHandler(nil) // must not panic

	baseline := liveServerCount()
	handler := NewProcessFailedHandler(func(*ICoreWebView2, *ICoreWebView2ProcessFailedEventArgs) {})
	ReleaseHandler(handler)
	if got := liveServerCount(); got != baseline {
		t.Fatalf("live servers = %d, want %d after releasing the only reference", got, baseline)
	}
	// Releasing again is a caller bug; it must not panic here, though it cannot
	// undo the damage of a premature release in the real world.
	ReleaseHandler(handler)
	if got := liveServerCount(); got != baseline {
		t.Errorf("live servers = %d, want %d after a redundant release", got, baseline)
	}
}
