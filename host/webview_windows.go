//go:build windows

package host

import (
	"errors"

	"github.com/Burakuslendera/mullion/internal/logsafe"
	"github.com/Burakuslendera/mullion/internal/webview2"
)

func (host *Host) ensureWebView(source string) error {
	if host.browser != nil {
		return nil
	}
	host.log.Debug("mullion: webview create requested, source=" + logsafe.Message(source))
	if err := host.createWebView(); err != nil {
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
	browser.MessageCallback = func(message string, source string, sender *webview2.ICoreWebView2) {
		if !host.config.messageSourceAllowed(source) {
			// The bridge is injected into every document, so a top-level navigation
			// away from the frontend must not be able to drive Config.Bridge. Drop
			// the message silently - a foreign origin gets no reply to correlate.
			host.log.Warn("mullion: web message rejected, untrusted source, origin=" + logsafe.Message(urlOrigin(source)))
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
	host.browser = browser
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
// loads success=true and so cannot itself reach this branch, but the guard means a
// future change that made it fail could not loop either. Any successful load re-arms
// the guard, so a Retry that fails again shows the surface again.
func (host *Host) handleNavigationOutcome(browser *webview2.Browser, success bool) {
	if success {
		host.errorPageShown = false
		return
	}
	if host.errorPageShown {
		host.log.Warn("mullion: fallback error surface load failed, not retrying")
		return
	}
	host.errorPageShown = true
	host.log.Info("mullion: navigation failed, showing fallback error surface")
	host.warnIf("error surface navigate", browser.Navigate(errorPageURL(host.config, host.config.startURL())))
	// Release the startup show gate so the surface appears now instead of after
	// Config.ShowTimeout. The render watchdog is left armed on purpose: the intended
	// frontend never rendered, so it should still fire, and a blank frontend after a
	// Retry must still be caught.
	host.requestStartupShow("navigation_failed")
}
