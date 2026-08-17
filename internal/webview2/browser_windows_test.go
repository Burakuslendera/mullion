//go:build windows

package webview2

import (
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestRegisterEventsFailureTearsDownBrowser locks the leak fix for the embed error
// path. By the time Embed registers events it has already stored the environment,
// controller and core on the Browser, and the host assigns host.browser only after
// Embed returns nil - so a registration failure there orphans all three references
// unless Embed releases them itself.
//
// What runs here and what does not. A fresh Browser has nil COM fields; ShuttingDown
// tolerates them (every release is nil-guarded), so this drives the real failure
// control flow without a live runtime. The actual Release of the environment,
// controller and core can only be observed on Windows with a WebView2 runtime
// installed; this pins the load-bearing half that IS reachable headlessly - that a
// registration failure runs the teardown path at all, rather than returning and
// leaking.
func TestRegisterEventsFailureTearsDownBrowser(t *testing.T) {
	browser := New()
	wantErr := errors.New("register failed")
	var reports int
	browser.ErrorCallback = func(error) { reports++ }

	err := browser.registerEventsOrTearDown(func() error { return wantErr })

	if !errors.Is(err, wantErr) {
		t.Fatalf("registerEventsOrTearDown err = %v, want %v", err, wantErr)
	}
	if !browser.IsShuttingDown() {
		t.Fatal("a registerEvents failure must tear the browser down, or the environment, controller and core leak")
	}
	if reports != 0 {
		t.Fatalf("returned registration failure was internally reported %d times, want 0", reports)
	}
}

// TestRegisterEventsSuccessKeepsBrowser is the other half: a successful embed must
// not tear the browser down. Without it, "always tear down" would pass the failure
// test while breaking every real window.
func TestRegisterEventsSuccessKeepsBrowser(t *testing.T) {
	browser := New()

	if err := browser.registerEventsOrTearDown(func() error { return nil }); err != nil {
		t.Fatalf("registerEventsOrTearDown err = %v, want nil", err)
	}

	if browser.IsShuttingDown() {
		t.Fatal("a successful embed must not tear the browser down")
	}
}

func TestRegisterEventsPairsEveryNumericalConsumerWithItsSemanticHandler(t *testing.T) {
	baseline := liveServerCount()
	core, state := newFakeRegisterEventsCore(t)
	browser := New()
	browser.core = core

	consumers := []struct {
		name string
		slot int
		iid  windows.GUID
	}{
		{"NavigationStarting", fakeAddNavigationStartingSlot, IIDICoreWebView2NavigationStartingEventHandler},
		{"NavigationCompleted", fakeAddNavigationCompletedSlot, IIDICoreWebView2NavigationCompletedEventHandler},
		{"ProcessFailed", fakeAddProcessFailedSlot, IIDICoreWebView2ProcessFailedEventHandler},
		{"WebMessageReceived", fakeAddWebMessageReceivedSlot, IIDICoreWebView2WebMessageReceivedEventHandler},
		{"NewWindowRequested", fakeAddNewWindowRequestedSlot, IIDICoreWebView2NewWindowRequestedEventHandler},
		{"WebResourceRequested", fakeAddWebResourceRequestedSlot, IIDICoreWebView2WebResourceRequestedEventHandler},
	}
	releaseRuntimeReferences := func() {
		for _, handler := range state.registeredEventRefs {
			if handler != 0 && serverFor(handler) != nil {
				serverRelease(handler)
			}
		}
	}
	t.Cleanup(releaseRuntimeReferences)

	if err := browser.registerEvents(); err != nil {
		t.Fatalf("registerEvents returned %v, want nil", err)
	}
	if got, want := liveServerCount(), baseline+len(consumers); got != want {
		t.Fatalf("live servers after registration = %d, want %d runtime-owned handlers", got, want)
	}

	for _, consumer := range consumers {
		t.Run(consumer.name, func(t *testing.T) {
			handler := state.registeredEventHandlers[consumer.slot]
			if handler == 0 {
				t.Fatalf("numerical Add* slot %d did not receive a handler", consumer.slot)
			}
			if got := state.registeredEventAdds[consumer.slot]; got != 1 {
				t.Fatalf("numerical Add* slot %d calls = %d, want 1", consumer.slot, got)
			}
			server := serverFor(handler)
			if server == nil {
				t.Fatalf("handler passed to numerical Add* slot %d is not rooted", consumer.slot)
			}
			if got := atomic.LoadInt32(&server.refs); got != 1 {
				t.Fatalf("refs after package release = %d, want exactly the runtime's reference", got)
			}

			iid := consumer.iid
			var queried uintptr
			hr := serverQueryInterface(
				handler,
				uintptr(unsafe.Pointer(&iid)),
				uintptr(unsafe.Pointer(&queried)),
			)
			if hr != sOK || queried != handler {
				t.Fatalf("slot %d handler QueryInterface(%s) = (hr=%#x, ptr=%#x), want (S_OK, %#x)", consumer.slot, consumer.name, hr, queried, handler)
			}
			if got := atomic.LoadInt32(&server.refs); got != 2 {
				t.Fatalf("refs after semantic QueryInterface = %d, want runtime plus queried references", got)
			}
			serverRelease(queried)
			if got := atomic.LoadInt32(&server.refs); got != 1 {
				t.Fatalf("refs after releasing queried interface = %d, want runtime reference", got)
			}
		})
	}

	releaseRuntimeReferences()
	if got := liveServerCount(); got != baseline {
		t.Fatalf("live servers after all runtime releases = %d, want baseline %d", got, baseline)
	}
	runtime.KeepAlive(core)
}
func TestReturnedSurfaceOperationFailuresAreNotInternallyReported(t *testing.T) {
	core, coreState := newFakeSurfaceCore(t, eFail)
	controller, controllerState := newFakeSurfaceController(t, eFail)
	browser := New()
	browser.core = core
	browser.controller = controller
	var reports int
	browser.ErrorCallback = func(error) { reports++ }

	operations := []struct {
		name string
		run  func() error
	}{
		{"Navigate", func() error { return browser.Navigate("https://example.test/") }},
		{"Init", func() error { return browser.Init("void 0") }},
		{"Eval", func() error { return browser.Eval("void 0") }},
		{"PostWebMessage", func() error { return browser.PostWebMessage(`{"event":"test"}`) }},
		{"Show", browser.Show},
		{"Hide", browser.Hide},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil {
				t.Fatal("operation returned nil, want the fake HRESULT failure")
			}
		})
	}
	if reports != 0 {
		t.Fatalf("returned operation failures were internally reported %d times, want 0", reports)
	}
	if got := coreState.operationCalls + controllerState.operationCalls; got != len(operations) {
		t.Fatalf("COM calls = %d, want %d", got, len(operations))
	}
}

// boundsPolicyBrowser stands a Browser on the given fake controller and records
// what each report channel receives, for the applyBoundsPolicy severity tests.
func boundsPolicyBrowser(controller *ICoreWebView2Controller) (browser *Browser, warnings, errs *[]error) {
	browser = New()
	browser.controller = controller
	warnings, errs = &[]error{}, &[]error{}
	browser.WarningCallback = func(err error) { *warnings = append(*warnings, err) }
	browser.ErrorCallback = func(err error) { *errs = append(*errs, err) }
	return browser, warnings, errs
}

// TestApplyBoundsPolicyRoutesAMissingController3ToTheWarningChannel locks the
// severity contract of issue #32. An older runtime answers E_NOINTERFACE for
// ICoreWebView2Controller3, a condition applyBoundsPolicy's own contract calls
// "a warning, not a failure" - yet the miss used to route through ErrorCallback
// and surface as ERROR on every embed, teaching an operator to distrust the one
// level the host reserves for events that need attention.
func TestApplyBoundsPolicyRoutesAMissingController3ToTheWarningChannel(t *testing.T) {
	controller, _ := newFakeController(t, nil)
	browser, warnings, errs := boundsPolicyBrowser(controller)

	browser.applyBoundsPolicy()

	if len(*warnings) != 1 {
		t.Fatalf("warnings = %d (%v), want exactly 1 for the Controller3 miss", len(*warnings), *warnings)
	}
	if len(*errs) != 0 {
		t.Fatalf("errors = %v, want none: a missing optional interface is not a failure (issue #32)", *errs)
	}
}

// TestApplyBoundsPolicyKeepsSetterFailuresOnTheErrorChannel is the other half of
// the split: PutBoundsMode / PutShouldDetectMonitorScaleChanges can only fail on
// a runtime that does implement Controller3, so those failures are genuine and
// must stay on the error channel - a fix that blanket-downgraded the whole
// function to Warn would pass the miss test and silence real failures. The
// reference QueryInterface handed out must also be dropped exactly once.
func TestApplyBoundsPolicyKeepsSetterFailuresOnTheErrorChannel(t *testing.T) {
	controller3, c3State := newFakeController3(t, eFail)
	controller, _ := newFakeController(t, controller3)
	browser, warnings, errs := boundsPolicyBrowser(controller)

	browser.applyBoundsPolicy()

	if len(*errs) != 2 {
		t.Fatalf("errors = %d (%v), want 2: both failing setters are real failures", len(*errs), *errs)
	}
	if len(*warnings) != 0 {
		t.Fatalf("warnings = %v, want none for setter failures", *warnings)
	}
	if c3State.puts != 2 {
		t.Fatalf("setter calls = %d, want 2: the first failure must not abort the second setting", c3State.puts)
	}
	if c3State.addRefs != 1 || c3State.releases != 1 {
		t.Fatalf("controller3 addRefs=%d releases=%d, want 1/1: the queried reference must be dropped exactly once", c3State.addRefs, c3State.releases)
	}
	// The fake controller reaches controller3 only through the uintptr in its
	// state, which the collector does not treat as a reference.
	runtime.KeepAlive(controller3)
}

// TestApplyBoundsPolicyIsSilentOnAHealthyController3 pins the common case: both
// setters succeed, neither channel fires, and the queried reference is dropped.
func TestApplyBoundsPolicyIsSilentOnAHealthyController3(t *testing.T) {
	controller3, c3State := newFakeController3(t, sOK)
	controller, _ := newFakeController(t, controller3)
	browser, warnings, errs := boundsPolicyBrowser(controller)

	browser.applyBoundsPolicy()

	if len(*warnings) != 0 || len(*errs) != 0 {
		t.Fatalf("warnings = %v, errors = %v, want both empty on a healthy runtime", *warnings, *errs)
	}
	if c3State.puts != 2 {
		t.Fatalf("setter calls = %d, want 2", c3State.puts)
	}
	if c3State.addRefs != 1 || c3State.releases != 1 {
		t.Fatalf("controller3 addRefs=%d releases=%d, want 1/1", c3State.addRefs, c3State.releases)
	}
	runtime.KeepAlive(controller3)
}

// TestHandleWebResourceRequestedReleasesTheRequest locks the lifetime of the
// reference GetRequest hands out. The event fires for every intercepted
// resource in embedded-FS mode, so a missing release is not a one-off: it leaks
// one ICoreWebView2WebResourceRequest per document, stylesheet, script, image
// and fetch, without bound, for the life of the window. The release must also
// not come earlier: the callback is still reading the request, and the request
// interface has no exported Release, so this closure is the only owner.
func TestHandleWebResourceRequestedReleasesTheRequest(t *testing.T) {
	request, requestState := newFakeWebResourceRequest(t)
	args, argsState := newFakeWebResourceArgs(t, request)

	browser := New()
	var callbackRequest *ICoreWebView2WebResourceRequest
	var releasedDuringCallback bool
	browser.WebResourceRequestedCallback = func(got *ICoreWebView2WebResourceRequest, _ *ICoreWebView2WebResourceRequestedEventArgs) {
		callbackRequest = got
		releasedDuringCallback = requestState.releases != 0
	}

	browser.handleWebResourceRequested(args)

	if callbackRequest != request {
		t.Fatalf("callback received %p, want the fake request %p", callbackRequest, request)
	}
	if releasedDuringCallback {
		t.Fatal("the request was released before the callback ran: use-after-free")
	}
	if got := requestState.releases; got != 1 {
		t.Fatalf("request releases = %d, want exactly 1: fewer leaks one COM object per intercepted request, more frees an object the runtime still owns", got)
	}
	if got := argsState.releases; got != 0 {
		t.Fatalf("args releases = %d, want 0: the args pointer is borrowed for the event, never owned", got)
	}
	runtime.KeepAlive(request)
	runtime.KeepAlive(args)
}

// A panicking callback must not leak the request either: the deferred release
// runs during the unwind, before the handler dispatch recovers the panic.
func TestHandleWebResourceRequestedReleasesOnCallbackPanic(t *testing.T) {
	request, requestState := newFakeWebResourceRequest(t)
	args, _ := newFakeWebResourceArgs(t, request)

	browser := New()
	browser.WebResourceRequestedCallback = func(*ICoreWebView2WebResourceRequest, *ICoreWebView2WebResourceRequestedEventArgs) {
		panic("callback exploded")
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the callback's panic must propagate for the handler dispatch to report")
			}
		}()
		browser.handleWebResourceRequested(args)
	}()

	if got := requestState.releases; got != 1 {
		t.Fatalf("request releases after a panicking callback = %d, want 1", got)
	}
	runtime.KeepAlive(request)
	runtime.KeepAlive(args)
}

// Without a registered callback no reference is taken, so there is nothing to
// release - taking one anyway would put an unreleased owned reference on every
// event of a host that never asked for requests.
func TestHandleWebResourceRequestedWithoutCallbackTakesNoReference(t *testing.T) {
	request, requestState := newFakeWebResourceRequest(t)
	args, argsState := newFakeWebResourceArgs(t, request)

	browser := New()
	browser.handleWebResourceRequested(args)

	if got := argsState.getRequest; got != 0 {
		t.Fatalf("GetRequest calls = %d, want 0 when no callback is registered", got)
	}
	if got := requestState.releases; got != 0 {
		t.Fatalf("request releases = %d, want 0: no reference was taken", got)
	}
	runtime.KeepAlive(request)
	runtime.KeepAlive(args)
}

// A failing GetRequest owns nothing: the error is reported and the callback is
// not run with a nil request.
func TestHandleWebResourceRequestedReportsGetRequestFailure(t *testing.T) {
	args, _ := newFakeWebResourceArgs(t, nil)

	browser := New()
	var reported error
	browser.ErrorCallback = func(err error) { reported = err }
	browser.WebResourceRequestedCallback = func(*ICoreWebView2WebResourceRequest, *ICoreWebView2WebResourceRequestedEventArgs) {
		t.Error("the callback must not run when GetRequest fails")
	}

	browser.handleWebResourceRequested(args)

	if reported == nil {
		t.Fatal("a GetRequest failure must reach the error callback")
	}
	runtime.KeepAlive(args)
}

// The teardown release sequence (issue #63). releaseBrowserObjects is the seam
// ShuttingDown uses, so implementation order and panic-independence can be pinned
// without a live COM runtime.

// TestReleaseBrowserObjectsRunsCloseThenReleasesInOrder pins package order.
// The runtime requirement is that Close precede every Release; the relative
// controller, core and environment order is deterministic seam behavior.
func TestReleaseBrowserObjectsRunsCloseThenReleasesInOrder(t *testing.T) {
	var order []string
	releaseBrowserObjects(
		func() error { order = append(order, "close"); return nil },
		func() { order = append(order, "controller") },
		func() { order = append(order, "core") },
		func() { order = append(order, "environment") },
		func(error) { t.Error("no error was returned by Close; reportErr must not run") },
	)
	want := []string{"close", "controller", "core", "environment"}
	if len(order) != len(want) {
		t.Fatalf("release order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("release order = %v, want %v", order, want)
		}
	}
}

// TestReleaseBrowserObjectsReleasesEveryObjectWhenClosePanics is the #63 fix
// itself: a panic in Close - this runs inside the panic-recovering window
// procedure, and ShuttingDown cannot be retried - must not strand the three
// owned references. Separate deferred drops guarantee it; a single wrapping
// closure would leave all three unreleased, which is what this test rejects.
func TestReleaseBrowserObjectsReleasesEveryObjectWhenClosePanics(t *testing.T) {
	var releasedController, releasedCore, releasedEnvironment bool
	var reported any
	func() {
		// The Close panic propagates out, exactly as it would to the window
		// procedure's recover; contain it here so the test can inspect the drops.
		defer func() {
			if recover() == nil {
				t.Error("a panic in Close must propagate for the window procedure to report")
			}
		}()
		releaseBrowserObjects(
			func() error { panic("close blew up") },
			func() { releasedController = true },
			func() { releasedCore = true },
			func() { releasedEnvironment = true },
			func(err error) { reported = err },
		)
	}()
	if !releasedController || !releasedCore || !releasedEnvironment {
		t.Fatalf("a panic in Close stranded a reference: controller=%t core=%t environment=%t",
			releasedController, releasedCore, releasedEnvironment)
	}
	if reported != nil {
		t.Fatalf("Close panicked rather than returning an error; reportErr ran with %v", reported)
	}
}

// TestReleaseBrowserObjectsReleasesEveryObjectWhenReportPanics covers the
// Embed failure window: a secondary Close error reaches embedder code, and a
// panic there must not strand the three owned references.
func TestReleaseBrowserObjectsReleasesEveryObjectWhenReportPanics(t *testing.T) {
	var releasedController, releasedCore, releasedEnvironment bool
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a panic in reportErr must propagate for the caller's recovery")
			}
		}()
		releaseBrowserObjects(
			func() error { return errors.New("close failed") },
			func() { releasedController = true },
			func() { releasedCore = true },
			func() { releasedEnvironment = true },
			func(error) { panic("report exploded") },
		)
	}()
	if !releasedController || !releasedCore || !releasedEnvironment {
		t.Fatalf("a panic in reportErr stranded a reference: controller=%t core=%t environment=%t",
			releasedController, releasedCore, releasedEnvironment)
	}
}

// TestReleaseBrowserObjectsReportsACloseError locks the other direction of the
// Close-error branch: a Close that returns a real error (rather than nil or a
// panic) must reach reportErr, and the three releases must still run. The order
// and panic tests only cover Close returning nil or panicking, so without this
// a regression that dropped the reportErr call would keep the suite green.
func TestReleaseBrowserObjectsReportsACloseError(t *testing.T) {
	wantErr := errors.New("close failed")
	var reported error
	var releasedController, releasedCore, releasedEnvironment bool
	releaseBrowserObjects(
		func() error { return wantErr },
		func() { releasedController = true },
		func() { releasedCore = true },
		func() { releasedEnvironment = true },
		func(err error) { reported = err },
	)
	if !errors.Is(reported, wantErr) {
		t.Fatalf("reported = %v, want %v: a Close error must reach the error callback", reported, wantErr)
	}
	if !releasedController || !releasedCore || !releasedEnvironment {
		t.Fatalf("a Close error must not skip the releases: controller=%t core=%t environment=%t",
			releasedController, releasedCore, releasedEnvironment)
	}
}

// TestReleaseBrowserObjectsToleratesNilCallbacks covers defensive helper states.
// Transferred Browser ownership contains controller, core and environment
// together; controller-only is a test-only state.
func TestReleaseBrowserObjectsToleratesNilCallbacks(t *testing.T) {
	// All nil: defensive no-owned-object state. Must not panic.
	releaseBrowserObjects(nil, nil, nil, nil, nil)

	// Controller only: test-only state; Close and release run.
	var closed, releasedController bool
	releaseBrowserObjects(
		func() error { closed = true; return nil },
		func() { releasedController = true },
		nil, nil, nil,
	)
	if !closed || !releasedController {
		t.Fatalf("controller-only teardown: closed=%t releasedController=%t, want both true", closed, releasedController)
	}
}
