//go:build windows

package host

import "github.com/Burakuslendera/mullion/internal/webview2"

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
// once. Four, because the live probe behind decisions/0021 watched the runtime
// run two navigations for one user action, and a redirect chain plus a
// runtime-initiated retry is the worst shape anyone has measured. It is a
// bound, not a capacity estimate: exceeding it is reported.
const cancelledNavSlots = 4

// rememberCancelledNavigation enters a confirmed cancel in the ledger.
//
// Position is age: the newest entry is appended at the end and the oldest falls
// off the front. A redirect reuses its navigation's id, so a redirect chain is
// one entry, not one per hop.
func (host *Host) rememberCancelledNavigation(navigationID uint64) {
	if navigationID == 0 {
		// No identity to match on. Count it instead, and stop counting at the
		// bound - nothing but a matching completion ever decrements this, and a
		// completion that never comes must not make the count grow forever.
		if host.cancelledNavAnonymous < cancelledNavSlots {
			host.cancelledNavAnonymous++
		}
		return
	}
	for _, id := range host.cancelledNavIDs {
		if id == navigationID {
			return
		}
	}
	if evicted := host.cancelledNavIDs[0]; evicted != 0 {
		// Four cancels outstanding at once, none of them completed. Whatever is
		// dropped here reverts to the pre-issue-73 behaviour for that one
		// navigation: its completion reaches the error-surface machine and may
		// arm the fallback. Say so - the bound being reached is itself the news.
		host.log.Warn("mullion: cancelled navigation forgotten, ledger full, id=" + formatUint64(evicted))
	}
	copy(host.cancelledNavIDs[:], host.cancelledNavIDs[1:])
	host.cancelledNavIDs[len(host.cancelledNavIDs)-1] = navigationID
}

// noteGateCancelledOutcome intercepts the completion of a navigation the gate
// cancelled (decisions/0023). The runtime completes a put_Cancel'd navigation
// with OperationCanceled; that is not a load failure - the cancel was
// deliberate, the target was routed elsewhere, and the current document stays -
// so this completion must not be reported as a failure, resynced or fed to the
// error-surface machine, which would read a foreign failure as "navigate to the
// fallback surface" and tear down the live frontend.
//
// It reports whether it consumed the completion. Consuming means the navigation
// really was abandoned; a completion that reports success did not abandon
// anything, and that is now detected rather than swallowed (issue #73). Either
// way the entry is taken out of the ledger: there is nothing left to wait for.
func (host *Host) noteGateCancelledOutcome(success bool, status webview2.WebErrorStatus, navigationID uint64) bool {
	if !host.takeCancelledNavigation(status, navigationID) {
		return false
	}
	if success {
		// The runtime accepted put_Cancel and then committed the navigation
		// anyway. decisions/0023 marks "that put_Cancel actually abandons it" as
		// unverified, and this is the line that would prove it wrong. The
		// completion is handed to the normal path, because a document really did
		// load: it needs the bounds sync, the diagnostic eval and the machine.
		host.log.Warn("mullion: cancelled navigation committed anyway, the cancel did not take, id=" + formatUint64(navigationID))
		return false
	}
	host.log.Debug("mullion: cancelled navigation completed, " + navigationFailureFields(status, navigationID))
	return true
}

// takeCancelledNavigation removes this completion's navigation from the ledger
// and reports whether it was there.
//
// With an id the match is positive and the status is not consulted: the id is
// proof enough, and requiring a particular status would fail open the day the
// runtime picks a different one. Without an id there is only order, so the
// match is narrowed to the status a cancel is documented to produce - which
// keeps an ordinary id-less failure from being mistaken for a cancel that is
// still outstanding. The cost of the id-less branch is decision 0020's cost in
// a different place: absent identity, a superseded navigation's own cancel can
// be taken for the gate's. It cannot arm the surface, which is the direction
// that matters.
func (host *Host) takeCancelledNavigation(status webview2.WebErrorStatus, navigationID uint64) bool {
	if navigationID != 0 {
		for i, id := range host.cancelledNavIDs {
			if id == navigationID {
				host.cancelledNavIDs[i] = 0
				return true
			}
		}
		return false
	}
	if host.cancelledNavAnonymous == 0 || status != webview2.WebErrorStatusOperationCanceled {
		return false
	}
	host.cancelledNavAnonymous--
	return true
}
