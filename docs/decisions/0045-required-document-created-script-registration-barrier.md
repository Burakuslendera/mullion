# 0045. Required document-created scripts complete before first navigation and availability

**Status:** Accepted; extends [0016](./0016-single-flight-embed.md)

## Context

`ICoreWebView2.AddScriptToExecuteOnDocumentCreated` accepts a documented
asynchronous completion handler. Starting its four required registrations does
not establish that the first document will receive them. The host also commits a
`Browser` before that wait so that one returned-error owner can tear it down.
Consequently, a non-nil stored Browser is not, during this narrow interval,
permission for a re-entrant `Show` to use it. The older single-flight record
0016 did not include this completion boundary.

A required wait pumps the UI queue. Shutdown, a stale lifecycle transition, and
`WM_QUIT` can therefore occur while it is live. A queue drain can make shutdown
and completion ready in the same turn, so a `select` between them is not a policy:
it is random choice. The required-script wait therefore uses cancellation →
queued `WM_QUIT` → completion → timeout priority and reposts a consumed quit. The
tab-strip mode flag has a real completion handler too, but it is an optional
capability; waiting for it would turn an advisory enhancement into startup
availability.

At the released v0.0.3 baseline `8807ede`, and still at the current main baseline
when Issue #135 was opened, `f7860ae`, the host made four Add calls with nil
handlers, logged registration success, and proceeded without observing any
asynchronous completion. This record defines Issue #135's corrective invariant;
implementation and release status belong to repository and tracker history,
rather than branch-relative decision prose that becomes stale after merge.

## Decision

The host serializes required document-created scripts as bridge Add → bridge
completion → diagnostics Add → diagnostics completion → drag Add → drag
completion → resize Add → resize completion. Failure at step N prevents the Add
for step N+1. It admits neither first `Navigate` nor re-entrant Browser
availability until all four documented completions succeed. Synchronous add
failure, failed or null-result completion, duplicate or stale completion while
the barrier is live, timeout, shutdown, lifecycle invalidation, and `WM_QUIT`
return one primary error through the committed-step teardown owner. The required
wait gives cancellation priority over an already-queued quit, that quit priority
over ready completion, and all three priority over timeout; it reposts the quit.
After every re-entrant startup diagnostic, and immediately before watchdog and
Navigate effects, the owner verifies that the same committed Browser is still
current. Optional tab-strip registration starts nonblocking, may miss the first
document, and retains the classic titlebar fallback when it is late or fails.

For supported-Runtime closure evidence only, the
`mullion_script_completion_delay_diag` build may activate one internal
coordinator. A genuine required-handler `Invoke` first captures the Runtime's
real HRESULT/result, returns without blocking the STA, and only then lets a
bounded goroutine delay publication into this unchanged owner/barrier. The
first two required completions are the only eligible holds; explicit release,
coordinator close, or the ten-second fail-safe ends each hold. The untagged
file is a compile-time-selected no-op implementation. There is no environment
switch, public API, or default behaviour change. Diagnostic marker callbacks
run only through a finite ordered dispatcher with a 100 ms observer timeout;
the observer cannot postpone Close or real-completion publication. After one
timeout later markers are dropped, so at most one arbitrary observer invocation
can remain blocked until it returns or the diagnostic process exits.

## Alternatives rejected

**Navigate after initiation only.** This treats a native request as if it were a
completed registration. A delayed or failed completion could leave the first
document without bridge, diagnostics, drag, or resize behavior, and a queued
quit could incorrectly continue startup.

**Initiate all four, then wait in caller order.** Add calls are asynchronous, and
the dependent scripts return permanently when the bridge namespace is absent.
Microsoft's Win32 guidance says ordered asynchronous work uses callbacks. Batching
therefore leaves Runtime scheduling, rather than the completion boundary, in
charge of whether diagnostics, drag, and resize are registered after bridge.

**Make optional tab-strip registration part of the barrier.** This would provide
a stronger first-document claim for a capability that can be absent, but it
would delay or fail ordinary startup for an advisory feature. The required
scripts and the classic fallback already provide the availability contract.

**Treat `host.browser != nil` as universally usable.** Committing only after the
barrier would split ownership and teardown; allowing re-entrant use after the
early commit would expose a half-configured controller. The existing
single-flight state is the narrow readiness discriminator until the barrier
returns.

## Consequences

Startup has a nested, cancellation-aware UI wait and may reject a re-entrant
`Show` even though a Browser has been committed. Every future first-navigation
route must keep its required registrations, success diagnostics, watchdog, and
Navigate behind this barrier and its current-Browser checks; production-route
mutation guards pin those owners. The cost is intentionally conservative:
shutdown cancels completion and a queued quit cancels a completion that was
otherwise ready, optional tab-strip mode has no first-document guarantee, and
callers must tolerate the classic fallback. The gain is that first-document
requirements and terminal teardown share one explicit owner instead of relying
on scheduling luck.

The tagged evidence artifact is deliberately not a release-equivalent binary.
It does not fabricate/replace a callback, delay Runtime `Invoke` return or
Runtime-owned reference release, change completion data, bypass/reorder the
barrier, or affect optional tab-strip registration. Its run proves only that the
exact source tree survives the forced post-callback timing and lifecycle
interleave. The paired untagged artifact must separately prove ordinary
first-document behavior on the same supported Runtime.

## What would change our mind

- WebView2 could publish a synchronous, failure-reporting required-script API,
  or guarantee completion before the call returns; then the nested barrier and
  its pump policy would be unnecessary.
- A supported Runtime observation could show that the documented completion
  ordering does not govern first-document injection; then this invariant would
  need a different Runtime-backed boundary rather than a stronger local wait.
- A product requirement could make optional tab-strip mode mandatory on the
  first document. It would reopen the trade-off with measured startup latency
  and a replacement fallback policy, not silently promote the optional request.

## Evidence

- Issue #135 defines the required first-document registration and shutdown
  boundary.
- Microsoft's
  [AddScript reference](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.4129.50#addscripttoexecuteondocumentcreated)
  defines the asynchronous readiness boundary; its
  [Win32 ordering guidance](https://learn.microsoft.com/en-us/microsoft-edge/webview2/get-started/win32#if-code-must-be-run-in-order-use-callbacks)
  says ordered asynchronous work uses callbacks and also demonstrates that this
  document must not claim `nullptr` is universally forbidden.
- `internal/webview2/script_completion_windows_test.go` exercises the handler,
  exact same-turn and deadline precedence, serialized Add/completion chain,
  failure-N stop, duplicates, and fake Runtime retention.
- `host/webview_script_registration_windows_test.go` drives the post-Embed
  ordering, logger-triggered teardown, and source mutation guards for the only
  production owners.
- The tag-specific completion tests drive genuine fake-Runtime `Invoke`/release,
  bounded publication, fail-safe timeout, optional exclusion, cancellation,
  abandonment, refcounts, and coordinator single ownership without a window.
- The [paired live record](../issue-135-paired-live-verification.md) records the
  exact source/artifact/log identities, supported Runtime chronology, acceptance
  result and retained Win10, close, Pong, race, scheduling and byte-equivalence
  nonclaims.

The acceptance counters have two explicit owners. Internal tests count fake
Runtime Add calls and the required wait's actual injected pump step/finish
effects; host tests separately count optional tab-strip, watchdog, Navigate,
and the post-ensure HWND/controller apply boundary, with a successful apply as
positive control. Zero means those effects remain unreachable **after required
registration failure**. It does not claim Runtime, controller, or HWND work did
not already occur during Embed before the barrier.

### Proof ceiling

Those are headless seams and source guards. They do not prove a supported
WebView2 Runtime's callback schedule, controller-close behavior, real Win32
queue ordering, or first-document bridge/diagnostics/drag/resize/frame
rendering. The 2026-08-30 record predates the serialized refinement. The
[2026-08-31 paired live record](../issue-135-paired-live-verification.md) supplies
the missing supported-Runtime evidence from one frozen source identity: ordinary
untagged first-document behavior plus the tagged post-real-callback interleave.
The tagged artifact remains non-release-equivalent and proves neither ordinary
Runtime scheduling nor byte equivalence; its paired untagged artifact carries
the ordinary behavior claim.

> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: bind the accepted paired Runtime evidence to the barrier while retaining artifact proof ceilings.
