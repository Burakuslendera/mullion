# 0054. The asset callback pins the WebView2 environment it borrows for its whole duration

**Status:** Accepted

## Context

The embedded-asset callback receives its `ICoreWebView2Environment` as an
uncounted copy of the interface the `Browser` stores. Between that handoff and
the callback's last environment COM call sit two pieces of embedder code: the
caller's `fs.FS`, which `resolve` reads, and every `logSink` line, which reaches
the caller's `Logger` — arbitrary code that may pump messages through a
MessageBox or a GUI toolkit's own loop
([decision 0026](./0026-navigation-failure-level-follows-classification.md)).
The codebase treats that reentrancy as a condition to survive, not as
impossible.

A nested pump can dispatch `WM_CLOSE`/`WM_DESTROY`. `WM_DESTROY` runs the
window's teardown, `ShuttingDown` drops the Browser-owned environment
reference, and when the pump returns, the still-active outer callback calls
`CreateWebResourceResponse` — and the fail-closed block of
[decision 0053](./0053-asset-callback-failure-installs-a-blocking-response.md)
calls it a second time — through a pointer whose authorizing reference the
process itself has since released (issue #161). Microsoft's rules for managing
reference counts are explicit that a local copy of an internally stored
interface pointer must be `AddRef`ed whenever another function can destroy the
stored copy while the local one is still in use; the schedule above is exactly
that case, self-inflicted. The event handler and `WM_DESTROY` run on the same
UI thread, so nothing can interleave between the raw retrieval and the pin —
but everything after the first Logger or `fs.FS` line can.

The reference-ownership violation and the re-entrant schedule are confirmed
from source and repository contracts. Whether the Runtime retains another
reference in this exact schedule, whether the environment object is actually
destroyed, and whether the call faults, hangs, or fails cleanly were not
observed: no supported Runtime was made to crash or corrupt memory. The
priority ladder classifies a COM lifetime bug as P0 regardless, because the
violation is provable and the failure mode is memory corruption inside the
runtime process.

## Decision

The asset callback owns an environment reference of its own for its whole
duration. It takes the reference with `AddRef` on entry — before any embedder
code runs — and releases it after the deferred fail-closed finish, so the
served path and the blocking answer of decision 0053 alike call a live
interface, and no exit (normal, error, panic, or escalated) strands a
reference. The retrieval in the `WebResourceRequestedCallback` stays a raw
copy, and nothing between that retrieval and the pin may pump messages.

The pin composes with the fail-closed rule instead of weakening it: a nested
teardown no longer changes what the callback can still answer. A request whose
served response fails after a re-entrant close is still answered by the
deterministic blocking response, built through the pinned environment, and the
terminal escalation keeps the refusal 0053 already defines for a browser that
is shutting down.

## Alternatives rejected

- **Defer Logger delivery and diagnostics until after the event returns.**
  Changes the public reentrancy contract every existing callback lives under
  (0026's ordering rules are written on the assumption that Logger lines run
  inline), reorders every diagnostic relative to the state transitions it
  describes, and does not cover the `fs.FS` seam, which is equally
  caller-provided and equally reachable. The pin costs two COM calls; the
  deferral costs a contract.
- **Suppress nested teardown while an asset callback is on the stack.**
  Requires a reentrancy flag threaded through the window procedure and a
  deferred teardown re-entry, to protect one callback — and the callback would
  then complete against a window that is half closed. Deferring the *effect*
  of WM_DESTROY behind an in-flight callback is a larger invariant than the
  bug needs, and it moves the ownership problem instead of closing it.
- **Generation counter / lifecycle recheck before each environment call.**
  Converts one COM rule into a Mullion-specific protocol that every future
  environment call site must remember to consult. The pin is local to the one
  function that holds the pointer and fails closed by construction: a missing
  Release is a leak, not a use-after-release.
- **Make `Browser.Environment` return an owned reference to every caller.**
  Strictly correct by Microsoft's rule, but it changes the accessor's contract
  for every current and future caller and moves the balancing `Release` into
  the one closure no headless test can execute. The pin lives inside the
  callback instead, where the counted fake suite can assert the exact
  AddRef/Release schedule on every exit.

## Consequences

- Every asset request costs one `AddRef`/`Release` pair on the environment.
  The pair is balanced by construction and asserted by test; the cost is a
  pair of atomic increments per request.
- `ICoreWebView2Environment` grows exported `AddRef`/`Release` methods. Any
  future code that keeps an environment pointer across embedder code must use
  them; code that keeps it across a pumping boundary must take the reference
  before the first pump.
- The raw `Environment()` handoff into the asset callback is now a documented
  adjacency contract: the retrieval and the pin must stay on the same thread
  with no message pumping between them. Moving the retrieval somewhere that
  pumps first reintroduces issue #161.
- The lifetime guarantee remains contract-based, not live-proven: the tests
  prove the reference schedule on a counted fake, not the Runtime's refcount
  behavior. That live proof ceiling stays with
  [issue #161](https://github.com/Burakuslendera/mullion/issues/161)'s own
  terms — an audit-owned live check may record callback chronology and
  HRESULTs on a supported Runtime, and must not corrupt memory or kill an
  unrelated process.

## What would change our mind

- A Runtime is observed destroying the environment object on controller
  teardown even while a caller holds a reference of its own — that would mean
  the pin cannot protect the blocking answer, and the fail-closed path would
  need an environment created outside the teardown's reach.
- Microsoft documents that nested message loops inside WebView2 event handlers
  are safe and that the Runtime pins the environment for the handler's
  duration. The pin would then be redundant, and could be retired as
  documentation-only.
- A measured cost from the per-request reference pair shows up in real
  startup profiles. The design would still stand; the pair would move to a
  per-Embed pin with an explicit reentrancy recheck instead.

## Evidence

Fix commit on branch `fix/issue-161-asset-env-lifetime`. The counted fake
environment gained `AddRef`/`Release` slots and a chronology recorder, and
`host/asset_environment_windows_test.go` pins the schedule:
`TestAssetCallbackPinsEnvironmentAcrossNestedTeardownFromLogger` runs the
issue's headline schedule (Logger line pumps the teardown effect mid-callback,
the outer callback still serves its own 200, chronology opens with the pin);
`TestAssetCallbackAnswersFailClosedWhenTeardownPrecedesResponseCreation` runs
the teardown from the `fs.FS` seam and fails the served build, so the blocking
500 of decision 0053 must still answer through the pinned environment;
`TestAssetCallbackServesWhenDeliveredAfterTeardown` covers late delivery after
the authorizing reference is already released;
`TestAssetCallbackSurvivesPanicWithThePinBalanced` covers the panic exit;
`TestAssetCallbackWithoutEnvironmentNeverTouchesTheReference` pins the nil
side. Every create snapshot asserts the callback's own reference was
outstanding at the vtable call. The existing fail-closed suite
(issue #150) passes unchanged against the pinned callback.

Mutant run, 2026-09-12: reverting the pin (removing the `AddRef`/`Release`
pair from `webResourceRequested`) while keeping the tests fails all four pin
tests at their first assertion (`environment AddRef calls = 0, want exactly
1`). The pre-fix code answers every blocking and serving question but runs the
environment's vtable with no reference of its own, which is the violation the
tests make observable.

> Last updated: 2026-09-12 | Editor: ZCode (GLM-5.3-Flash) | Change: record the asset callback's pinned environment reference and the adjacency contract it imposes (issue #161).
