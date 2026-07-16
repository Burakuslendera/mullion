//go:build windows

package webview2

import (
	"errors"
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
// it turns the four COM events the host cares about into plain Go callbacks.
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
	NavigationCompletedCallback  func(success bool, status WebErrorStatus)
	ProcessFailedCallback        func(kind ProcessFailedKind)
	ErrorCallback                func(err error)

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

	if err := browser.registerEvents(); err != nil {
		browser.reportError(err)
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
// enough to keep a single-DPI setup working.
func (browser *Browser) applyBoundsPolicy() {
	controller3, err := browser.Controller().QueryController3()
	if err != nil {
		browser.reportError(err)
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

// SetRasterizationScale updates the scale WebView2 rasterizes content at - the
// devicePixelRatio the frontend renders against.
//
// applyBoundsPolicy turns the runtime's own monitor-scale detection off, so the
// runtime never revises this scale on its own. After the host moves the window to
// a monitor with a different DPI it must set the new scale here, or the content
// keeps rendering at the scale of the monitor the controller was created on - too
// large on a lower-DPI monitor, too small on a higher one. The matching bounds are
// fed separately, in raw pixels, by the host's own DPI handling; the two do not
// compound because only the host drives either.
//
// The scale lives on ICoreWebView2Controller3. An older runtime without it is a
// warning to the caller, not a crash, exactly as in applyBoundsPolicy.
func (browser *Browser) SetRasterizationScale(scale float64) error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: controller unavailable")
	}
	controller3, err := controller.QueryController3()
	if err != nil {
		return err
	}
	defer controller3.Release()
	return controller3.PutRasterizationScale(scale)
}

// addEvent registers one handler and immediately drops the package's reference
// to it.
//
// add_* takes its own reference, so the object survives this release and lives
// until the WebView is torn down. Holding on to our reference as well would leak
// one COM object per handler; releasing before add_* would hand the runtime a
// freed object.
func addEvent(handler unsafe.Pointer, register func(unsafe.Pointer) (EventRegistrationToken, error)) error {
	_, err := register(handler)
	ReleaseHandler(handler)
	return err
}

func (browser *Browser) registerEvents() error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: core webview unavailable")
	}

	if err := addEvent(NewWebMessageReceivedHandler(func(sender *ICoreWebView2, args *ICoreWebView2WebMessageReceivedEventArgs) {
		if browser.MessageCallback == nil {
			return
		}
		message, err := args.TryGetWebMessageAsString()
		if err != nil {
			// A frontend may post a structured message rather than a string.
			// The bridge protocol here is string-based, so dropping it is
			// correct - there is nothing the host could do with it.
			return
		}
		// GetSource is the URI of the document that posted the message; the host
		// uses it to keep the bridge pinned to the trusted origin.
		source, _ := args.GetSource()
		browser.MessageCallback(message, source, sender)
	}), core.AddWebMessageReceived); err != nil {
		return err
	}

	if err := addEvent(NewWebResourceRequestedHandler(func(_ *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) {
		if browser.WebResourceRequestedCallback == nil || args == nil {
			return
		}
		request, err := args.GetRequest()
		if err != nil {
			browser.reportError(err)
			return
		}
		browser.WebResourceRequestedCallback(request, args)
	}), core.AddWebResourceRequested); err != nil {
		return err
	}

	if err := addEvent(NewNavigationCompletedHandler(func(_ *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) {
		if browser.NavigationCompletedCallback == nil {
			return
		}
		success, _ := args.GetIsSuccess()
		status, _ := args.GetWebErrorStatus()
		browser.NavigationCompletedCallback(success, status)
	}), core.AddNavigationCompleted); err != nil {
		return err
	}

	return addEvent(NewProcessFailedHandler(func(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) {
		if browser.ProcessFailedCallback == nil {
			return
		}
		kind, _ := args.GetProcessFailedKind()
		browser.ProcessFailedCallback(kind)
	}), core.AddProcessFailed)
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

// Navigate loads a URL.
func (browser *Browser) Navigate(url string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: navigate before embed")
	}
	err := core.Navigate(url)
	browser.reportError(err)
	return err
}

// Init registers a script to run in every document before any page script.
func (browser *Browser) Init(script string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: init before embed")
	}
	err := core.AddScriptToExecuteOnDocumentCreated(script, nil)
	browser.reportError(err)
	return err
}

// Eval runs a script in the current document.
func (browser *Browser) Eval(script string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: eval before embed")
	}
	err := core.ExecuteScript(script, nil)
	browser.reportError(err)
	return err
}

// PostWebMessageAsString sends a string to the frontend's
// chrome.webview message listener.
func (browser *Browser) PostWebMessageAsString(message string) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: post before embed")
	}
	return core.PostWebMessageAsString(message)
}

// Show makes the control visible.
//
// Showing the host window is not enough: the controller has its own visibility,
// and a controller left invisible renders nothing into a perfectly visible
// window.
func (browser *Browser) Show() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: show before embed")
	}
	err := controller.PutIsVisible(true)
	browser.reportError(err)
	return err
}

// Hide makes the control invisible.
func (browser *Browser) Hide() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: hide before embed")
	}
	err := controller.PutIsVisible(false)
	browser.reportError(err)
	return err
}

// PutBounds resizes the control. Bounds are physical pixels; see
// applyBoundsPolicy.
func (browser *Browser) PutBounds(bounds Rect) error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: bounds before embed")
	}
	return controller.PutBounds(bounds)
}

// GetBounds reads back the control's rectangle.
func (browser *Browser) GetBounds() (Rect, error) {
	controller := browser.Controller()
	if controller == nil {
		return Rect{}, errors.New("webview2: bounds before embed")
	}
	return controller.GetBounds()
}

// NotifyParentWindowPositionChanged tells the control its host moved. Without
// it, anything the control positions in screen coordinates - the caret, an
// autofill popup - stays where the window used to be.
func (browser *Browser) NotifyParentWindowPositionChanged() error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: notify before embed")
	}
	return controller.NotifyParentWindowPositionChanged()
}

// SetBackgroundColour paints behind the page. It is what the user sees between
// the window appearing and the first frame being rendered, and during a resize.
func (browser *Browser) SetBackgroundColour(r, g, b, a uint8) error {
	controller := browser.Controller()
	if controller == nil {
		return errors.New("webview2: background before embed")
	}
	controller2, err := controller.QueryController2()
	if err != nil {
		return err
	}
	defer controller2.Release()
	return controller2.PutDefaultBackgroundColor(Color{A: a, R: r, G: g, B: b})
}

// Settings returns the base settings object.
func (browser *Browser) Settings() (*ICoreWebView2Settings, error) {
	core := browser.CoreWebView2()
	if core == nil {
		return nil, errors.New("webview2: settings before embed")
	}
	return core.GetSettings()
}

// AddWebResourceRequestedFilter subscribes the resource handler to a URI
// pattern. Without a filter the event never fires.
func (browser *Browser) AddWebResourceRequestedFilter(uri string, context WebResourceContext) error {
	core := browser.CoreWebView2()
	if core == nil {
		return errors.New("webview2: filter before embed")
	}
	return core.AddWebResourceRequestedFilter(uri, context)
}

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

	if controller != nil {
		if err := controller.Close(); err != nil {
			browser.reportError(err)
		}
		asUnknown(controller).Release()
	}
	// GetCoreWebView2 returned a reference this Browser owns (see its doc in
	// interfaces_windows.go). Closing the controller does not drop it, so release
	// it explicitly - otherwise one ICoreWebView2 leaks on every teardown, which
	// grows without bound in a process that opens and closes many windows.
	if core != nil {
		asUnknown(core).Release()
	}
	if environment != nil {
		environment.Release()
	}
}

// IsShuttingDown reports whether ShuttingDown has run.
func (browser *Browser) IsShuttingDown() bool {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	return browser.shuttingDown
}
