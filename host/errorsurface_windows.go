//go:build windows

package host

import "github.com/Burakuslendera/mullion/internal/webview2"

type errorSurfacePlan uint64

const noErrorSurfacePlan errorSurfacePlan = 0

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

// showErrorSurface issues the side effect requested by noteNavigationOutcome,
// but only while plan is still the latest classified action. Diagnostics between
// classification and this call may pump the STA loop; any intervening start,
// success, or unclassifiable completion invalidates the plan before it can
// create fallback authority (issue #86).
//
// The state commit and Browser.Navigate are deliberately adjacent. A
// NavigationStarting callback may run from inside Navigate, so pending/loading
// must exist before that COM boundary, while no Logger or other COM call may
// open a claim window before it.
func (host *Host) showErrorSurface(browser *webview2.Browser, plan errorSurfacePlan) {
	url := errorPageURL(host.config, host.source.retryTarget)
	if !host.issueErrorSurfaceNavigation(plan, url) {
		return
	}
	if err := browser.Navigate(url); err != nil {
		// A synchronous failure produces no start/completion to revoke this
		// issued generation. Revoke before reporting: Logger is embedder code
		// and may re-enter the event machine.
		host.noteSurfaceNavigateFailed(plan)
		host.warnIf("error surface navigate", err)
	} else {
		// Report only a navigation that was actually issued successfully.
		host.log.Info("mullion: showing fallback error surface")
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

// noteSurfaceNavigateFailed revokes only the generation whose Navigate failed.
// The generation token survives its synchronous start claim so an error return
// can still revoke that same authority; a nested success clears the token first,
// protecting any newer state from this stale return.
func (host *Host) noteSurfaceNavigateFailed(plan errorSurfacePlan) {
	if host.errorSurfacePendingGeneration != plan {
		return
	}
	host.clearErrorSurfaceCapability()
}

// clearErrorSurfaceCapability fails native-control admission closed at an event
// boundary whose document identity cannot be established.
func (host *Host) clearErrorSurfaceCapability() {
	host.errorSurfacePlan = noErrorSurfacePlan
	host.errorSurfaceActive = false
	host.errorSurfaceSuspended = false
	host.errorSurfaceDeparture = navigationIdentity{}
	host.errorSurfacePending = false
	host.errorSurfacePendingGeneration = noErrorSurfacePlan
	host.errorSurfaceLoading = false
	host.errorSurfaceNav = navigationIdentity{}
}

func (host *Host) invalidateErrorSurfacePlan() {
	host.errorSurfacePlan = noErrorSurfacePlan
}

// issueErrorSurfaceNavigation converts a classified plan into claimable state.
// Production calls Browser.Navigate on the very next line; tests call this seam
// explicitly to model that issuance instead of making classification itself
// open an authority window.
func (host *Host) issueErrorSurfaceNavigation(plan errorSurfacePlan, url string) bool {
	if plan == noErrorSurfacePlan || host.errorSurfacePlan != plan {
		return false
	}
	host.errorSurfacePlan = noErrorSurfacePlan
	host.errorSurfacePending = true
	host.errorSurfacePendingGeneration = plan
	host.errorSurfaceLoading = true
	host.errorSurfaceNav = navigationIdentity{}
	host.errorSurfaceURL = url
	return true
}

func (host *Host) clearErrorSurfaceSuspension() {
	host.errorSurfaceSuspended = false
	host.errorSurfaceDeparture = navigationIdentity{}
}

func (host *Host) suspendErrorSurfaceForDeparture(identity navigationIdentity) {
	if !host.errorSurfaceActive && !host.errorSurfaceSuspended {
		return
	}
	host.errorSurfaceActive = false
	host.errorSurfaceSuspended = true
	host.errorSurfaceDeparture = identity
}

func (host *Host) restoreErrorSurfaceAfterDeparture(identity navigationIdentity) bool {
	// Zero and unavailable ids can suppress an expected cancelled completion,
	// but they cannot identify which of overlapping departures completed. Only
	// exact non-zero attribution may hand empty-source controls back.
	if !host.errorSurfaceSuspended || !identity.exact() ||
		identity != host.errorSurfaceDeparture {
		return false
	}
	host.errorSurfaceActive = true
	host.clearErrorSurfaceSuspension()
	return true
}

func (host *Host) clearErrorSurfaceSuspensionForDeparture(identity navigationIdentity) {
	if host.errorSurfaceSuspended && identity.exact() &&
		identity == host.errorSurfaceDeparture {
		host.clearErrorSurfaceSuspension()
	}
}

type unclassifiableCompletionAction uint8

const (
	unclassifiableCompletionDropped unclassifiableCompletionAction = iota
	unclassifiableCompletionSucceeded
)

// noteUnclassifiableNavigationCompletion owns the state transition for a
// completion with at least one failed getter. It must run before diagnostics:
// Logger is embedder code and can re-enter with a message, so stale fallback
// admission cannot survive until the first log call.
func (host *Host) noteUnclassifiableNavigationCompletion(
	successKnown, success, statusKnown bool,
	status webview2.WebErrorStatus,
	navigationIDKnown bool,
	navigationID uint64,
) unclassifiableCompletionAction {
	host.invalidateErrorSurfacePlan()
	identity := navigationIdentity{known: navigationIDKnown, value: navigationID}
	if !successKnown {
		host.clearErrorSurfaceCapability()
		host.log.Debug("mullion: navigation completion unclassifiable, success=unavailable, fallback controls disabled")
		return unclassifiableCompletionDropped
	}
	if success {
		if !navigationIDKnown {
			host.clearErrorSurfaceCapability()
		} else {
			host.noteNavigationSuccessObserved(identity)
			if identity.exact() && host.takeCancelledNavigationID(identity.value) {
				host.log.Warn("mullion: cancelled navigation committed anyway, the cancel did not take, " +
					navigationIdentityField(identity))
			}
		}
		return unclassifiableCompletionSucceeded
	}

	host.clearErrorSurfaceCapability()
	fields := unavailableNavigationFailureFields(status, statusKnown, navigationID, navigationIDKnown)
	if statusKnown && host.takeCancelledNavigationObserved(status, identity) {
		host.log.Debug("mullion: cancelled navigation completed through bounded identity, " + fields)
		return unclassifiableCompletionDropped
	}
	if identity.exact() && host.takeCancelledNavigationID(identity.value) {
		host.log.Debug("mullion: cancelled navigation completed, " + fields)
		return unclassifiableCompletionDropped
	}
	host.log.Debug("mullion: navigation failure unclassifiable, fallback controls disabled, " + fields)
	return unclassifiableCompletionDropped
}

func unavailableNavigationFailureFields(
	status webview2.WebErrorStatus,
	statusKnown bool,
	navigationID uint64,
	navigationIDKnown bool,
) string {
	statusField := "status=unavailable"
	if statusKnown {
		statusField = "status=" + formatInt32(int32(status))
	}
	idField := "id=unavailable"
	if navigationIDKnown {
		idField = "id=" + formatUint64(navigationID)
	}
	return statusField + ", " + idField
}

// noteNavigationOutcome runs the headless error-surface classification and
// reports whether it produced a fallback plan. Production keeps the actual
// token through planNavigationOutcome so it can reject stale side effects.
//
// Classification creates only an unissued plan. issueErrorSurfaceNavigation
// makes the surface pending/loading immediately before Browser.Navigate.
// Admission begins only when NavigationStarting successfully reports either the
// exact generated URL or WebView2's measured empty representation of a data:
// document. That claim still precedes document creation and its earliest
// messages, while preventing the departing failed document from borrowing
// empty-source controls before Navigate. Config.Bridge remains behind
// source-plan trust and never accepts the empty source.
//
// Completions are positively attributed only when both this completion and the
// surface claim carry the same exact non-zero identity (decisions/0021): the
// surface's own completion resolves the surface - success re-admits it, a
// superseded start is cleanup, and a genuine load failure seals. Known zero is
// retained as observed provenance but is not unique enough to grant authority.
// When exact identity is missing, the order-based fallback resolves a loading
// window on success without creating admission and absorbs failures inside it.
// Admission remains tied to the NavigationStarting claim.
func (host *Host) noteNavigationOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	return host.planNavigationOutcome(success, status, navigationID) != noErrorSurfacePlan
}

func (host *Host) planNavigationOutcome(
	success bool,
	status webview2.WebErrorStatus,
	navigationID uint64,
) errorSurfacePlan {
	return host.planNavigationOutcomeObserved(
		success,
		status,
		knownNavigationIdentity(navigationID),
	)
}

func (host *Host) planNavigationOutcomeObserved(
	success bool,
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) errorSurfacePlan {
	if success {
		host.noteNavigationSuccessObserved(identity)
		return noErrorSurfacePlan
	}
	if identity.exact() && host.errorSurfaceNav.exact() {
		if identity == host.errorSurfaceNav {
			host.noteSurfaceOwnOutcome(false, status, identity.value)
			return noErrorSurfacePlan
		}
		return host.noteForeignOutcome(false, status, identity)
	}
	if identity.known && host.errorSurfacePending {
		return host.noteForeignOutcome(false, status, identity)
	}
	if identity.known && !host.errorSurfaceLoading {
		return host.noteForeignOutcome(false, status, identity)
	}
	return host.noteOrderedOutcome(false, status, identity)
}

// noteNavigationSuccess classifies a successful completion whose id getter
// succeeded. The observed half keeps a known zero separate from unavailable.
func (host *Host) noteNavigationSuccess(navigationID uint64) {
	host.noteNavigationSuccessObserved(knownNavigationIdentity(navigationID))
}

func (host *Host) noteNavigationSuccessObserved(identity navigationIdentity) {
	host.invalidateErrorSurfacePlan()
	host.clearErrorSurfaceSuspension()
	if identity.exact() && host.errorSurfaceNav.exact() {
		if identity == host.errorSurfaceNav {
			host.errorSurfacePending = false
			host.errorSurfacePendingGeneration = noErrorSurfacePlan
			host.errorSurfaceLoading = false
			host.errorSurfaceNav = navigationIdentity{}
			host.errorSurfaceActive = true
			return
		}
		host.errorSurfaceActive = false
		host.errorSurfacePendingGeneration = noErrorSurfacePlan
		host.errorSurfaceLoading = false
		return
	}
	if identity.known && (host.errorSurfacePending || !host.errorSurfaceLoading) {
		host.errorSurfaceActive = false
		if !host.errorSurfacePending {
			host.errorSurfacePendingGeneration = noErrorSurfacePlan
		}
		host.errorSurfaceLoading = false
		return
	}
	if host.errorSurfaceLoading || host.errorSurfacePending {
		host.errorSurfacePending = false
		host.errorSurfacePendingGeneration = noErrorSurfacePlan
		host.errorSurfaceLoading = false
		return
	}
	host.errorSurfaceActive = false
}

// noteSurfaceOwnOutcome handles a completion positively attributed to the
// surface's own navigation.
func (host *Host) noteSurfaceOwnOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	host.errorSurfacePending = false
	host.errorSurfacePendingGeneration = noErrorSurfacePlan
	host.errorSurfaceLoading = false
	host.errorSurfaceNav = navigationIdentity{}
	if success {
		host.clearErrorSurfaceSuspension()
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
	host.clearErrorSurfaceSuspension()
	host.errorSurfaceActive = false
	host.log.Warn("mullion: fallback error surface load failed, not retrying, " + navigationFailureFields(status, navigationID))
	return false
}

// noteForeignOutcome handles a completion positively attributed to a
// navigation that is not the surface's.
func (host *Host) noteForeignOutcome(
	success bool,
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) errorSurfacePlan {
	navigationID := identity.value
	if success {
		host.invalidateErrorSurfacePlan()
		host.clearErrorSurfaceSuspension()
		host.errorSurfaceActive = false
		host.errorSurfaceLoading = false
		return noErrorSurfacePlan
	}
	if host.errorSurfacePlan != noErrorSurfacePlan ||
		host.errorSurfaceLoading || host.errorSurfacePending {
		host.clearErrorSurfaceSuspensionForDeparture(identity)
		host.log.Debug("mullion: navigation failure absorbed while the error surface loads, " +
			navigationFailureIdentityFields(status, identity))
		return noErrorSurfacePlan
	}
	if host.benignAbort(status, identity) {
		host.restoreErrorSurfaceAfterDeparture(identity)
		host.log.Debug("mullion: navigation aborted, not arming the error surface, " +
			navigationFailureIdentityFields(status, identity))
		return noErrorSurfacePlan
	}
	return host.armErrorSurface(status, navigationID)
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
func (host *Host) benignAbort(status webview2.WebErrorStatus, identity navigationIdentity) bool {
	if status != webview2.WebErrorStatusConnectionAborted || !host.source.embedded {
		return false
	}
	return identity.exact() && identity == host.navStart && host.navStartInOrigin
}

// armErrorSurface starts a new surface generation. It is one function because
// both entry points - the identified path and 0020's id-less fallback - have to
// arm identically, including retiring any suspended prior document. A drift
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
// The plan write comes before the log, and that order is load-bearing. Logger
// is arbitrary embedder code and may pump a nested completion. Publishing the
// token first makes a nested failure absorb without creating competing
// authority, while pending/loading remain closed until production is adjacent
// to Browser.Navigate.
func (host *Host) armErrorSurface(status webview2.WebErrorStatus, navigationID uint64) errorSurfacePlan {
	host.clearErrorSurfaceSuspension()
	host.errorSurfaceActive = false
	host.errorSurfacePending = false
	host.errorSurfacePendingGeneration = noErrorSurfacePlan
	host.errorSurfaceLoading = false
	host.errorSurfaceNav = navigationIdentity{}
	host.errorSurfacePlanSerial++
	if host.errorSurfacePlanSerial == uint64(noErrorSurfacePlan) {
		host.errorSurfacePlanSerial++
	}
	plan := errorSurfacePlan(host.errorSurfacePlanSerial)
	host.errorSurfacePlan = plan
	host.log.Warn("mullion: navigation failed, " + navigationFailureFields(status, navigationID))
	return plan
}

// noteOrderedOutcome is the fallback for completions the machine cannot
// attribute. Failures inside a pending/loading window are absorbed; success
// resolves that window but cannot create admission. Only a positively claimed
// NavigationStarting activates the surface, so an unreadable start cannot be
// laundered into a legitimate empty-source document by an id-less completion.
func (host *Host) noteOrderedOutcome(
	success bool,
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) errorSurfacePlan {
	if success {
		host.invalidateErrorSurfacePlan()
		host.clearErrorSurfaceSuspension()
		if host.errorSurfaceLoading || host.errorSurfacePending {
			host.errorSurfacePending = false
			host.errorSurfacePendingGeneration = noErrorSurfacePlan
			host.errorSurfaceLoading = false
			return noErrorSurfacePlan
		}
		host.errorSurfaceActive = false
		return noErrorSurfacePlan
	}
	if host.errorSurfacePlan != noErrorSurfacePlan ||
		host.errorSurfaceLoading || host.errorSurfacePending {
		host.clearErrorSurfaceSuspensionForDeparture(identity)
		host.log.Warn("mullion: unattributed navigation failure absorbed while the error surface loads, " +
			navigationFailureIdentityFields(status, identity))
		return noErrorSurfacePlan
	}
	return host.armErrorSurface(status, identity.value)
}

// errorSurfaceMessageAllowed admits a web message that messageSourceAllowed
// rejected when it can only plausibly come from mullion's own fallback error
// surface: the source is the empty string - the runtime's representation of a
// data: document (issue #56, measured live) - and NavigationStarting positively
// claimed the current fallback generation.
// The later bridge dispatch grants only the six existing fallback window-control
// methods, so injected readiness and diagnostics cannot mutate the failed
// application's watchdog evidence;
// Config.Bridge stays behind messageSourceTrusted, which never accepts an empty
// source (decisions/0014).
func (host *Host) errorSurfaceMessageAllowed(source string) bool {
	return source == "" && host.errorSurfaceActive
}
