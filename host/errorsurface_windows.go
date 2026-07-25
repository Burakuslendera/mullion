//go:build windows

package host

import "github.com/Burakuslendera/mullion/internal/webview2"

// The fallback error surface's admission state machine, split out of
// webview_windows.go: that file owns the WebView's lifecycle - embed, commit,
// navigate, tear down - and this one owns what a NavigationCompleted means once
// it arrives. What a NavigationStarting means is the other half, in
// errorsurface_claim_windows.go. The seam the PinNavigationToOrigin gate meets
// this machine through (decisions/0023) lives here, because its whole reason for
// existing is the interaction: a cancelled navigation's completion never reaches
// the machine below. The ledger that decides which completions those are is
// navigationcancel_windows.go.
//
// The rules themselves are decisions 0017, 0020, 0021, 0024 and 0026.
// Everything here runs on the UI thread, from the navigation callbacks, so none
// of the errorSurface* fields need a lock.
//
// One rule spans every branch below: a failed completion is reported exactly
// once, by whichever branch classified it, at the level that classification
// deserves - warn where the host is reporting a failure, debug where the host is
// saying it expected this one and has handled it (issue #79, decisions/0026).
// The completion callback hands failures down unlogged for that reason: it runs
// before the classification exists, so anything it logged would be a guess at
// the level.

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
	// Not "navigation failed, showing ...": the failure was already reported, once,
	// by the branch that classified it, and repeating the phrase here put two
	// hits per arming in front of anyone grepping for it - which is the claim
	// this whole change makes (issue #79, decisions/0026). This line says what
	// the host did about it, and nothing else.
	host.log.Info("mullion: showing fallback error surface")
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

// navigationFailureFields is the tail every report of a failed completion
// carries: the status the runtime gave it and the navigation it belongs to. One
// function so the reports stay one family - a reader comparing two of them is
// comparing the same fields in the same order - and so the id cannot be dropped
// from a line by being forgotten at one site.
func navigationFailureFields(status webview2.WebErrorStatus, navigationID uint64) string {
	return "status=" + formatInt32(int32(status)) + ", id=" + formatUint64(navigationID)
}

// The completion of a navigation the PinNavigationToOrigin gate cancelled never
// reaches the machine below: noteGateCancelledOutcome, in
// navigationcancel_windows.go, consumes it first (decisions/0023, 0027).
//
// The NavigationStarting half - the surface's claim on a start, the navigation
// target the abort exemption reads back, and the gate seam that lets a claimed
// start through uncancelled - is in errorsurface_claim_windows.go. It was here.

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
			return host.noteSurfaceOwnOutcome(success, status, navigationID)
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
	return host.noteOrderedOutcome(success, status, navigationID)
}

// noteSurfaceOwnOutcome handles a completion positively attributed to the
// surface's own navigation.
func (host *Host) noteSurfaceOwnOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
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
		// so leave the admission for it to resolve. Expected and handled, so
		// debug - the host asked for this navigation and then replaced it.
		host.log.Debug("mullion: error surface navigation superseded, " + navigationFailureFields(status, navigationID))
		return false
	}
	// The surface's own load genuinely failed - the one claim the pre-identity
	// machines could never make (issues #56/#68). Nothing on screen is
	// mullion's page, so the admission drops, and re-navigating would loop.
	// This is the whole report of that failure: the callback no longer logs a
	// generic one in front of it, which used to make one dead surface two
	// warnings.
	//
	// The admission drops before the log for the reason armErrorSurface gives:
	// the Logger is embedder code, and a re-entrant completion reaching this
	// machine while the dead surface is still marked admitted would both be
	// classified against a lie and have its own arming overwritten by the write
	// below. This ordering was the other way round before decisions/0026.
	host.errorSurfaceActive = false
	host.log.Warn("mullion: fallback error surface load failed, not retrying, " + navigationFailureFields(status, navigationID))
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
		//
		// Debug, unlike the same absorb in noteOrderedOutcome: this completion
		// is positively someone else's, so it is not the surface's own load
		// dying in disguise, and the failure that put the surface in flight
		// warned when it armed.
		host.log.Debug("mullion: navigation failure absorbed while the error surface loads, " + navigationFailureFields(status, navigationID))
		return false
	}
	if host.benignAbort(status, navigationID) {
		host.log.Debug("mullion: navigation aborted, not arming the error surface, " + navigationFailureFields(status, navigationID))
		return false
	}
	// Arming starts a new surface generation: any lingering id belongs to a
	// navigation that no longer matters here, and carrying it forward would
	// let a superseded generation's late cancel be mis-attributed to this one
	// and unwind its claim window before its start ever fires.
	host.armErrorSurface(status, navigationID)
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
//
// The warning belongs here for the same reason the arming does. Arming is the
// host deciding this failure is real and worth replacing the document over, so
// it is exactly the set of failures a warning should count (issue #79,
// decisions/0026); putting the line at the two call sites instead would let the
// two drift, and putting it in the callback is what made the count wrong in the
// first place. Only failures reach this - both callers arm behind !success - so
// the line never describes a completion that worked.
//
// The log call comes last, and that order is load-bearing. It hands control to
// the embedder's Logger, which is arbitrary code; if it pumps messages - a
// MessageBox, a GUI toolkit's own loop - a queued navigation completion is
// dispatched inside it and runs this machine re-entrantly. Logging first would
// let that nested completion arm, claim and navigate a generation, and then the
// four writes below would land on top of it: the claim destroyed, a second
// surface Navigate issued, and the surface finally on screen unadmitted, which
// is issue #56's dead-caption-buttons symptom. Writing first means the nested
// call sees a machine that is already armed and absorbs, exactly as it would
// have before this line existed.
func (host *Host) armErrorSurface(status webview2.WebErrorStatus, navigationID uint64) {
	host.errorSurfaceActive = true
	host.errorSurfaceLoading = true
	host.errorSurfacePending = true
	host.errorSurfaceNavID = 0
	host.log.Warn("mullion: navigation failed, " + navigationFailureFields(status, navigationID))
}

// noteOrderedOutcome is the order-based fallback for completions the machine
// cannot attribute - this completion's id is unavailable, or the surface is in
// flight without a claimed id. It is decision 0020's machine verbatim: the
// first success inside the loading window is taken as the surface's own load,
// failures inside the window are absorbed, and a failure outside it arms. Its
// accepted costs - the mis-admission orderings 0017 and 0020 record - apply
// only while identity is unavailable.
func (host *Host) noteOrderedOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
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
		// Warn, where the identified absorb above is debug. Absent identity
		// this branch is also where the surface's own load dying lands - 0020
		// absorbs every failure in the window because it cannot tell whose it
		// is - so silencing it would take the only trace of a dead surface out
		// of the log at exactly the point the seal is unreachable
		// (decisions/0026).
		//
		// "unattributed" is what tells the two absorbs apart in the artifact
		// they exist for. The level alone does not: a log read after the fact is
		// text, and the id does not separate them either - this branch is
		// reachable with a non-zero id, when the surface's own start was claimed
		// under an id the runtime could not supply.
		host.log.Warn("mullion: unattributed navigation failure absorbed while the error surface loads, " + navigationFailureFields(status, navigationID))
		return false
	}
	// Arming resets the generation id for the same reason as the identity
	// arm above.
	host.armErrorSurface(status, navigationID)
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
