//go:build windows

package webview2

import (
	"errors"
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
