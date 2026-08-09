//go:build windows

package host

import "github.com/Burakuslendera/mullion/internal/webview2"

// navigationIdentity carries the result of GetNavigationID without assigning a
// getter failure the authority of the valid value zero. A non-zero known value
// is the only identity unique enough to restore suspended fallback controls;
// zero and unavailable identities remain useful only as bounded cleanup hints.
type navigationIdentity struct {
	known bool
	value uint64
}

func knownNavigationIdentity(value uint64) navigationIdentity {
	return navigationIdentity{known: true, value: value}
}

func (identity navigationIdentity) exact() bool {
	return identity.known && identity.value != 0
}

// The ledger of navigations the PinNavigationToOrigin gate cancelled, and what
// their completions mean (decisions/0023, 0027). Split from the error-surface
// machine next door because it is a different question: that file decides what
// a completion says about the document on screen, this one decides whether a
// completion belongs to a navigation the host deliberately abandoned - and
// answers it before the machine is ever consulted.
//
// A cancel is only ever entered here after the runtime confirmed it, so the
// ledger is a record of what happened rather than of what was asked for. That
// is the whole point of issue #73: the gate used to commit to a cancel it had
// not yet attempted.
//
// Everything runs on the UI thread, from the navigation callbacks, so the
// fields need no lock.

// cancelledNavSlots is how many cancelled navigations can be outstanding at
// once. It is a bound, not a capacity estimate: exceeding it is reported, on
// both halves of the ledger.
//
// Four, and the reason is a shape rather than a measurement. put_Cancel is
// issued while NavigationStarting is being handled, but the OperationCanceled
// completion that clears the entry is a separately queued event, so nothing
// stops the runtime dispatching a second start before the first completion is
// delivered. How many can stack up that way has never been measured here; four
// is room for a couple of them plus the runtime's own retry.
//
// An earlier version of this comment said decisions/0021's live probe had
// measured two navigations outstanding at once. It had not: 0021 records "the
// second starting right after the first's failure completion", which is
// strictly sequential and is the single-slot premise holding, not failing.
const cancelledNavSlots = 4

// rememberCancelledNavigation enters a confirmed cancel whose id getter
// succeeded. Runtime callbacks use rememberCancelledNavigationObserved so a
// getter failure is never silently promoted to known zero.
func (host *Host) rememberCancelledNavigation(navigationID uint64) {
	host.rememberCancelledNavigationObserved(knownNavigationIdentity(navigationID))
}

func (host *Host) rememberCancelledNavigationObserved(identity navigationIdentity) {
	if !identity.exact() {
		// Anonymous credits suppress only the documented cancelled completion;
		// they never confer restoration authority. Preserve the known-zero /
		// unavailable tag so overlapping callbacks cannot cross-spend credits.
		if host.cancelledNavAnonymous >= cancelledNavSlots {
			host.log.Warn("mullion: cancelled navigation forgotten, ledger full and it has no id")
			return
		}
		host.cancelledNavAnonymous++
		if !identity.known {
			host.cancelledNavUnknown++
		}
		return
	}
	navigationID := identity.value
	for _, id := range host.cancelledNavIDs {
		if id == navigationID {
			return
		}
	}
	for i, id := range host.cancelledNavIDs {
		if id == 0 {
			host.cancelledNavIDs[i] = navigationID
			return
		}
	}
	// Commit the eviction before reporting it. Logger is embedder code and may
	// pump a nested navigation callback; the nested callback must observe the
	// new dense ledger rather than evict the same entry again.
	evicted := host.cancelledNavIDs[0]
	copy(host.cancelledNavIDs[:], host.cancelledNavIDs[1:])
	host.cancelledNavIDs[len(host.cancelledNavIDs)-1] = navigationID
	host.log.Warn("mullion: cancelled navigation forgotten, ledger full, id=" + formatUint64(evicted))
}

// noteGateCancelledOutcome intercepts a completion whose id getter succeeded.
// Runtime callbacks use the tagged variant to preserve getter provenance.
func (host *Host) noteGateCancelledOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	return host.noteGateCancelledOutcomeObserved(success, status, knownNavigationIdentity(navigationID))
}

func (host *Host) noteGateCancelledOutcomeObserved(
	success bool,
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) bool {
	if !host.takeCancelledNavigationObserved(status, identity) {
		return false
	}
	if success {
		// A successful commit supersedes the visible surface regardless of
		// identity. Commit the revocation before Logger can re-enter.
		host.clearErrorSurfaceSuspension()
		host.log.Warn("mullion: cancelled navigation committed anyway, the cancel did not take, " +
			navigationIdentityField(identity))
		return false
	}
	// Suppression and restoration are deliberately different authorities.
	// Anonymous/zero credit may consume an expected completion, but only an
	// exact non-zero identity may restore the suspended surface. This split is
	// load-bearing when an older anonymous completion re-enters after a newer
	// anonymous departure has replaced the suspension.
	host.restoreErrorSurfaceAfterDeparture(identity)
	host.log.Debug("mullion: cancelled navigation completed, " +
		navigationFailureIdentityFields(status, identity))
	return true
}

// takeCancelledNavigation removes a completion whose id getter succeeded.
func (host *Host) takeCancelledNavigation(status webview2.WebErrorStatus, navigationID uint64) bool {
	return host.takeCancelledNavigationObserved(status, knownNavigationIdentity(navigationID))
}

func (host *Host) takeCancelledNavigationObserved(
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) bool {
	if identity.exact() {
		return host.takeCancelledNavigationID(identity.value)
	}
	if status != webview2.WebErrorStatusOperationCanceled || host.cancelledNavAnonymous == 0 {
		return false
	}
	if identity.known {
		// Known zero may spend only a known-zero credit.
		if host.cancelledNavAnonymous == host.cancelledNavUnknown {
			return false
		}
	} else {
		// A getter failure may spend only an unavailable-id credit.
		if host.cancelledNavUnknown == 0 {
			return false
		}
		host.cancelledNavUnknown--
	}
	host.cancelledNavAnonymous--
	return true
}

// takeCancelledNavigationID is the identity-only half of the ledger. Callers
// use it when GetNavigationID succeeded but GetWebErrorStatus did not: a known
// non-zero id remains sufficient evidence without inventing a status value.
func (host *Host) takeCancelledNavigationID(navigationID uint64) bool {
	if navigationID == 0 {
		return false
	}
	for i, id := range host.cancelledNavIDs {
		if id == navigationID {
			copy(host.cancelledNavIDs[i:], host.cancelledNavIDs[i+1:])
			host.cancelledNavIDs[len(host.cancelledNavIDs)-1] = 0
			return true
		}
	}
	return false
}

func navigationIdentityField(identity navigationIdentity) string {
	if !identity.known {
		return "id=unavailable"
	}
	return "id=" + formatUint64(identity.value)
}

func navigationFailureIdentityFields(
	status webview2.WebErrorStatus,
	identity navigationIdentity,
) string {
	return "status=" + formatInt32(int32(status)) + ", " + navigationIdentityField(identity)
}
