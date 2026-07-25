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

// rememberCancelledNavigation enters a confirmed cancel in the ledger.
//
// The live entries are a dense prefix, oldest first: takeCancelledNavigation
// closes the gap when it removes one, so the first empty slot is always the end
// and an entry's position really is its age. Getting that wrong is not
// cosmetic - an earlier version zeroed in place and shifted unconditionally,
// which evicted a live entry while three slots stood empty and told the reader
// the ledger was full.
//
// An id already present is left alone. That covers a redirect, which the
// runtime is documented to run under its navigation's original id, so a chain
// would otherwise book one entry per hop. The branch is defensive rather than
// load-bearing: an abandoned navigation should produce no further hop at all,
// and the id-sharing itself is `unverified` in decisions/0023.
func (host *Host) rememberCancelledNavigation(navigationID uint64) {
	if navigationID == 0 {
		// No identity to match on. Count it instead, and stop counting at the
		// bound - nothing but a matching completion ever decrements this, and a
		// completion that never comes must not make the count grow forever.
		if host.cancelledNavAnonymous >= cancelledNavSlots {
			host.log.Warn("mullion: cancelled navigation forgotten, ledger full and it has no id")
			return
		}
		host.cancelledNavAnonymous++
		return
	}
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
	// Genuinely full: every slot holds a cancel whose completion has not
	// arrived. The oldest is dropped, and that one navigation reverts to the
	// pre-issue-73 behaviour - its completion reaches the error-surface machine
	// and may arm the fallback - so the bound being reached is itself the news.
	//
	// The warning comes after the writes, and that order is load-bearing for the
	// reason armErrorSurface gives (decisions/0026): the Logger is embedder code,
	// and one that pumps messages runs a queued navigation event inside this
	// call. Logging first let the nested call see the pre-shift array, name the
	// same id a second time, and shift again - losing an entry that no line ever
	// named.
	evicted := host.cancelledNavIDs[0]
	copy(host.cancelledNavIDs[:], host.cancelledNavIDs[1:])
	host.cancelledNavIDs[len(host.cancelledNavIDs)-1] = navigationID
	host.log.Warn("mullion: cancelled navigation forgotten, ledger full, id=" + formatUint64(evicted))
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
// and reports whether it was there. Removing closes the gap, which is what keeps
// the live entries a dense, age-ordered prefix for rememberCancelledNavigation.
//
// With an id the match is positive and the status is not consulted: the id is
// proof enough, and requiring a particular status would fail open the day the
// runtime picks a different one. Without an id there is only order, so the
// match is narrowed to the status a cancel is documented to produce - which
// keeps an ordinary id-less failure from being mistaken for a cancel that is
// still outstanding.
//
// Identity here is a property of the *event*, not of the navigation: the id is
// read separately at the start and at the completion, and either read can fail
// on its own. A start that had an id and a completion that does not - or the
// reverse - therefore never match, and the entry is stranded until the bound
// evicts it while the completion goes to the error-surface machine. Absent
// identity the id-less credit can also be spent on the wrong completion, and
// then the right one arms the surface instead. Both are the pre-issue-73
// behaviour for one navigation, they are bounded, and they exist only while
// GetNavigationID is failing - but the earlier claim that this branch can only
// ever cost a skipped cleanup was wrong, and it is corrected here.
func (host *Host) takeCancelledNavigation(status webview2.WebErrorStatus, navigationID uint64) bool {
	if navigationID != 0 {
		for i, id := range host.cancelledNavIDs {
			if id == navigationID {
				copy(host.cancelledNavIDs[i:], host.cancelledNavIDs[i+1:])
				host.cancelledNavIDs[len(host.cancelledNavIDs)-1] = 0
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
