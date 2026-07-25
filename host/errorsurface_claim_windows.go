//go:build windows

package host

import "strings"

// What a NavigationStarting means, split from errorsurface_windows.go, which
// owns what a NavigationCompleted means. The two callbacks are the boundary:
// this side records where a navigation was going and decides whether it is the
// fallback surface's own start - and therefore beyond the PinNavigationToOrigin
// gate's reach - while the completion side reads both answers back to attribute
// the completion that follows.
//
// The rules are decisions 0021, 0023 and 0024, unchanged by the split. As next
// door, everything here runs on the UI thread from the navigation callbacks, so
// none of the fields it touches need a lock.

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
