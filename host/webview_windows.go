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
// seam registerEventsOrTearDown and committedBrowserStepOrTearDown use).
//
// Embed pumps the message loop, so a message dispatched mid-embed can land
// right back here with host.browser still nil. The webViewEmbedding flag makes
// that re-entrant call fail instead of starting a second embed - two browsers
// would race for the one host.browser commit and the loser would leak, browser
// process and all (issue #23, decision 0016). A destroyed window refuses too:
// there is nothing left to embed into.
func (host *Host) ensureWebViewWith(source string, create func() error) error {
	// A committed Browser is usable only after create returns. Required script
	// completion pumps the STA queue after commit, so a re-entrant Show must stay
	// behind webViewEmbedding instead of exposing a half-configured controller.
	if host.browser != nil && !host.webViewEmbedding {
		return nil
	}
	// Returned errors are not reported here: the Run or Show path that can
	// return them owns the one terminal diagnostic (decision 0038). Logging in
	// both layers makes one failed filter registration look like two failures.
	if host.windowDestroyed {
		return errors.New("window already destroyed")
	}
	if host.webViewEmbedding {
		return errors.New("webview embed already in flight")
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
		return err
	}
	return nil
}

func (host *Host) isWebViewDeferred() bool {
	return host.config.StartHidden && host.browser == nil
}

// newWebViewBrowser is the single production constructor used by createWebView.
// It is headless so the suite can verify the actual Browser callback field,
// rather than only the callback body: bypassing this constructor would otherwise
// silently leave WebMessageReceived disconnected while helper tests stayed green.
func (host *Host) newWebViewBrowser() *webview2.Browser {
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
	browser.MessageCallback = host.webMessageCallback()
	browser.NavigationStartingCallback = func(observation webview2.NavigationStartingObservation) bool {
		// Resolve the document boundary before any getter diagnostic can re-enter
		// through the embedder's Logger and dispatch an empty-source message.
		// Preserve the getter tag through the whole transition: zero is a value,
		// not a substitute for an unavailable navigation identity.
		cancel := host.noteAndGateNavigationKnown(
			observation.URI,
			observation.URIErr == nil,
			observation.NavigationIDErr == nil,
			observation.NavigationID,
		)
		host.reportEventGetterFailure("NavigationStarting", "GetUri", observation.URIErr)
		host.reportEventGetterFailure("NavigationStarting", "GetNavigationID", observation.NavigationIDErr)
		host.reportEventGetterFailure("NavigationStarting", "GetIsUserInitiated", observation.IsUserInitiatedErr)
		host.reportEventGetterFailure("NavigationStarting", "GetIsRedirected", observation.IsRedirectedErr)
		if observation.URIErr == nil && observation.NavigationIDErr == nil &&
			observation.IsUserInitiatedErr == nil && observation.IsRedirectedErr == nil {
			host.logNavigationStarting(
				observation.URI,
				observation.NavigationID,
				observation.IsUserInitiated,
				observation.IsRedirected,
			)
		}
		return cancel
	}
	browser.NavigationCancelledCallback = func(observation webview2.NavigationStartingObservation) {
		identity := navigationIdentity{
			known: observation.NavigationIDErr == nil,
			value: observation.NavigationID,
		}
		if observation.URIErr != nil || observation.IsUserInitiatedErr != nil {
			host.rememberCancelledNavigationObserved(identity)
			return
		}
		host.noteNavigationCancelledObserved(
			observation.URI,
			identity,
			observation.IsUserInitiated,
		)
	}
	browser.NavigationCompletedCallback = func(observation webview2.NavigationCompletedObservation) {
		hasGetterFailure := observation.IsSuccessErr != nil ||
			observation.WebErrorStatusErr != nil ||
			observation.NavigationIDErr != nil
		if hasGetterFailure {
			action := host.noteUnclassifiableNavigationCompletion(
				observation.IsSuccessErr == nil,
				observation.IsSuccess,
				observation.WebErrorStatusErr == nil,
				observation.WebErrorStatus,
				observation.NavigationIDErr == nil,
				observation.NavigationID,
			)
			host.reportEventGetterFailure("NavigationCompleted", "GetIsSuccess", observation.IsSuccessErr)
			host.reportEventGetterFailure("NavigationCompleted", "GetWebErrorStatus", observation.WebErrorStatusErr)
			host.reportEventGetterFailure("NavigationCompleted", "GetNavigationID", observation.NavigationIDErr)
			if action != unclassifiableCompletionSucceeded {
				return
			}
			if observation.NavigationIDErr == nil {
				host.log.Debug("mullion: navigation completed, id=" + formatUint64(observation.NavigationID))
			} else {
				host.log.Debug("mullion: navigation completed, id=unavailable")
			}
			host.syncWebViewBounds("navigation_completed")
			host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
			return
		}
		if host.noteGateCancelledOutcome(
			observation.IsSuccess,
			observation.WebErrorStatus,
			observation.NavigationID,
		) {
			return
		}
		// Classification creates only a revocable plan. Logger and COM calls
		// below may pump the STA loop; a nested start, success, or
		// unclassifiable completion invalidates this token so the outer callback
		// cannot issue a stale fallback navigation (issue #86).
		errorSurfacePlan := host.planNavigationOutcome(
			observation.IsSuccess,
			observation.WebErrorStatus,
			observation.NavigationID,
		)
		host.log.Debug("mullion: navigation completed, id=" + formatUint64(observation.NavigationID))
		host.syncWebViewBounds("navigation_completed")
		host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
		if errorSurfacePlan != noErrorSurfacePlan {
			host.showErrorSurface(browser, errorSurfacePlan)
		}
	}
	browser.ProcessFailedCallback = func(observation webview2.ProcessFailedObservation) {
		if observation.KindErr != nil {
			host.reportEventGetterFailure("ProcessFailed", "GetProcessFailedKind", observation.KindErr)
			return
		}
		host.log.Error("mullion: webview2 process failed, kind=" + formatInt32(int32(observation.Kind)))
	}
	browser.NewWindowRequestedCallback = func(observation webview2.NewWindowRequestedObservation) {
		host.reportEventGetterFailure("NewWindowRequested", "GetUri", observation.URIErr)
		host.reportEventGetterFailure("NewWindowRequested", "GetIsUserInitiated", observation.IsUserInitiatedErr)
		if observation.URIErr != nil || observation.IsUserInitiatedErr != nil {
			return
		}
		host.routeNewWindow(observation.URI, observation.IsUserInitiated)
	}
	return browser
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
	browser := host.newWebViewBrowser()
	if host.source.embedded {
		browser.WebResourceRequestedCallback = func(request *webview2.ICoreWebView2WebResourceRequest, args *webview2.ICoreWebView2WebResourceRequestedEventArgs) {
			host.assets.webResourceRequested(request, args, browser.Environment())
		}
	}

	host.log.Debug("mullion: webview2 embed requested")
	if err := browser.Embed(uintptr(host.window())); err != nil {
		return errors.Join(errors.New("embed webview2"), err)
	}
	if err := host.commitEmbeddedBrowser(browser); err != nil {
		return err
	}
	if err := host.registerAssetFilterOrTearDown(func(pattern string, context webview2.WebResourceContext) error {
		return browser.AddWebResourceRequestedFilter(pattern, context)
	}); err != nil {
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

	return host.startWebViewFirstNavigation(
		browser,
		browser.RegisterDocumentCreatedScripts,
		host.applyTabStripStartup,
		host.startRenderWatchdog,
		func() error { return browser.Navigate(host.source.startURL) },
	)
}

// startWebViewFirstNavigation is the production post-Embed startup seam. Its
// collaborators are effects already owned by createWebView; retaining them as
// arguments lets headless tests execute the same failure and ordering path
// without a Runtime, HWND, or native message pump.
func (host *Host) startWebViewFirstNavigation(
	browser *webview2.Browser,
	register func(...string) error,
	applyTabStrip func(*webview2.Browser) error,
	startWatchdog func(),
	navigate func() error,
) error {
	if err := host.registerRequiredDocumentCreatedScripts(browser, register); err != nil {
		return err
	}

	if err := host.committedBrowserStepOrTearDown(func() error {
		return applyTabStrip(browser)
	}); err != nil {
		return err
	}
	if err := host.committedBrowserStepOrTearDown(func() error {
		return host.requireCurrentBrowser(browser)
	}); err != nil {
		return err
	}
	if host.source.embedded {
		host.log.Debug("mullion: asset serving ready, source=embedded-fs")
		if err := host.committedBrowserStepOrTearDown(func() error {
			return host.requireCurrentBrowser(browser)
		}); err != nil {
			return err
		}
	}
	host.log.Debug("mullion: injected scripts registered")
	if err := host.committedBrowserStepOrTearDown(func() error {
		return host.requireCurrentBrowser(browser)
	}); err != nil {
		return err
	}
	startWatchdog()
	if err := host.committedBrowserStepOrTearDown(func() error {
		return host.requireCurrentBrowser(browser)
	}); err != nil {
		return err
	}
	if err := host.committedBrowserStepOrTearDown(navigate); err != nil {
		return err
	}
	// Navigate may dispatch teardown before returning success. Report the
	// request only after both the effect and its resulting lifecycle state are
	// committed, then re-check after the user Logger callback re-enters.
	if err := host.committedBrowserStepOrTearDown(func() error {
		return host.requireCurrentBrowser(browser)
	}); err != nil {
		return err
	}
	host.log.Debug("mullion: navigate requested")
	return host.committedBrowserStepOrTearDown(func() error {
		return host.requireCurrentBrowser(browser)
	})
}

// webMessageCallback is the source-admission body installed by
// newWebViewBrowser, the production constructor used by createWebView. Keeping
// classification in this headless closure lets tests drive trusted and fallback
// messages without a runtime while also asserting the Browser field is wired.
func (host *Host) webMessageCallback() func(webview2.WebMessageObservation, *webview2.ICoreWebView2) {
	return func(observation webview2.WebMessageObservation, sender *webview2.ICoreWebView2) {
		if observation.SourceErr != nil {
			host.reportEventGetterFailure("WebMessageReceived", "GetSource", observation.SourceErr)
			return
		}
		source := observation.Source
		if !host.source.messageSourceAllowed(source) && !host.errorSurfaceMessageAllowed(source) {
			// A top-level navigation away from the frontend must not be able to
			// drive Config.Bridge. A foreign origin gets no reply to correlate.
			host.logRejectedWebMessage(source)
			return
		}
		response := host.handleWebMessage(
			observation.Message,
			host.source.messageSourceTrusted(source),
		)
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
}

func (host *Host) reportEventGetterFailure(event, getter string, err error) {
	if err == nil {
		return
	}
	host.log.Error("mullion: webview2 event getter failed, event=" + event +
		", getter=" + getter + ", reason=" + logsafe.Reason(err))
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

// registerAssetFilterOrTearDown makes the embedded request filter a mandatory
// post-commit step. A failure uncommits and shuts down the browser before any
// settings, scripts, watchdog or navigation can run.
func (host *Host) registerAssetFilterOrTearDown(register func(string, webview2.WebResourceContext) error) error {
	if !host.source.embedded {
		host.log.Debug("mullion: asset serving skipped, source=external-url")
		return nil
	}
	err := host.committedBrowserStepOrTearDown(func() error {
		return register(host.source.filterPattern, webview2.WebResourceContextAll)
	})
	if err != nil {
		return errors.Join(errors.New("register web resource filter"), err)
	}
	host.log.Debug("mullion: webresource filter registered")
	return nil
}

// committedBrowserStepOrTearDown runs a required operation against the browser
// committed in host.browser. On failure it stops the watchdog, uncommits the
// browser and calls ShuttingDown, leaving a later sequential embed free to retry.
// The committed field is deliberately the single source of truth so a caller
// cannot tear down one browser while uncommitting another.
func (host *Host) committedBrowserStepOrTearDown(step func() error) error {
	if err := step(); err != nil {
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
