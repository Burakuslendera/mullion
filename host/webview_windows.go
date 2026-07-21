//go:build windows

package host

import (
	"errors"
	"strconv"
	"strings"

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
	browser.NavigationStartingCallback = func(uri string, navigationID uint64, isUserInitiated bool, isRedirected bool) {
		// The uri is clamped and reduced like a rejected message source: a
		// navigation target is foreign input, and a data: URI is arbitrarily
		// long. The id is what ties this line to the completion that follows.
		host.log.Debug("mullion: navigation starting, id=" + formatUint64(navigationID) +
			", user_initiated=" + strconv.FormatBool(isUserInitiated) +
			", redirected=" + strconv.FormatBool(isRedirected) +
			", uri=" + logsafe.Message(clampSourceForLog(uri)))
		if host.noteSurfaceNavigationStarting(uri, navigationID) {
			host.log.Debug("mullion: error surface navigation identified, id=" + formatUint64(navigationID))
		}
	}
	browser.NavigationCompletedCallback = func(success bool, status webview2.WebErrorStatus, navigationID uint64) {
		if !success {
			host.log.Warn("mullion: navigation failed, status=" + formatInt32(int32(status)) + ", id=" + formatUint64(navigationID))
		}
		host.log.Debug("mullion: navigation completed, id=" + formatUint64(navigationID))
		host.syncWebViewBounds("navigation_completed")
		host.warnIf("navigation diagnostic eval", browser.Eval(host.js.navigationEval))
		host.handleNavigationOutcome(browser, success, status, navigationID)
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
// It runs on the UI thread from NavigationCompletedCallback, so the error-surface
// state needs no lock. The recursion guard is belt-and-braces: the fallback is a
// data: page that loads success=true and so cannot itself reach this branch, and if
// its load did fail, the completion would carry the surface's own navigation id and
// land in the seal branch - disarm, never re-navigate - while an unattributable
// failure inside the loading window is absorbed (decisions/0021). It cannot loop
// either way. Any successful load re-arms the guard, so a Retry that fails again
// shows the surface again.
//
// A synchronous Navigate failure means no completion will ever arrive for the
// surface; noteSurfaceNavigateFailed unwinds the arming so the admission does not
// stay armed against a navigation that never started (a residual decision 0020
// accepted, closed by 0021).
func (host *Host) handleNavigationOutcome(browser *webview2.Browser, success bool, status webview2.WebErrorStatus, navigationID uint64) {
	if !host.noteNavigationOutcome(success, status, navigationID) {
		return
	}
	host.log.Info("mullion: navigation failed, showing fallback error surface")
	host.errorSurfaceURL = errorPageURL(host.config, host.config.startURL())
	if err := browser.Navigate(host.errorSurfaceURL); err != nil {
		host.warnIf("error surface navigate", err)
		host.noteSurfaceNavigateFailed()
	}
	// Release the startup show gate so the surface appears now instead of after
	// Config.ShowTimeout - even when the Navigate failed, because a visible broken
	// window can be reported and a hidden one cannot. The render watchdog is left
	// armed on purpose: the intended frontend never rendered, so it should still
	// fire, and a blank frontend after a Retry must still be caught.
	host.requestStartupShow("navigation_failed")
}

// noteSurfaceNavigationStarting claims a NavigationStarting event as the
// fallback error surface's own navigation and records its id. It reports
// whether the claim happened, so the caller can log it. Split from the
// callback so the claim is headless-testable without a Browser.
//
// The claim is guarded twice. errorSurfacePending scopes it to the window
// between the host issuing the surface Navigate and that navigation starting,
// so no later data: navigation can steal the identity. The URI match then
// keeps a racing foreign navigation - one already queued when the host
// navigated - from being claimed inside that window: its http(s) URI matches
// none of the accepted forms, so it passes through unclaimed and the surface's
// own start, which the runtime guarantees will still fire, claims later.
func (host *Host) noteSurfaceNavigationStarting(uri string, navigationID uint64) bool {
	if !host.errorSurfacePending {
		return false
	}
	if !surfaceURIMatches(uri, host.errorSurfaceURL) {
		return false
	}
	host.errorSurfacePending = false
	host.errorSurfaceNavID = navigationID
	return true
}

// surfaceURIMatches reports whether a NavigationStarting URI can be the
// surface's own navigation. The exact data: URL is deterministic
// (errorPageURL is a pure function of Config), so equality is the primary
// test. Two tolerances cover runtime reporting variance while the surface
// Navigate is pending: an empty URI, because the runtime erases data: URIs at
// both GetSource levels (issue #56, measured live) and it is unverified
// whether NavigationStarting shares that erasure; and any other data: URI,
// because content cannot navigate the top frame to data: (Chromium blocks
// renderer-initiated top-level data: navigations; likely) and the host issues
// no data: URL but the surface - so a data: start inside the pending window is
// the surface's own start, however the runtime chose to report or truncate it.
func surfaceURIMatches(reported, expected string) bool {
	if reported == expected {
		return true
	}
	if reported == "" {
		return true
	}
	return strings.HasPrefix(reported, "data:")
}

// noteSurfaceNavigateFailed unwinds an arming whose Navigate call itself
// failed: no NavigationStarting and no completion will ever arrive for the
// surface, so leaving the admission armed would hold it open against whatever
// document is on screen with nothing left to resolve it (the completion-less
// residual decision 0020 accepted, closed here per 0021).
func (host *Host) noteSurfaceNavigateFailed() {
	host.errorSurfaceActive = false
	host.errorSurfacePending = false
	host.errorSurfaceLoading = false
	host.errorSurfaceNavID = 0
}

// noteNavigationOutcome runs the error-surface bookkeeping for a completed
// navigation and reports whether the fallback surface should be navigated to
// now. It is split from handleNavigationOutcome so the state machine is
// headless-testable without a Browser.
//
// The surface is armed at the decision to navigate, rather than when its load
// completes: the injected diagnostics post their first messages from document
// creation, before NavigationCompleted fires, and arming late would reject
// them (the ten-in-a-row WARN flurry issue #56 was reported with). The cost is
// a window, until the surface's document commits, in which the departing
// document could post an empty-source message and be granted the reserved
// methods; on this path that document is a failed load or mullion's own
// about:blank, and Config.Bridge stays out of reach regardless
// (messageSourceTrusted).
//
// Completions are attributed by navigation id when both this completion's id
// and the surface's claimed id are known (decisions/0021): the surface's own
// completion resolves the surface - success re-admits it positively, a
// superseded start is cleanup, a genuine load failure seals - and every other
// completion is someone else's, however it is ordered against the surface's.
// When either id is missing, the machine falls back to the order-based rules
// decision 0020 locked: the first success inside the loading window is taken
// as the surface's load, failures inside it are absorbed, and the accepted
// costs of that ordering are 0020's.
func (host *Host) noteNavigationOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	if navigationID != 0 && host.errorSurfaceNavID != 0 {
		if navigationID == host.errorSurfaceNavID {
			return host.noteSurfaceOwnOutcome(success, status)
		}
		return host.noteForeignOutcome(success)
	}
	if navigationID != 0 && host.errorSurfacePending {
		// A completion cannot precede its own navigation's start, so while the
		// surface's start is still unclaimed, an identified completion is
		// necessarily some other navigation's - classifying it foreign keeps
		// the claim window open for the surface's own start.
		return host.noteForeignOutcome(success)
	}
	if navigationID != 0 && !host.errorSurfaceLoading {
		// Identified completion with no surface story in flight: ordinary
		// classification, same result the fallback would produce, taken here
		// so the fallback below stays exactly 0020's machine.
		return host.noteForeignOutcome(success)
	}
	return host.noteOrderedOutcome(success)
}

// noteSurfaceOwnOutcome handles a completion positively attributed to the
// surface's own navigation.
func (host *Host) noteSurfaceOwnOutcome(success bool, status webview2.WebErrorStatus) bool {
	host.errorSurfacePending = false
	host.errorSurfaceLoading = false
	host.errorSurfaceNavID = 0
	if success {
		// The surface committed: it is the document on screen, so it is
		// admitted - asserted, not merely left armed, because a foreign
		// success that landed inside the loading window has dropped the
		// admission and this is what restores it to the right document.
		host.errorSurfaceActive = true
		return false
	}
	if status == webview2.WebErrorStatusOperationCanceled {
		// The surface's Navigate was superseded by a newer navigation before
		// it committed (the runtime completes the loser with OperationCanceled).
		// Not the surface dying: the winner's completion decides the document,
		// so leave the admission for it to resolve.
		host.log.Debug("mullion: error surface navigation superseded")
		return false
	}
	// The surface's own load genuinely failed - the one claim the pre-identity
	// machines could never make (issues #56/#68). Nothing on screen is
	// mullion's page, so the admission drops, and re-navigating would loop.
	host.log.Warn("mullion: fallback error surface load failed, not retrying")
	host.errorSurfaceActive = false
	return false
}

// noteForeignOutcome handles a completion positively attributed to a
// navigation that is not the surface's.
func (host *Host) noteForeignOutcome(success bool) bool {
	if success {
		// A foreign document committed, so the empty source is foreign again.
		// A still-unresolved surface navigation stays claimable: if the
		// surface commits after this document, its own success re-admits it
		// (noteSurfaceOwnOutcome), and if it was superseded, its canceled
		// completion cleans up.
		host.errorSurfaceActive = false
		host.errorSurfaceLoading = false
		return false
	}
	if host.errorSurfaceLoading || host.errorSurfacePending {
		// A foreign failure while the surface is on its way - the failed
		// Retry's second completion (issue #68), or another navigation losing
		// a race - changes nothing: the surface's own completion is still
		// coming, and re-navigating here is the recursion the guard exists
		// for.
		host.log.Debug("mullion: navigation failure absorbed while the error surface loads")
		return false
	}
	// Arming starts a new surface generation: any lingering id belongs to a
	// navigation that no longer matters here, and carrying it forward would
	// let a superseded generation's late cancel be mis-attributed to this one
	// and unwind its claim window before its start ever fires.
	host.errorSurfaceActive = true
	host.errorSurfaceLoading = true
	host.errorSurfacePending = true
	host.errorSurfaceNavID = 0
	return true
}

// noteOrderedOutcome is the order-based fallback for completions the machine
// cannot attribute - this completion's id is unavailable, or the surface is in
// flight without a claimed id. It is decision 0020's machine verbatim: the
// first success inside the loading window is taken as the surface's own load,
// failures inside the window are absorbed, and a failure outside it arms. Its
// accepted costs - the mis-admission orderings 0017 and 0020 record - apply
// only while identity is unavailable.
func (host *Host) noteOrderedOutcome(success bool) bool {
	if success {
		if host.errorSurfaceLoading || host.errorSurfacePending {
			// Taken as the surface's own load completing; it is now the
			// document on screen, and stays admitted until a navigation
			// leaves it. A claimed id is left alone: if this id-less
			// completion was not actually the surface's, the surface's own
			// identified completion must still be attributable when it comes.
			host.errorSurfaceActive = true
			host.errorSurfacePending = false
			host.errorSurfaceLoading = false
			return false
		}
		// A navigation away from the surface (a Retry that reached the origin,
		// or the frontend recovering on its own): its messages are foreign now.
		host.errorSurfaceActive = false
		return false
	}
	if host.errorSurfaceLoading || host.errorSurfacePending {
		host.log.Debug("mullion: navigation failure absorbed while the error surface loads")
		return false
	}
	// Arming resets the generation id for the same reason as the identity
	// arm above.
	host.errorSurfaceActive = true
	host.errorSurfaceLoading = true
	host.errorSurfacePending = true
	host.errorSurfaceNavID = 0
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
