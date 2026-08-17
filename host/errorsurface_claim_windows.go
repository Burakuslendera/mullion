//go:build windows

package host

// What a NavigationStarting means, split from errorsurface_windows.go, which
// owns what a NavigationCompleted means. The two callbacks are the boundary:
// this side records where a navigation was going and decides whether it is the
// fallback surface's own start - and therefore beyond the PinNavigationToOrigin
// gate's reach - while the completion side reads both answers back to attribute
// the completion that follows.
//
// The rules are decisions 0021, 0023, 0024, 0027 and 0037. As next door,
// everything here runs on the UI thread from the navigation callbacks, so none
// of the fields it touches need a lock.

// noteAndGateNavigation runs the error-surface identity claim and then the
// PinNavigationToOrigin gate for one NavigationStarting, and reports whether to
// cancel. The surface's own navigation - claimed only from the exact generated
// URL or a successfully read empty URI - is never cancelled. Cancelling it
// would tear down mullion's own fallback page the moment it was recognised.
// Split from the callback so the claim-beats-gate rule and the gate are both
// headless-testable (issue #6, decisions/0023).
//
// Deciding is all it does. What follows a cancel - remembering the id and
// routing the target - happens in noteNavigationCancelled only after PutCancel
// accepts the request (decisions/0027 and 0037). Acceptance is not proof that
// navigation was abandoned: a later successful completion returns to ordinary
// policy. This helper no longer takes isUserInitiated because only routing needs
// that value, and the accepted-cancel callback receives its own copy.
func (host *Host) noteAndGateNavigation(uri string, navigationID uint64) bool {
	return host.noteAndGateNavigationKnown(uri, true, true, navigationID)
}

func (host *Host) noteAndGateNavigationKnown(
	uri string,
	uriKnown bool,
	navigationIDKnown bool,
	navigationID uint64,
) bool {
	identity := navigationIdentity{known: navigationIDKnown, value: navigationID}
	host.noteNavigationTargetObserved(uri, uriKnown, identity)
	if host.noteSurfaceNavigationStartingObserved(uri, uriKnown, identity) {
		host.log.Debug("mullion: error surface navigation identified")
		return false
	}
	if !uriKnown {
		return host.config.PinNavigationToOrigin
	}
	return host.shouldCancelNavigation(uri)
}

// noteNavigationTarget records where a start was going under the tagged
// identity observed at the same event boundary. Keeping the tag alongside the
// value is load-bearing: a failed getter must not turn into a known-zero match
// when a completion re-enters after Logger diagnostics.
func (host *Host) noteNavigationTarget(uri string, uriKnown bool, navigationID uint64) {
	host.noteNavigationTargetObserved(
		uri,
		uriKnown,
		knownNavigationIdentity(navigationID),
	)
}

func (host *Host) noteNavigationTargetObserved(
	uri string,
	uriKnown bool,
	identity navigationIdentity,
) {
	host.navStart = identity
	host.navStartInOrigin = uriKnown && host.source.origin.matches(uri)
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
	return host.noteSurfaceNavigationStartingKnown(uri, true, navigationID)
}

func (host *Host) noteSurfaceNavigationStartingKnown(uri string, uriKnown bool, navigationID uint64) bool {
	return host.noteSurfaceNavigationStartingObserved(
		uri,
		uriKnown,
		knownNavigationIdentity(navigationID),
	)
}

func (host *Host) noteSurfaceNavigationStartingObserved(
	uri string,
	uriKnown bool,
	identity navigationIdentity,
) bool {
	// Every start is a newer document boundary than an unissued completion
	// plan. Invalidate before examining the URI so even an empty value cannot
	// claim authority that production has not yet committed to Navigate.
	host.invalidateErrorSurfacePlan()
	if host.errorSurfacePending &&
		host.errorSurfacePendingGeneration != noErrorSurfacePlan &&
		uriKnown && surfaceURIMatches(uri, host.errorSurfaceURL) {
		host.errorSurfaceActive = true
		host.clearErrorSurfaceSuspension()
		host.errorSurfacePending = false
		host.errorSurfaceNav = identity
		return true
	}
	// Once a fallback has been claimed, the next top-level start is a document
	// boundary: suspend its messages immediately. A matching benign abort or
	// expected failed/cancelled completion for an accepted cancel request may
	// restore that still-visible document only when a unique non-zero identity
	// attributes both sides. Keep an unclaimed generation pending so a racing
	// foreign start cannot steal its later claim.
	host.suspendErrorSurfaceForDeparture(identity)
	return false
}

// surfaceURIMatches admits only the exact generated URL or a successfully read
// empty value. The latter is WebView2's measured representation of a data:
// document; a getter failure is kept separate by the caller and never reaches
// this predicate.
func surfaceURIMatches(reported, expected string) bool {
	return reported == expected || reported == ""
}
