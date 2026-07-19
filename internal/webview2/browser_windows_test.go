//go:build windows

package webview2

import (
	"errors"
	"runtime"
	"testing"
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

	err := browser.registerEventsOrTearDown(func() error { return wantErr })

	if !errors.Is(err, wantErr) {
		t.Fatalf("registerEventsOrTearDown err = %v, want %v", err, wantErr)
	}
	if !browser.IsShuttingDown() {
		t.Fatal("a registerEvents failure must tear the browser down, or the environment, controller and core leak")
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

// The teardown release sequence (issue #63). releaseBrowserObjects is the
// seam ShuttingDown runs its Close-and-release through, so the ordering and the
// panic-independence can be pinned without a live COM runtime.

// TestReleaseBrowserObjectsRunsCloseThenReleasesInOrder locks the order the
// runtime requires: Close before the controller is released, then core, then
// environment. The window path relies on this exact sequence.
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

// TestReleaseBrowserObjectsToleratesNilCallbacks covers a partially embedded
// Browser: the controller may exist while core/environment do not, or none may.
// Absent objects pass nil callbacks, which must be skipped, not invoked.
func TestReleaseBrowserObjectsToleratesNilCallbacks(t *testing.T) {
	// All nil: a Browser that never embedded. Must not panic.
	releaseBrowserObjects(nil, nil, nil, nil, nil)

	// Controller only: Close and its release run, the others are skipped.
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
