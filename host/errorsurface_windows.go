//go:build windows

package host

import (
	"strings"

	"github.com/Burakuslendera/mullion/internal/webview2"
)

// The fallback error surface's admission state machine, split out of
// webview_windows.go: that file owns the WebView's lifecycle - embed, commit,
// navigate, tear down - and this one owns what the navigation callbacks mean
// once they arrive. The two seams the PinNavigationToOrigin gate meets it
// through (decisions/0023) live here too, because their whole reason for
// existing is the interaction: a claimed surface start is never cancelled, and
// a cancelled navigation's completion never reaches the machine below.
//
// The rules themselves are decisions 0017, 0020, 0021 and 0024. Everything here
// runs on the UI thread, from the navigation callbacks, so none of the
// errorSurface* fields need a lock.

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

// noteGateCancelledOutcome intercepts the completion of a navigation the
// PinNavigationToOrigin gate cancelled (decisions/0023). The runtime completes a
// put_Cancel'd navigation with OperationCanceled; that is not a load failure -
// the cancel was deliberate, the foreign target was routed to the system browser,
// and the current document stays - so this completion must not be reported as a
// failure, resynced or fed to the error-surface machine, which would read a
// foreign failure as "navigate to the fallback surface" and tear down the live
// frontend. It reports whether it consumed the completion; the id is cleared so a
// later navigation reusing state cannot inherit it (ids are monotonic, so it will
// not recur, but clearing keeps the slot honest).
func (host *Host) noteGateCancelledOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	if navigationID == 0 || navigationID != host.cancelledNavID {
		return false
	}
	host.cancelledNavID = 0
	if !success {
		host.log.Debug("mullion: cancelled navigation completed, status=" + formatInt32(int32(status)))
	}
	return true
}

// noteAndGateNavigation runs the error-surface identity claim and then the
// PinNavigationToOrigin gate for one NavigationStarting, and reports whether to
// cancel. The surface's own navigation - claimed here - is never cancelled,
// whatever URI the runtime reports for it: an empty or truncated data: URI is a
// tolerated form of the surface's start (surfaceURIMatches), and cancelling it
// would tear down mullion's own fallback page the moment it was recognised. Split
// from the callback so the claim-beats-gate rule and the gate are both
// headless-testable (issue #6, decisions/0023).
func (host *Host) noteAndGateNavigation(uri string, navigationID uint64, isUserInitiated bool) bool {
	host.noteNavigationTarget(uri, navigationID)
	if host.noteSurfaceNavigationStarting(uri, navigationID) {
		host.log.Debug("mullion: error surface navigation identified, id=" + formatUint64(navigationID))
		return false
	}
	return host.shouldCancelNavigation(uri, navigationID, isUserInitiated)
}

// noteNavigationTarget records where the navigation that is starting was going,
// keyed by its id, because the completion will not say (decisions/0024). It runs
// before the surface claim and the gate, so it sees every start, including the
// surface's own - whose data: URI is not the trusted origin, which is the right
// answer: an aborted surface load is resolved by identity, never by this pair.
//
// An unreadable URI reaches here as the empty string, which is not the trusted
// origin either, so a start the runtime could not describe never earns the
// abort exemption.
func (host *Host) noteNavigationTarget(uri string, navigationID uint64) {
	host.navStartID = navigationID
	host.navStartInOrigin = sameHTTPOrigin(uri, host.config.trustedOrigin())
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
		return host.noteForeignOutcome(success, status, navigationID)
	}
	if navigationID != 0 && host.errorSurfacePending {
		// A completion cannot precede its own navigation's start, so while the
		// surface's start is still unclaimed, an identified completion is
		// necessarily some other navigation's - classifying it foreign keeps
		// the claim window open for the surface's own start.
		return host.noteForeignOutcome(success, status, navigationID)
	}
	if navigationID != 0 && !host.errorSurfaceLoading {
		// Identified completion with no surface story in flight: ordinary
		// classification, same result the fallback would produce, taken here
		// so the fallback below stays exactly 0020's machine.
		return host.noteForeignOutcome(success, status, navigationID)
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
func (host *Host) noteForeignOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
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
	if host.benignAbort(status, navigationID) {
		host.log.Debug("mullion: navigation aborted, not arming the error surface, status=" + formatInt32(int32(status)))
		return false
	}
	// Arming starts a new surface generation: any lingering id belongs to a
	// navigation that no longer matters here, and carrying it forward would
	// let a superseded generation's late cancel be mis-attributed to this one
	// and unwind its claim window before its start ever fires.
	host.armErrorSurface()
	return true
}

// benignAbort reports whether an attributed failure completion is an abort that
// must not arm the fallback surface (issue #72, decisions/0024).
//
// ConnectionAborted is a connection that ended mid-flight, and whether that can
// mean "could not load" depends on whether this navigation had a connection at
// all. Two conditions have to hold together, and both are about where the bytes
// for *this* navigation came from:
//
//   - mullion serves the frontend itself, from the embedded fs.FS through
//     WebResourceRequested (Config.URL empty). With Config.URL set the caller
//     serves it over a socket, and a dead endpoint has been seen to produce this
//     status (issue #68), which is the case the surface exists for.
//   - the navigation was headed for the trusted origin. Config.URL being empty
//     does not keep the top frame there: PinNavigationToOrigin is opt-in and off
//     by default (decisions/0023), so a frontend link or a script assignment can
//     take the top frame to any origin, and that navigation is a real socket
//     load whose abort is a real failure. noteNavigationTarget is what remembers
//     the answer; a completion carries no URI of its own.
//
// Both true, the status can only mean the runtime abandoned a navigation it had
// started - which a renderer-initiated same-origin document navigation was
// observed doing while its asset was served 200 (issue #72). Arming there
// replaces a live frontend with the fallback page over a navigation that was
// never going to fail.
//
// The id must still match the navigation that started, so a completion for an
// older navigation - the one case where the recorded target is not this
// completion's - falls through and arms, which is the safe direction.
//
// It deliberately does not extend to noteOrderedOutcome: without an id there is
// nothing to say this completion belongs to the navigation whose asset was
// served, and suppressing the surface on that guess would fail open in the one
// case the surface is for. Absent identity, 0020's machine stands.
func (host *Host) benignAbort(status webview2.WebErrorStatus, navigationID uint64) bool {
	if status != webview2.WebErrorStatusConnectionAborted || !host.config.servesAssetsInProcess() {
		return false
	}
	return navigationID != 0 && navigationID == host.navStartID && host.navStartInOrigin
}

// armErrorSurface starts a new surface generation. It is one function because
// both entry points - the identified path and 0020's id-less fallback - have to
// arm identically, and the four fields are the machine's whole state: a drift
// between the two call sites is a state the decisions 0020, 0021 and 0024 never
// describe.
//
// Resetting the id is the load-bearing part: a lingering one belongs to a
// navigation that no longer matters here, and carrying it forward would let a
// superseded generation's late cancel be mis-attributed to this one and unwind
// its claim window before its start ever fires.
func (host *Host) armErrorSurface() {
	host.errorSurfaceActive = true
	host.errorSurfaceLoading = true
	host.errorSurfacePending = true
	host.errorSurfaceNavID = 0
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
	host.armErrorSurface()
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
