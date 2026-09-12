# 0053. An asset callback that cannot serve installs a blocking response instead of letting the request reach the network

**Status:** Accepted

## Context

The embedded asset route is a no-network promise ([decision 0002](./0002-no-local-port.md)):
a request matched by the `WebResourceRequested` filter is answered in process, and
nothing binds a socket. But WebView2's own contract for that event is fail-open by
default. Microsoft's documented behavior is that when the handler ends without
`put_Response`, the request continues normally through the network stack. Several
error exits in the callback chain did exactly that (issue #150): a failed `GetRequest`
returned from the COM-layer handler without answering, and the host callback returned
without a response when the request, the event args or the environment was
unavailable, when `GetUri` failed, when `CreateWebResourceResponse` failed, and when
`PutResponse` failed. A recovered panic took the same road. Each of these is
unlikely on a supported Runtime, and none was observed live - but each converts a
proven in-process boundary into a network fetch under the documented contract, which
the repository priority ladder classifies as a P0 breach of the asset-serving
boundary.

The escalation pressure is asymmetric. Availability says: log it and move on, the
COM failure is transient. The boundary says: an unanswered matched request is the
one outcome that cannot be allowed, because whatever answers it is, by definition,
not mullion.

## Decision

Every exit from the asset callback answers in process or ends the session; the
network is unreachable from both.

The callback defers a finish step that owns the guarantee. An exit that installed
its own response is done. Any other exit - a failed getter, unavailable args or
environment, a failed response construction, a failed `PutResponse`, a recovered
panic - receives one deterministic blocking response: `500 Internal Server Error`,
no content, the boundary's standard `nosniff` and no-store header block. It is
built with a nil content stream on purpose, so the blocking path needs no
`SHCreateMemStream` call and cannot fail where the served path did. The creator
reference is released after the put attempt, so the blocking answer adds no
lifetime rules.

When even that cannot be built or installed - no args to answer on, no environment
to build from, or a Runtime that refused both attempts - the callback escalates to
the fail-closed terminal teardown of [decision 0052](./0052-browser-process-exit-fails-closed.md):
one latched cause, one token-validated tagged destroy command, `Run` reporting
`ErrAssetBoundaryClosed` instead of a false normal close. The escalation keeps
0052's `ShuttingDown` refusal: one arriving while a user-initiated close already
owns the browser is declined, so a close the user started still returns a normal
close. The webview2 layer
participates only by forwarding: a failed `GetRequest` is reported and the host
callback still runs with no request, because the host owns the boundary policy and
a silent return there would be the fail-open exit itself.

## Alternatives rejected

- **Restrict `Config.VirtualHost` to the reserved `.localhost` suffix instead.**
  The attack schedule of issue #150 starts from a caller choosing a resolvable
  virtual host name, so refusing other names would shrink the exposure. It would
  also relocate rather than close the gap: the boundary must hold for every origin
  mullion is configured to serve, and [decision 0036](./0036-one-source-plan-defines-origin.md)
  makes the plan the caller's choice. A consumer with a legitimate non-reserved
  internal name loses nothing from this fix and would lose their configuration to
  the alternative.
- **Retry the failed `PutResponse` with the same response object.** The Runtime
  has already refused that object once; the contract gives no meaning to a second
  put of a response it rejected, and the issue itself flags "PutResponse after a
  retained earlier response" as uncharted. A fresh minimal response is
  deterministic and cannot disagree with retained state.
- **Make the webview2 layer answer the blocking response itself when `GetRequest`
  fails.** The COM layer would then need the environment, a response policy and an
  escalation path of its own, duplicating the boundary in a package whose job is
  transport. Forwarding the failure to the host callback keeps one owner
  ([decision 0038](./0038-terminal-policy-owns-error-reporting.md) keeps the same
  shape for reporting).
- **Recover in place on escalation** - re-embed, navigate to a fallback, or drop
  the filter. Same reasoning as [decision 0052](./0052-browser-process-exit-fails-closed.md)
  refused recreation: a boundary that just failed to answer twice has no standing
  to manage its own recovery from inside the runtime's event dispatch.

## Consequences

- A persistent COM failure in the asset path now costs the window. Availability is
  the accepted price; a user sees the window close instead of a page served by
  something that is not mullion.
- `Run`'s contract grows `ErrAssetBoundaryClosed`. Callers must not treat it as a
  normal close; the `Host` remains reusable for a later `Run`, exactly as for
  `ErrBrowserProcessExited`.
- The webview2 callback contract changes: `WebResourceRequestedCallback` may now
  be invoked with a nil request after a failed `GetRequest`. Every consumer must
  answer, not dereference.
- The blocking answer is contract-based, not live-proven. Microsoft's page for
  `add_WebResourceRequested` is the authority for the fail-open transition; no
  supported-Runtime run has recorded the default-host failure presentation or a
  loopback fallback probe. That live proof ceiling stays open in
  [issue #150](https://github.com/Burakuslendera/mullion/issues/150)'s own terms:
  a later audit-owned check may use a loopback listener and a non-default virtual
  host, and must not use a public host.

## What would change our mind

- Microsoft adds a cancellation or fail-closed primitive to
  `WebResourceRequestedEventArgs`, or documents that an event handler failing
  after a `put_Response` cannot fall through. The blocking response would then be
  the redundant half and the terminal escalation the only needed one - or both
  could be replaced by the primitive.
- A supported Runtime is observed serving the network answer for an event whose
  handler successfully called `PutResponse`. That would mean the runtime does not
  honor its own contract, and the blocking response cannot be trusted; the design
  would need the navigation-level cancel gate instead.
- Escalations fire in practice - repeated `ErrAssetBoundaryClosed` sessions on
  ordinary machines. That would mean the blocking path's two COM calls are too
  fragile to be the last resort, and a cheaper answer (a cached response object,
  for instance) belongs here.

## Evidence

- `TestAssetCallbackFailsClosedOnEveryErrorExit` injects each recoverable
  failure independently on the fake COM vtables - a nil request, a failed
  `GetUri`, a `CreateWebResourceResponse` that fails once, a `PutResponse`
  that fails once, and a panic from the asset `fs.FS` - and asserts the
  blocking answer every time: a `500` with no content, the standard
  `nosniff`/no-store header block, put exactly once, its creator reference
  released.
- `TestAssetCallbackBlocksBeforeReportingThePanic` runs the recovered-panic
  exit against a Logger whose `Error` panics and asserts the blocking response
  is installed before the panic report, so a second panic out of the recover
  body cannot leave the event without a response.
- `TestAssetCallbackEscalatesWhenNoResponseIsPossible` covers the exits with
  no answer to install - unavailable event args or environment, a response
  creation that never succeeds, a `PutResponse` that never succeeds - and
  asserts exactly one terminal escalation with a recorded stage and cause.
- `TestAssetCallbackServesWithoutEscalation` pins the success path: a normal
  document still answers its own `200` with the body stream attached, an
  unreadable method is absorbed as before, and the terminal seam stays silent.
- `TestAssetBoundaryTerminalLatchesOnceAndRefusesShuttingDown` and
  `TestAssetBoundaryTerminalAppliesThroughTheTaggedCommand` pin the host side
  of the escalation: one latched cause per `Run`, a shutting-down browser
  refused, and the shared teardown command logging the true cause.

> Last updated: 2026-09-13 | Editor: ZCode (GLM-5.3-Flash) | Change: record the blocking-response escalation and add the Evidence section - the fake-vtable suite pinning the blocking 500, the panic-report ordering, the exactly-once escalation and the unchanged success path (issue #150).
