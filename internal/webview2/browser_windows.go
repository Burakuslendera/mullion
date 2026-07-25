//go:build windows

package webview2

// The Browser's lifecycle: the type itself, embedding, and the report channels
// the rest of the family speaks through. Event registration lives in
// browser_events_windows.go, teardown in browser_teardown_windows.go, and the
// surface methods a host calls on an embedded control in
// browser_surface_windows.go.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Browser is one WebView2 control embedded in a host window.
//
// It owns the environment, the controller and the CoreWebView2 behind them, and
// it turns the six COM events the host cares about into plain Go callbacks.
//
// A Browser is bound to the thread that called Embed: WebView2 requires a
// single-threaded apartment and delivers every event on that thread's message
// loop. The host already locks its OS thread and pumps the loop, so callbacks
// arrive there and may touch the window directly.
type Browser struct {
	// Callbacks. Set them before Embed; they are registered during Embed and
	// must not change afterwards.
	MessageCallback              func(message string, source string, sender *ICoreWebView2)
	WebResourceRequestedCallback func(request *ICoreWebView2WebResourceRequest, args *ICoreWebView2WebResourceRequestedEventArgs)
	// NavigationStartingCallback fires when a top-level navigation begins.
	// navigationID is the runtime's identity for it; the matching completion
	// reports the same id, which is what lets the host attribute completions
	// to the navigation that caused them (decisions/0021). A redirect fires
	// this again with the same id and isRedirected set. Returning true cancels
	// the navigation - the runtime abandons it and the current document stays;
	// that is the navigation-cancel gate (decisions/0023).
	NavigationStartingCallback func(uri string, navigationID uint64, isUserInitiated bool, isRedirected bool) bool
	// NavigationCancelledCallback fires after a navigation the callback above
	// asked to cancel has actually been cancelled - put_Cancel returned success.
	// It is where a host commits to the cancel: remembering the id so the
	// resulting completion is not read as a load failure, and handing the target
	// somewhere else. Doing that work from the callback above instead would
	// commit to a cancel that may not have taken, which is issue #73: the
	// document loads anyway, the target opens twice, and the completion of a
	// navigation that succeeded is consumed as though it had been abandoned.
	// The split mirrors the PutHandled guard on NewWindowRequested
	// (decisions/0022), which has always worked this way.
	NavigationCancelledCallback func(uri string, navigationID uint64, isUserInitiated bool)
	NavigationCompletedCallback func(success bool, status WebErrorStatus, navigationID uint64)
	ProcessFailedCallback       func(kind ProcessFailedKind)
	// NewWindowRequestedCallback fires when content asks for a new window
	// (window.open, a target=_blank link). The runtime's default new window is
	// always suppressed first; the host decides what to do with the URI - a
	// single-window host routes it to the system browser (issue #6). isUserInitiated
	// counts host-API-driven opens as true too, as with navigation starting.
	NewWindowRequestedCallback func(uri string, isUserInitiated bool)
	ErrorCallback              func(err error)
	// WarningCallback receives conditions the browser tolerates by design - an
	// older runtime answering E_NOINTERFACE for an optional interface - as
	// opposed to ErrorCallback's real failures. Splitting the channels is what
	// lets the host keep its severity contract: ERROR is reserved for events
	// that need attention (issue #32).
	WarningCallback func(err error)

	// UserDataFolder is where WebView2 keeps its profile. Empty means "a folder
	// under the user's local app data, named after the executable".
	UserDataFolder string
	// AdditionalBrowserArguments is passed to the Chromium command line. This is
	// the main performance lever the runtime exposes.
	AdditionalBrowserArguments string

	mu           sync.Mutex
	environment  *Environment
	controller   *ICoreWebView2Controller
	core         *ICoreWebView2
	shuttingDown bool
}

// New returns an unembedded Browser.
func New() *Browser { return &Browser{} }

func (browser *Browser) reportError(err error) {
	if err != nil && browser.ErrorCallback != nil {
		browser.ErrorCallback(err)
	}
}

func (browser *Browser) reportWarning(err error) {
	if err != nil && browser.WarningCallback != nil {
		browser.WarningCallback(err)
	}
}

// Embed creates the WebView2 environment and controller as children of parent.
//
// It blocks: environment and controller creation are asynchronous COM
// operations whose completion handlers are delivered on the message loop, and
// the loader pumps the loop until they land. On a warm runtime this takes a few
// hundred milliseconds; on a cold one, longer.
func (browser *Browser) Embed(parent uintptr) error {
	userData, err := browser.userDataFolder()
	if err != nil {
		browser.reportError(err)
		return err
	}

	environment, err := CreateEnvironment(userData, browser.AdditionalBrowserArguments)
	if err != nil {
		browser.reportError(err)
		return err
	}

	controllerUnknown, err := environment.CreateController(windows.Handle(parent))
	if err != nil {
		environment.Release()
		browser.reportError(err)
		return err
	}

	// CreateController hands back the ICoreWebView2Controller interface pointer;
	// it is typed as IUnknown only because the loader does not depend on the
	// interface definitions. The pointer identity is the same.
	controller := (*ICoreWebView2Controller)(unsafe.Pointer(controllerUnknown))

	core, err := controller.GetCoreWebView2()
	if err != nil {
		// CreateController handed us an owned reference. Close and release it
		// before bailing, or it is orphaned: browser.controller is not assigned
		// until below, so ShuttingDown could never reclaim it.
		if closeErr := controller.Close(); closeErr != nil {
			browser.reportError(closeErr)
		}
		asUnknown(controller).Release()
		environment.Release()
		browser.reportError(err)
		return err
	}

	browser.mu.Lock()
	browser.environment = environment
	browser.controller = controller
	browser.core = core
	browser.mu.Unlock()

	browser.applyBoundsPolicy()

	return browser.registerEventsOrTearDown(browser.registerEvents)
}

// registerEventsOrTearDown runs register and, on failure, tears the browser down
// before returning the error.
//
// By this point Embed has stored the environment, controller and core on the
// browser, and the caller (host.createWebView) assigns host.browser only after
// Embed returns nil. So if event registration fails here, nothing else will ever
// release those three references: host.browser stays nil, and ShuttingDown - the
// only code that releases them - never runs on this Browser. This releases them
// itself. ShuttingDown is idempotent and nils the fields, and its controller.Close
// also drops any handler registered before the failing one.
//
// register is a parameter so the failure path is unit-testable without a live
// runtime: the real release counts need Windows and a runtime, but the contract
// "a registration failure tears the browser down" is checkable headlessly.
func (browser *Browser) registerEventsOrTearDown(register func() error) error {
	if err := register(); err != nil {
		browser.reportError(err)
		browser.ShuttingDown()
		return err
	}
	return nil
}

// applyBoundsPolicy pins the coordinate system the host expects.
//
// The host measures its client area in physical pixels and hands those to
// PutBounds, and it handles WM_DPICHANGED itself. So the controller must be told
// to read bounds as raw pixels and to keep its hands off the rasterisation scale
// when the window crosses a monitor boundary - otherwise two independent pieces
// of code would react to the same DPI change and the scale would compound.
//
// Both settings live on ICoreWebView2Controller3. An older runtime simply does
// not have it: that is a warning, not a failure, because the defaults are close
// enough to keep a single-DPI setup working. The query miss therefore goes to
// WarningCallback - the same severity SetRasterizationScale's caller applies to
// the identical miss - while the Put* calls, which can only fail on a runtime
// that does have the interface, stay real errors (issue #32).
func (browser *Browser) applyBoundsPolicy() {
	controller3, err := browser.Controller().QueryController3()
	if err != nil {
		browser.reportWarning(err)
		return
	}
	defer controller3.Release()

	if err := controller3.PutBoundsMode(BoundsModeUseRawPixels); err != nil {
		browser.reportError(err)
	}
	if err := controller3.PutShouldDetectMonitorScaleChanges(false); err != nil {
		browser.reportError(err)
	}
}

// userDataFolder resolves where WebView2 keeps its profile.
//
// Leaving this empty is a trap: WebView2 then falls back to a folder next to the
// executable, which fails outright for anything installed under Program Files.
// Defaulting to the user's local app data means an application does not have to
// know this.
func (browser *Browser) userDataFolder() (string, error) {
	if browser.UserDataFolder != "" {
		return browser.UserDataFolder, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	name := "webview2"
	if executable, err := os.Executable(); err == nil {
		name = strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
	}
	folder := filepath.Join(base, name, "WebView2")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	return folder, nil
}

// CoreWebView2 returns the underlying ICoreWebView2, or nil before Embed.
func (browser *Browser) CoreWebView2() *ICoreWebView2 {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	return browser.core
}

// Controller returns the underlying ICoreWebView2Controller, or nil before Embed.
func (browser *Browser) Controller() *ICoreWebView2Controller {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	return browser.controller
}

// Environment returns the underlying ICoreWebView2Environment, or nil before Embed.
func (browser *Browser) Environment() *ICoreWebView2Environment {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	return browser.environment.Interface()
}
