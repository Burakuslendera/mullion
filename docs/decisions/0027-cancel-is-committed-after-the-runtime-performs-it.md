# 0027. A navigation cancel is committed only after the runtime has performed it

**Status:** Accepted

## Context

The `PinNavigationToOrigin` gate (0023) decided to cancel a navigation, wrote the
navigation's id down so its completion would not be read as a load failure, and
handed the target to the system browser — all of it before `put_Cancel` was
attempted, and it never learned whether the cancel took. Three defects share
that root (issue #73).

**It failed open.** `put_Cancel` returning a failure means the navigation goes
ahead. The foreign document then loads into the WebView — the thing the gate
exists to prevent — with the bridge injected into it; the same target is *also*
opened in the system browser, so it opens twice; and the document's own
completion, reporting success under the recorded id, is consumed as though the
navigation had been abandoned, skipping the bounds sync, the diagnostic eval and
the error-surface machine. A start navigation redirected off-origin would be
cancelled, its completion swallowed, and the startup show gate never released:
the window stays hidden until the render watchdog times out.

0022 already had the symmetric guard on the sibling path — if `PutHandled`
fails, `routeNewWindow` does not route, because the runtime is opening its own
window. 0023 had no equivalent, and it mattered more here, because 0023's own
Evidence marks "that `put_Cancel` actually abandons it" as `unverified`.

**One slot was not enough.** The id lived in a single field, so a second cancel
evicted the first, and the evicted navigation's `OperationCanceled` completion
fell through to the error-surface machine, armed it, and replaced the live
frontend with the fallback page — precisely the failure the id consumption was
added to prevent. The code's premise, that "a top-frame navigation completes
before the next starts", is contradicted by 0021's own live probe, which watched
the runtime start a second navigation of its own after the first ended.

**A failed `GetUri` pushed the gate silently.** The URI getter's error was
discarded where the sibling id getter's was reported. An empty URI is no
origin's, so the gate cancelled a navigation it could not read, and
`isExternalBrowserSafe("")` is false, so it dropped it without routing: a dead
link, reported as an "unsupported scheme", with one debug line to show for it.

## Decision

**A cancel is committed only after the runtime has performed it.** The
`internal/webview2` layer asks the host whether to cancel, attempts
`put_Cancel`, and only on success calls a second callback,
`NavigationCancelledCallback`, where the host does everything that follows from
a cancel. A failed attempt warns and tells nobody: the navigation is going
ahead, so it must reach the host as an ordinary navigation. This is 0022's
`PutHandled` guard, applied to the path that lacked it.

The host side splits accordingly. `shouldCancelNavigation` is a decision and
nothing else — no state written, no target routed, no line logged.
`noteNavigationCancelled` is the commit.

**Outstanding cancels are a small bounded ledger, not a slot.** Four entries,
appended newest-last so position is age, deduplicated by id because a redirect
reuses its navigation's id. Only a completion removes an entry; when a fifth
cancel needs room the oldest is evicted, and the eviction is logged at warn,
because the evicted navigation reverts to the pre-issue-73 behaviour and nothing
else could say which one it was.

**A cancel with no id is counted, and matched by order.** Identity is what the
ledger matches on; without it the only evidence left is order, so an id-less
`OperationCanceled` completion arriving while an id-less cancel is outstanding
is taken as its cleanup. That is decision 0020's trade in a different place. The
count is bounded by the same four, because nothing but a matching completion
ever decrements it.

**A completion that reports success is not consumed.** It means the cancel did
not take: a document loaded, and it needs the bounds sync, the diagnostic eval
and the machine. It is reported at warn, and it is the line that would disprove
0023's `unverified` premise.

**An unreadable URI is cancelled, and said out loud.** Fail-closed, because a
gate that lets through what it cannot identify is not a gate — but it may have
just killed a legitimate in-origin navigation, and no one downstream can tell,
so it warns instead of being filed under "unsupported scheme". The layer reports
the getter failure alongside it.

## Alternatives rejected

**Keep the single slot and accept eviction.** The measured shape (0021's two
navigations for one action) makes two outstanding cancels ordinary rather than
pathological, and the failure mode is the frontend being replaced by the error
page. Rejected on the evidence that already existed.

**An unbounded map of outstanding ids.** Simpler to write and correct for every
ordering. Rejected because nothing but a completion removes an entry, and a
cancelled navigation whose completion never arrives is exactly the case that
cannot be ruled out here — an unbounded structure fed by content-driven
navigation is a slow leak with a hostile page for a pump.

**Refuse to cancel when the runtime gives no id.** It would make the ledger
exact. Rejected because it fails open on the containment the gate exists for:
an unidentifiable navigation would be the one that gets through.

**Let the webview2 layer route the target itself.** It knows the cancel
succeeded, so it could hand the URI over directly and the host would need no
second callback. Rejected: the layer has no policy — `isExternalBrowserSafe`,
the trusted origin, the `openExternal` seam the suite stubs (issue #76) are all
host concerns, and moving them down would put a `ShellExecute` inside the COM
binding.

**Have `NavigationCancelledCallback` return whether the host accepted, and let
the layer act on it.** A round trip with nothing to do at the end of it: the
cancel has already happened by then, so there is no decision left to make.

**Fail open on an unreadable URI** — let the navigation through when the target
cannot be read. Rejected as above: it is the one case where an attacker-shaped
event args would buy a bypass, and the gate is opt-in, so its user asked for
containment over availability.

## Consequences

**A failed `put_Cancel` is now a visible, ordinary navigation.** The foreign
document still loads — nothing here can stop that, the runtime refused — but it
is no longer also opened in the browser, no longer hidden from the error-surface
machine, and the startup show gate still releases. The warning names it.

**`Browser` grew a callback.** `NavigationCancelledCallback` is the second half
of the navigation-cancel contract, and a host that sets `NavigationStartingCallback`
to return true without setting it will cancel navigations and commit to none of
them. The doc comment says so; there is no way to enforce it in the type.

**Four is a bound, not a capacity.** Exceeding it is reported, and the reported
case behaves as it did before this record. If it is ever seen in a real log the
answer is to find out why four cancels were outstanding, not to raise the number.

**The id-less branch can mistake a superseded navigation's cancel for the
gate's.** It consumes an id-less `OperationCanceled` while an id-less cancel is
outstanding, and a superseded surface `Navigate` produces exactly that status.
The consequence is a skipped cleanup, never an armed surface — the direction
that matters — and it only exists when `GetNavigationID` is failing.

**One log line moved level.** An off-origin cancel with an unreadable target is
warn where it was debug, so it is now visible to an embedder that drops debug.

## What would change our mind

- A live `cancelled navigation committed anyway` line would settle 0023's
  `unverified` premise the wrong way, and the gate would need a second mechanism
  — there is no third thing to try on `NavigationStarting`.
- A `cancelled navigation forgotten, ledger full` line would mean four cancels
  really can be outstanding, and the bound would need to be understood before it
  is changed.
- `GetNavigationID` failing in the field would make the id-less branch the
  common path rather than the degraded one, at which point matching by order is
  not good enough and the gate needs its own identity.

## Evidence

Commit on branch `fix-73-cancel-after-confirmation`. Locked by
`host/navigationcancel_windows_test.go` — the decision commits to nothing, the
ledger holds four out-of-order completions, an eviction is reported, an id-less
cancel is recognised exactly once, an id-less *failure* is not mistaken for one,
and an unreadable target is cancelled loudly and never routed — plus the gate
cases in `host/systembrowser_windows_test.go`, now driven through both halves,
and the cancel-did-not-take line in
`host/errorsurface_logging_windows_test.go`.

The layer's own half — cancel before notify, no notify on failure, both getter
errors reported — is guarded by reading the source
(`internal/webview2/browser_events_source_test.go`), because the handler is a
closure over a live COM event-args object that no test here can construct. That
is the same gap, and the same answer, as issue #79's completion callback.

Nine mutants, each killed by the case that owns it: committing during the
decision; a single slot; a silent eviction; the id-less early return that was
the old behaviour; the id-less branch without its status check; consuming a
successful completion; an unreadable URI filed as an unsupported scheme; the
layer notifying before it cancels; and the layer discarding the URI getter's
error.

Not verified live. The failure this record is mostly about — `put_Cancel`
returning an error — has never been observed and cannot be provoked from
outside; what a live run can confirm is that the ordinary cancel path still
cancels, still routes and still leaves the frontend alone, which is the existing
`PinNavigationToOrigin` item in [verification.md](../verification.md).

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: new record - a cancel is committed only after put_Cancel succeeds, outstanding cancels are a bounded ledger rather than one slot, and an unreadable target is cancelled loudly (issue #73, closing the fail-open half of 0023).
