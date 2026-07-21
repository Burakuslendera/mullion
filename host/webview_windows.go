//go:build windows

package host

import (
	"errors"
	"strconv"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

func (host *Host) ensureWebView(source string) error {
	return host.ensureWebViewWith(source, host.createWebView)
}

// clampSourceForLog bounds a rejected source before it is reduced for the debug
// log: a foreign data: or blob: URI can be arbitrarily long, and the first bytes
// are what identify it. The cut can land mid-rune; logsafe's reduction tolerates
// that, and an imperfect tail beats an unbounded log line.
func clampSourceForLog(source string) string {
	const limit = 160
	if len(source) <= limit {
		return source
	}
	return source[:limit]
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
			// The debug line carries the reduced raw source because the WARN's
			// origin form collapses every schemeless value to the same ":unknown",
			// which is what made issue #56 need a live probe to diagnose.
			host.log.Warn("mullion: web message rejected, untrusted source, origin=" + logsafe.Message(urlOrigin(source)))
			host.log.Debug("mullion: web message rejected, raw source=" + logsafe.Message(clampSourceForLog(source)) + ", len=" + strconv.Itoa(len(source)))
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
	browser.NavigationCompletedCallback = func(success bool, status webview2.WebErrorStatus) {
		if !success {
			host.log.Warn("mullion: navigation failed, status=" + formatInt32(int32(status)))
		}
		host.log.Debug("mullion: navigation completed")
		host.syncWebViewBounds("navigation_completed")
		host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
		host.handleNavigationOutcome(browser, success)
	}
	browser.ProcessFailedCallback = func(kind webview2.ProcessFailedKind) {
		host.log.Error("mullion: webview2 process failed, kind=" + formatInt32(int32(kind)))
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

// handleNavigationOutcome shows mullion's own controllable surface when a
// navigation fails, so an end user is never stranded on Edge's chromeless
// network-error page - which, with the native caption removed, has no title bar and
// no visible way to minimise, maximise, close or reload (issue #3, found live in
// PR #4). The surface is a self-contained data: URL (errorpage.go); no socket is
// opened, consistent with the no-port guarantee.
//
// It runs on the UI thread from NavigationCompletedCallback, so errorPageShown needs
// no lock. The recursion guard is belt-and-braces: the fallback is a data: page that
// loads success=true and so cannot itself reach this branch, and if a future change
// made it fail, the completion would land inside the absorb window and be ignored
// rather than re-navigated (decisions/0020) - it could not loop either way. Any
// successful load re-arms the guard, so a Retry that fails again shows the surface
// again.
func (host *Host) handleNavigationOutcome(browser *webview2.Browser, success bool) {
	if !host.noteNavigationOutcome(success) {
		return
	}
	host.log.Info("mullion: navigation failed, showing fallback error surface")
	host.warnIf("error surface navigate", browser.Navigate(errorPageURL(host.config, host.config.startURL())))
	// Release the startup show gate so the surface appears now instead of after
	// Config.ShowTimeout. The render watchdog is left armed on purpose: the intended
	// frontend never rendered, so it should still fire, and a blank frontend after a
	// Retry must still be caught.
	host.requestStartupShow("navigation_failed")
}

// noteNavigationOutcome runs the error-surface bookkeeping for a completed
// navigation and reports whether the fallback surface should be navigated to
// now. It is split from handleNavigationOutcome so the state machine is
// headless-testable without a Browser.
//
// The surface is armed here, at the decision to navigate, rather than when its
// load completes: the injected diagnostics post their first messages from
// document creation, before NavigationCompleted fires, and arming late would
// reject them (the ten-in-a-row WARN flurry issue #56 was reported with). The
// cost is a window, until the surface's document commits, in which the departing
// document could post an empty-source message and be granted the reserved window
// controls; on this path that document is a failed load or mullion's own
// about:blank, and Config.Bridge stays out of reach regardless
// (messageSourceTrusted).
//
// The machine has no navigation identity, so it classifies completions by
// order alone, and two assumptions follow. A failure completion arriving while
// the surface's own load is in flight is not the surface dying - the surface is
// a data: URL whose load realistically cannot fail, while a failed Retry
// delivers a second failure completion 23ms after the one that armed the
// surface (issue #68, observed) and a rapid Retry double-click delivers more -
// so it is absorbed. And the first success completion after arming is taken as
// the surface's own load; a page-initiated navigation that supersedes the
// surface's Navigate and completes success while the surface is still loading
// is mis-taken for it, and its document stays admitted for the reserved window
// controls until the next successful navigation. The accepted costs and their
// trip-wires are recorded in decisions/0017 and decisions/0020.
func (host *Host) noteNavigationOutcome(success bool) bool {
	if success {
		host.errorPageShown = false
		if host.errorSurfaceLoading {
			// The surface's own load completing; it is now the document on
			// screen, and stays admitted until a navigation leaves it.
			host.errorSurfaceLoading = false
			return false
		}
		// A navigation away from the surface (a Retry that reached the origin,
		// or the frontend recovering on its own): its messages are foreign now.
		host.errorSurfaceActive = false
		return false
	}
	if host.errorSurfaceLoading {
		// A failure completion racing the surface's own load: a failed Retry's
		// second completion, or another failed navigation - not the surface
		// dying (issue #68). Absorb it: sealing here is what dead-ended the
		// visible surface's caption buttons, and returning false keeps the
		// no-re-navigation recursion guard (decisions/0020).
		host.log.Debug("mullion: navigation failure absorbed while the error surface loads")
		return false
	}
	if host.errorPageShown {
		// Unreachable through the transitions above: errorPageShown is true
		// only between arming and the next success completion, and arming sets
		// errorSurfaceLoading, which only a success completion clears - so the
		// absorb branch shadows this one. Kept fail-closed for a future path
		// that clears the loading flag early: in a state the machine cannot
		// explain, the admission must drop, not persist (decisions/0020).
		host.log.Warn("mullion: fallback error surface load failed, not retrying")
		host.errorSurfaceActive = false
		host.errorSurfaceLoading = false
		return false
	}
	host.errorPageShown = true
	host.errorSurfaceActive = true
	host.errorSurfaceLoading = true
	return true
}

// errorSurfaceMessageAllowed admits a web message that messageSourceAllowed
// rejected when it can only plausibly come from mullion's own fallback error
// surface: the source is the empty string - the runtime's representation of a
// data: document (issue #56, measured live) - and the surface is the document
// the host last navigated to. The admission grants the reserved window controls
// only, so the surface's caption buttons work; Config.Bridge stays behind
// messageSourceTrusted, which never accepts an empty source (decisions/0014).
func (host *Host) errorSurfaceMessageAllowed(source string) bool {
	return source == "" && host.errorSurfaceActive
}
