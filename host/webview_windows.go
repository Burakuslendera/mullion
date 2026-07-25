//go:build windows

package host

import (
	"errors"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

func (host *Host) ensureWebView(source string) error {
	return host.ensureWebViewWith(source, host.createWebView)
}

// ensureWebViewWith is ensureWebView with the embed injected, so the
// single-flight contract is unit-testable without a live runtime (the same
// seam registerEventsOrTearDown and navigateOrTearDown use).
//
// Embed pumps the message loop, so a message dispatched mid-embed can land
// right back here with host.browser still nil. The webViewEmbedding flag makes
// that re-entrant call fail instead of starting a second embed - two browsers
// would race for the one host.browser commit and the loser would leak, browser
// process and all (issue #23, decision 0016). A destroyed window refuses too:
// there is nothing left to embed into.
func (host *Host) ensureWebViewWith(source string, create func() error) error {
	if host.browser != nil {
		return nil
	}
	if host.windowDestroyed {
		err := errors.New("window already destroyed")
		host.log.Warn("mullion: webview create refused, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return err
	}
	if host.webViewEmbedding {
		err := errors.New("webview embed already in flight")
		host.log.Warn("mullion: webview create refused, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return err
	}
	host.webViewEmbedding = true
	defer func() { host.webViewEmbedding = false }()

	host.log.Debug("mullion: webview create requested, source=" + logsafe.Message(source))
	// Before create, because create is what brings the event handlers into
	// existence and a handler that panics before the hook is installed reports to
	// stderr instead of this host's logger. Installed here rather than inside
	// createWebView so the wiring sits on the seam the suite can drive without a
	// runtime: everything below this line needs a live WebView2.
	host.installHandlerPanicLogging()
	if err := create(); err != nil {
		host.log.Error("mullion: webview2 embed failed, source=" + logsafe.Message(source) + ", reason=" + logsafe.Reason(err))
		return err
	}
	return nil
}

func (host *Host) isWebViewDeferred() bool {
	return host.config.StartHidden && host.browser == nil
}

// createWebView embeds the control and prepares it for the first navigation.
//
// The order below is a contract, not a style choice. Settings, the injected
// scripts and non-client region support must all be applied after Embed and
// before the first Navigate: WebView2 applies several of them "on the next
// navigation", so doing any of it later either has no effect on the first paint
// or forces a second navigation, which shows up as a reload flash.
func (host *Host) createWebView() error {
	host.log.Debug("mullion: webview2 instance requested")
	browser := webview2.New()
	browser.UserDataFolder = host.config.UserDataFolder
	browser.AdditionalBrowserArguments = host.config.BrowserArguments

	browser.ErrorCallback = func(err error) {
		host.log.Error("mullion: webview2 runtime error, reason=" + logsafe.Reason(err))
	}
	browser.WarningCallback = func(err error) {
		// Tolerated-by-design conditions - an older runtime without an optional
		// interface - land at Warn, keeping ERROR meaningful (issue #32).
		host.log.Warn("mullion: webview2 runtime warning, reason=" + logsafe.Reason(err))
	}
	browser.MessageCallback = func(message string, source string, sender *webview2.ICoreWebView2) {
		if !host.config.messageSourceAllowed(source) && !host.errorSurfaceMessageAllowed(source) {
			// The bridge is injected into every document, so a top-level navigation
			// away from the frontend must not be able to drive Config.Bridge. Drop
			// the message silently - a foreign origin gets no reply to correlate.
			host.logRejectedWebMessage(source)
			return
		}
		// A data: source (the error surface, or a hostile data: iframe) is allowed
		// only the reserved window controls, never Config.Bridge; the trusted
		// origin gets full access (decisions/0014).
		response := host.handleWebMessage(message, host.config.messageSourceTrusted(source))
		if response == "" {
			return
		}
		if sender == nil {
			host.log.Warn("mullion: bridge response sender unavailable")
			return
		}
		if err := sender.PostWebMessageAsString(response); err != nil {
			host.log.Warn("mullion: bridge response post failed, reason=" + logsafe.Reason(err))
		}
	}
	if host.config.URL == "" {
		browser.WebResourceRequestedCallback = func(request *webview2.ICoreWebView2WebResourceRequest, args *webview2.ICoreWebView2WebResourceRequestedEventArgs) {
			host.assets.webResourceRequested(request, args, browser.Environment())
		}
	}
	browser.NavigationStartingCallback = func(uri string, navigationID uint64, isUserInitiated bool, isRedirected bool) bool {
		host.logNavigationStarting(uri, navigationID, isUserInitiated, isRedirected)
		return host.noteAndGateNavigation(uri, navigationID, isUserInitiated)
	}
	browser.NavigationCompletedCallback = func(success bool, status webview2.WebErrorStatus, navigationID uint64) {
		if host.noteGateCancelledOutcome(success, status, navigationID) {
			// The PinNavigationToOrigin gate cancelled this navigation: nothing
			// committed, the current document stays, and the target was routed to
			// the system browser. It must not be reported as a failure, resynced,
			// re-evaluated or fed to the error-surface machine (decisions/0023).
			return
		}
		// A failure is handed down unlogged: which line it deserves, and at what
		// level, is what handleNavigationOutcome's machine decides, and it
		// reports the failure itself once it knows (issue #79, decisions/0026).
		// A generic warning here could only guess, and it guessed wrong for
		// every suppression the machine owns - a benign abort, a superseded
		// surface Navigate, an absorbed straggler - which is what put
		// deliberately suppressed events into the warn count. The gate's cancel
		// above escaped it only by being resolved before this line.
		host.log.Debug("mullion: navigation completed, id=" + formatUint64(navigationID))
		host.syncWebViewBounds("navigation_completed")
		host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
		host.handleNavigationOutcome(browser, success, status, navigationID)
	}
	browser.ProcessFailedCallback = func(kind webview2.ProcessFailedKind) {
		host.log.Error("mullion: webview2 process failed, kind=" + formatInt32(int32(kind)))
	}
	browser.NewWindowRequestedCallback = func(uri string, isUserInitiated bool) {
		host.routeNewWindow(uri, isUserInitiated)
	}

	host.log.Debug("mullion: webview2 embed requested")
	if err := browser.Embed(uintptr(host.window())); err != nil {
		return errors.Join(errors.New("embed webview2"), err)
	}
	if err := host.commitEmbeddedBrowser(browser); err != nil {
		return err
	}
	host.log.Debug("mullion: webview2 embedded")

	background := host.config.BackgroundColour
	host.warnIf("background colour", browser.SetBackgroundColour(background.R, background.G, background.B, background.A))
	host.applyWebViewHardening(browser)
	// Pin the content scale to the window's monitor up front. The runtime picks a
	// scale when the controller is created and then never revises it (monitor-scale
	// detection is off), so setting it here makes the first paint correct even when
	// the window opened on a non-primary monitor at a different DPI.
	host.syncRasterizationScale("embed", dpiForWindow(host.window()))
	host.syncWebViewBounds("embed")

	if host.config.URL == "" {
		host.log.Debug("mullion: webresource filter registered")
		host.warnIf("web resource filter", browser.AddWebResourceRequestedFilter(host.config.origin()+"/*", webview2.WebResourceContextAll))
		host.log.Debug("mullion: asset serving ready, source=embedded-fs")
	} else {
		// Config.URL is set: the caller serves the origin, so there is nothing to
		// intercept. The injected scripts below still run - they are per-navigation
		// and origin-independent - so the bridge and window controls work either way.
		host.log.Debug("mullion: asset serving skipped, source=external-url")
	}

	// The bridge script installs the namespace the other three scripts use, so
	// it must be injected first.
	host.warnIf("bridge script", browser.Init(host.js.bridge))
	host.warnIf("diagnostics script", browser.Init(host.js.diagnostics))
	host.warnIf("drag script", browser.Init(host.js.drag))
	host.warnIf("resize script", browser.Init(host.js.resize))
	host.log.Debug("mullion: injected scripts registered")

	host.applyTabStripStartup(browser)
	host.log.Debug("mullion: navigate requested")
	host.startRenderWatchdog()
	return host.navigateOrTearDown(func() error {
		return browser.Navigate(host.config.startURL())
	})
}

// commitEmbeddedBrowser assigns the freshly embedded browser - unless the
// window was destroyed while Embed pumped the message loop.
//
// A WM_DESTROY dispatched inside the embed pump finds host.browser still nil
// and skips ShuttingDown; committing afterwards would hand host.browser a
// browser whose HWND is already gone and whose teardown has already passed -
// nothing would ever release it (issue #23, defect 2). The browser is torn
// down here instead. The HWND is no longer alive, so the controller Close may
// report a failure the error callback logs; a best-effort teardown still beats
// a stranded browser process. Split from createWebView so the contract is
// unit-testable without a runtime.
func (host *Host) commitEmbeddedBrowser(browser *webview2.Browser) error {
	if host.windowDestroyed {
		browser.ShuttingDown()
		return errors.New("window destroyed during webview embed")
	}
	host.browser = browser
	return nil
}

// navigateOrTearDown starts the first navigation and, on failure, undoes the
// embed commit before returning the error.
//
// By this point createWebView has assigned host.browser, and the only code that
// releases the browser's COM references - Browser.ShuttingDown - runs from the
// WM_DESTROY case of the window procedure. On the initial embed path a Navigate
// failure propagates out of Run before the message loop ever starts, so
// WM_DESTROY is never dispatched, Run's deferred CoUninitialize executes with
// the environment, controller and core still referenced, and the WebView2
// browser child process is orphaned. Tearing down here - watchdog stopped,
// host.browser uncommitted, ShuttingDown while the HWND is still alive - closes
// that path, and leaves ensureWebView free to embed a fresh browser if the
// caller retries (a nil-ed host.browser is what its guard checks).
//
// navigate is a parameter so the failure contract is unit-testable without a
// live runtime, exactly like registerEventsOrTearDown on the in-Embed error
// path (internal/webview2/browser_windows.go): the real release counts need a
// runtime, but "a Navigate failure uncommits and tears down" is checkable
// headlessly. The browser is read from host.browser rather than taken as a
// parameter, so the committed field is the single source of truth: a second
// caller could otherwise tear down one browser while uncommitting another.
func (host *Host) navigateOrTearDown(navigate func() error) error {
	if err := navigate(); err != nil {
		browser := host.browser
		host.stopRenderWatchdog()
		host.browser = nil
		if browser != nil {
			browser.ShuttingDown()
		}
		return err
	}
	return nil
}
