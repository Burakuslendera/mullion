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
the error-surface machine.

0022 already had the symmetric guard on the sibling path — if `PutHandled`
fails, `routeNewWindow` does not route, because the runtime is opening its own
window. 0023 had no equivalent, and it mattered more here, because 0023's own
Evidence marks "that `put_Cancel` actually abandons it" as `unverified`.

**One slot was not enough.** The id lived in a single field, so a second cancel
evicted the first, and the evicted navigation's `OperationCanceled` completion
fell through to the error-surface machine, armed it, and replaced the live
frontend with the fallback page — precisely the failure the id consumption was
added to prevent.

**A failed `GetUri` pushed the gate silently.** The URI getter's error was
discarded where the sibling id getter's was reported. An empty URI is no
origin's, so the gate cancelled a navigation it could not read, and
`isExternalBrowserSafe("")` is false, so it dropped it without routing: a dead
link, reported as an "unsupported scheme", with one debug line to show for it.

Two claims that were in this record's first draft, and in issue #73's body
before it, do not survive a read of the code and are withdrawn:

- **"the startup show gate never releases, so the window stays hidden until the
  render watchdog times out."** The render watchdog never shows the window
  (`render_watchdog_windows.go` only logs). The show gate has its own fallback
  and fires after `Config.ShowTimeout`, and the code this record documents says
  so in as many words — `handleNavigationOutcome` releases the gate "so the
  surface appears now *instead of after* `Config.ShowTimeout`". A swallowed
  completion delays the window; it does not strand it.
- **"0021's live probe watched the runtime start a second navigation of its own
  after the first ended, so a top-frame navigation completing before the next
  starts is not a rule."** 0021 records "consecutive ids, the second starting
  right after the first's failure completion" — strictly sequential, which is
  that rule *holding*. See the Decision for the argument that does hold.

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

**Outstanding cancels are a small bounded ledger, not a slot.** Not because
anything has measured two cancels outstanding at once, but because nothing
prevents it: `put_Cancel` is issued while `NavigationStarting` is being handled,
while the `OperationCanceled` completion that clears the entry is a separately
queued event, so the runtime is free to dispatch a second start first. Four
entries; the live ones are kept a dense, oldest-first prefix, so a consumed slot
is reused and an eviction happens on occupancy rather than on position. A
redirect reuses its navigation's id, so an id already present is left alone —
defensively: the id-sharing is `unverified` in 0023, and an abandoned navigation
should produce no further hop to dedupe.

**Both halves of the ledger report what they drop.** Evicting the oldest entry,
and saturating the id-less count, each mean one navigation reverts to the
pre-issue-73 behaviour, and nothing else could say which.

**A cancel with no id is counted, and matched by order.** Identity is what the
ledger matches on; without it the only evidence left is order and the status, so
an id-less `OperationCanceled` completion arriving while an id-less cancel is
outstanding is taken as its cleanup. That is decision 0020's trade in a
different place, and it is weaker than the first draft of this record claimed —
see Consequences.

**A completion that reports success is not consumed.** It means the cancel did
not take: a document loaded, and it needs the bounds sync, the diagnostic eval
and the machine. It is reported at warn, and it is the line that would disprove
0023's `unverified` premise.

**An unreadable URI is cancelled, and said out loud.** Fail-closed, because a
gate that lets through what it cannot identify is not a gate — but it may have
just killed a legitimate in-origin navigation, and no one downstream can tell,
so it warns instead of being filed under "unsupported scheme". The layer reports
the getter failure alongside it, and names the navigation in the failed-cancel
warning too, because a bare HRESULT says neither that a cancel failed nor which
navigation it was.

## Alternatives rejected

**Keep the single slot.** The failure mode is the live frontend being replaced
by the error page, and the precondition is only that two cancels overlap — which
the queueing above allows even though no run has shown it. Rejected on the
structure, not on a measurement.

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
second callback. Rejected because the policy is all host-side —
`isExternalBrowserSafe`, the trusted origin, the `openExternal` seam the suite
stubs (issue #76) — and moving it down would put a `ShellExecute` inside the COM
binding. The layer is not policy-free (its unconditional `PutHandled` is a
policy, and this record cites it as precedent); it is free of *this* policy.

**Have `NavigationCancelledCallback` return whether the host accepted, and let
the layer act on it.** A round trip with nothing to do at the end of it: the
cancel has already happened by then, so there is no decision left to make.

**Fail open on an unreadable URI.** Rejected as above: it is the one case where
an attacker-shaped event args would buy a bypass, and the gate is opt-in, so its
user asked for containment over availability.

## Consequences

**A failed `put_Cancel` is now a visible, ordinary navigation.** The foreign
document still loads — nothing here can stop that, the runtime refused — but it
is no longer *also* opened in the browser, no longer hidden from the
error-surface machine, and the warning names the navigation. That is the
HRESULT-failure case. The other one — `put_Cancel` returns success and the
navigation commits anyway — cannot be caught in advance: by the time the
completion says so, the ledger entry and the browser tab already exist. The
`cancelled navigation committed anyway` warn is the detection, not a prevention.

**`Browser` grew a callback.** `NavigationCancelledCallback` is the second half
of the navigation-cancel contract, and a host that sets
`NavigationStartingCallback` to return true without setting it will cancel
navigations and commit to none of them. There is no way to enforce that in the
type; a source guard checks the one caller in this repository.

**Four is a bound, not a capacity.** Reaching it is reported on both halves, and
the reported navigation behaves as it did before this record.

**The id-less branch is weaker than "it can only cost a skipped cleanup".** That
was the first draft's claim and it is wrong across a pair of completions:
spending the one credit on the wrong id-less `OperationCanceled` leaves the
gate's own completion unconsumed, and *that* arms the surface. The same goes for
identity disagreeing between a start and its completion — the id is read
separately at each, and either read can fail — which strands an entry until the
bound evicts it while its completion goes to the machine. All of it is bounded,
all of it reverts one navigation to the pre-issue-73 behaviour, and none of it
exists unless `GetNavigationID` is failing.

**The ledger is never reset.** Nothing clears it on teardown, process-failed or
re-embed, so a stranded id-less credit can be spent by a much later completion.
Bounded by the same four; the cost is a misreported line and a skipped bounds
sync, not a stuck state.

**An unreadable URI during the startup navigation costs the window its prompt
appearance.** The gate cancels it, the ledger consumes its completion, and the
show gate is released by `Config.ShowTimeout` instead of by the frontend — the
window appears seconds late rather than at once. That is the price of
fail-closed, and it is only reachable when `GetUri` fails.

**Two log lines moved level, and one appears with the gate off.** An off-origin
cancel with an unreadable target is warn where it was debug; and the URI
getter's failure is now reported whatever the gate is set to, because a failing
COM getter is news on its own.

**The gate is still fail-open on an unreadable URI in one place, and it is not
this one.** The error-surface claim runs *before* the gate and accepts an empty
URI as a tolerated form of the surface's own start (0021), so an unreadable
start inside the claim window is claimed and never reaches the gate at all. It
is not content-reachable — `GetUri` has to fail — but the two halves of the same
callback now answer the same question in opposite directions.

## What would change our mind

- A live `cancelled navigation committed anyway` line would settle 0023's
  `unverified` premise the wrong way, and the gate would need a second mechanism
  — there is no third thing to try on `NavigationStarting`.
- A `cancelled navigation forgotten` line on either half would mean four cancels
  really can be outstanding at once, which nothing has yet shown; the bound
  should be understood before it is changed.
- `GetNavigationID` failing in the field would make the id-less branch the
  common path rather than the degraded one, at which point matching by order is
  not good enough and the gate needs its own identity.

## Evidence

Commit on branch `fix-73-cancel-after-confirmation`. Locked by
`host/navigationcancel_windows_test.go` — the decision commits to nothing, the
ledger holds four out-of-order completions, a consumed slot is reused before
anything is evicted, an eviction drops the *oldest* entry and says so, both
halves report what they forget, repeated starts under one id are one entry, an
id-less cancel is recognised exactly once, an id-less *failure* is not mistaken
for one, the two halves never cross-match, the ledger is consulted whatever the
surface is doing, and an unreadable target is cancelled loudly and never routed
— plus the gate cases in `host/systembrowser_windows_test.go`, driven through
both halves from `noteAndGateNavigation`, and the cancel-did-not-take line in
`host/errorsurface_logging_windows_test.go`.

Two things no behavioural test in this suite can execute are guarded by reading
the source: the layer's ordering (`internal/webview2/browser_events_source_test.go`)
and the wiring that connects the two halves inside `createWebView`
(`host/navigation_report_source_test.go`). Both closures need a live WebView2.

**This record's first version was audited by eight reviewers and did not
survive it.** The ledger they were given evicted by position rather than
occupancy — it dropped a live entry with three slots standing empty and called
itself full — and it logged that eviction *before* rewriting the array, which is
the re-entrant-Logger ordering decision 0026 exists to forbid. Of 29 mutants run
against the tests, 12 survived, including the whole pre-issue-73 shape restored
in the wiring, a commit added one level above where the decision test probed,
and four separate ways past the layer's source guard. Every one of those is now
killed, and the guards read a comment-stripped body, scope the `return` to the
error branch it belongs to, and count the call sites they cannot see into.

Verified live, `examples/basic` with `PinNavigationToOrigin` on, runtime
150.0.4078.83, two runs on 2026-07-25. Nine off-origin navigations were
cancelled across the two: each logged `navigation cancelled off origin, routed
to system browser`, opened in the system browser, and completed with
`cancelled navigation completed, status=14` — consumed, never fed to the
error-surface machine. The frontend stayed where it was every time, the fallback
surface never appeared, and both runs ended `SessionWarnCount=0,
SessionErrorCount=0`.

**That is also the first evidence for 0023's `unverified` premise.** 0023 could
not say whether `put_Cancel` actually abandons the navigation. This record adds
the observer that would say otherwise — a cancelled navigation completing with
`success` warns `cancelled navigation committed anyway` — and across nine
cancels it never fired: all nine reported `OperationCanceled`. Not a proof, but
it is the difference between an assumption and an assumption with something
watching it.

`cancelled navigation forgotten` never fired either, on either half, so nothing
in those runs had four cancels outstanding at once — consistent with the
ordering the ledger was widened against being rare rather than absent.

What could not be exercised live is the failure this record is mostly about:
`put_Cancel` returning an error has never been observed and cannot be provoked
from outside.

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: new record - a cancel is committed only after put_Cancel succeeds, outstanding cancels are a bounded ledger rather than one slot, and an unreadable target is cancelled loudly (issue #73, closing the fail-open half of 0023). Rewritten the same day after an eight-agent audit: the ledger evicts on occupancy and logs after it writes, both halves report what they drop, and four claims the first draft inherited or invented - the startup-show-gate chain, 0021's probe as evidence for concurrent cancels, the id-less branch's safety, and the double-open being gone - are withdrawn or corrected.
