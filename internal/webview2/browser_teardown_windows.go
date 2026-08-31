//go:build windows

package webview2

// Teardown closes the controller before dropping the three COM references a live
// Browser owns. Each drop is independent so a panic cannot strand another
// reference (issue #63). Split from browser_windows.go, whose Embed is the half
// that acquires what this half releases.

// ShuttingDown closes the controller and drops the browser's references.
//
// Normal teardown is called from WM_DESTROY while the parent HWND is still alive.
// Embed also calls it after references transfer into Browser when later setup
// fails or panics, before a successfully embedded Browser reaches the host.
func (browser *Browser) ShuttingDown() {
	browser.mu.Lock()
	if browser.shuttingDown {
		browser.mu.Unlock()
		return
	}
	browser.shuttingDown = true
	if browser.shutdown != nil {
		close(browser.shutdown)
	}
	optionalHandlers := browser.optionalScriptHandlers
	browser.optionalScriptHandlers = nil
	for handler := range optionalHandlers {
		handler.abandon()
	}
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

// releaseBrowserObjects closes the controller before dropping the three COM
// references a live Browser owns, each under its own deferred call. The runtime
// requirement is Close-before-Release; the relative controller, core and
// environment release order is this package's implementation seam.
//
// The separate defers are load-bearing, not style. ShuttingDown runs inside the
// panic-recovering window procedure, and its shuttingDown guard makes a retry a
// no-op, so a panic partway through the release sequence would strand whatever
// had not yet been dropped (issue #63). A panic inside one deferred call still
// runs the remaining defers; a single wrapping closure would skip the rest of
// itself. Registering Close last makes it run first.
//
// Nil callbacks cover defensive and test-only partial states. A Browser that
// receives transferred Embed ownership normally owns controller, core and
// environment together.
//
// Callbacks keep the order and panic-independence testable without a live runtime
// (docs/decisions/0006); those tests do not establish runtime release semantics.
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
