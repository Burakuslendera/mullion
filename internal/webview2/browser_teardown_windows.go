//go:build windows

package webview2

// Teardown: closing the controller and dropping the three COM references a live
// Browser owns, in the order the runtime requires and with each drop
// independent of the others so a panic cannot strand one (issue #63). Split
// from browser_windows.go, whose Embed is the half that acquires what this half
// releases.

// ShuttingDown closes the controller and drops the browser's references.
//
// It is called from the window procedure while the HWND is still alive: closing
// the controller after its parent window is gone leaves the runtime's own
// child windows orphaned, and the teardown reports failures nobody can act on.
func (browser *Browser) ShuttingDown() {
	browser.mu.Lock()
	if browser.shuttingDown {
		browser.mu.Unlock()
		return
	}
	browser.shuttingDown = true
	controller := browser.controller
	core := browser.core
	environment := browser.environment
	browser.controller = nil
	browser.core = nil
	browser.environment = nil
	browser.mu.Unlock()

	var closeController func() error
	var releaseController, releaseCore, releaseEnvironment func()
	if controller != nil {
		closeController = controller.Close
		releaseController = func() { asUnknown(controller).Release() }
	}
	// GetCoreWebView2 returned a reference this Browser owns (see its doc in
	// interfaces_controller_windows.go). Closing the controller does not drop it,
	// so release it explicitly - otherwise one ICoreWebView2 leaks on every
	// teardown, which grows without bound in a process that opens and closes many
	// windows.
	if core != nil {
		releaseCore = func() { asUnknown(core).Release() }
	}
	if environment != nil {
		releaseEnvironment = environment.Release
	}
	releaseBrowserObjects(closeController, releaseController, releaseCore, releaseEnvironment, browser.reportError)
}

// releaseBrowserObjects closes the controller and drops the three COM
// references a live Browser owns, each under its own deferred call.
//
// The separate defers are load-bearing, not style. ShuttingDown runs inside the
// panic-recovering window procedure, and its shuttingDown guard makes a retry a
// no-op, so a panic partway through the release sequence would strand whatever
// had not yet been dropped - one ICoreWebView2 or the environment - for good
// (issue #63). A panic inside a deferred call still runs the remaining deferred
// calls, so each drop is independent; a single wrapping closure would skip the
// rest of itself once one call panicked. Deferred calls run
// last-registered-first, so registering the Close last keeps the
// Close-before-Release order the runtime requires. Nil callbacks are skipped so
// a partially embedded Browser tears down cleanly.
//
// It takes callbacks rather than the COM pointers so the ordering and the
// panic-independence are testable without a live runtime (docs/decisions/0006).
func releaseBrowserObjects(closeController func() error, releaseController, releaseCore, releaseEnvironment func(), reportErr func(error)) {
	if releaseEnvironment != nil {
		defer releaseEnvironment()
	}
	if releaseCore != nil {
		defer releaseCore()
	}
	if releaseController != nil {
		defer releaseController()
	}
	if closeController != nil {
		defer func() {
			if err := closeController(); err != nil && reportErr != nil {
				reportErr(err)
			}
		}()
	}
}

// IsShuttingDown reports whether ShuttingDown has run.
func (browser *Browser) IsShuttingDown() bool {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	return browser.shuttingDown
}
