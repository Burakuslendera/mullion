# 0037. Event values preserve getter provenance before granting fallback authority

**Status:** Accepted; owns current fallback claim authority, separately from [0043](./0043-external-routes-are-uri-only-os-activations.md)'s external-routing contract

## Context

WebView2 event values arrive through independent COM getters. A zero value after
a failed getter is not an observed empty URI, navigation id `0`, success, status
or process kind. Collapsing both cases made an unreadable event look like valid
identity and could leave the fallback page's native window controls admitted to
the wrong document. The fallback needs those controls because WebView2 has been
observed reporting its `data:` document source as empty.

## Decision

For every value passed across an observation callback, the Go-owned adapter
carries the getter's value and original HRESULT independently. Getters that fail
before a safe callback exists are reported locally under decision 0038. Host
policy transitions state before reporting callback-carried failures and uses a
value only when its getter succeeded.

A navigation failure creates a revocable, unissued fallback plan; it grants no
authority. Immediately before `Navigate`, the current plan becomes **pending**.
A NavigationStarting event makes that issued generation **active** only when the
URI getter succeeded and reported the exact generated URL or a successfully read
empty URI, and that pending generation is claimed. The empty form is tolerated
but remains unverified on NavigationStarting; prior live fallback starts
reported the full generated URL. Claim evaluation runs before
`PinNavigationToOrigin`: a matching fallback start is not cancelled, but a
failed URI getter, an arbitrary `data:` URI, or any other value cannot claim and
fails closed. The next top-level start suspends active controls immediately.
A matching, positively classified benign abort or confirmed cancel restores the
still-visible fallback exactly; a completion whose success or required identity
is unavailable clears the capability and fails closed; a known success does not
require an error status. Unissued, pending and active are distinct throughout.

The source plan never admits `data:` as an origin, and no blanket `data:` rule is
added. The claimed fallback receives exactly seven reserved methods:
`WindowStartDrag`, `WindowStartResize`, `WindowMinimise`,
`WindowToggleMaximise`, `WindowIsMaximised`, `WindowFrameState`, and
`WindowClose`. It never reaches readiness, diagnostics, or `Config.Bridge`.
This change adds no random capability token; the successful source/URI
observation plus claimed pending generation is the authority.

## Alternatives rejected

**Treat getter failure as the type's zero value.** This erases HRESULT provenance
and turns missing evidence into evidence, especially for the empty fallback URI
and id `0` paths.

**Admit empty source or every `data:` document.** Frames and unrelated data
navigations can share that representation; source text alone cannot bind a
message to Mullion's current fallback generation.

**Put a random token in the generated page.** A token could strengthen document
identity, but would add generation transport, lifetime and disclosure machinery.
The event claim already supplies the required boundary; this fix does not invent
a second capability system.

## Consequences

Event callback types are more verbose and every host consumer must check the
paired error. Unclassifiable failures can suppress a fallback that might have
been legitimate; this availability cost is accepted to avoid granting native
control authority without evidence. Exact abort/cancel restoration must remain
scoped to the suspended departure id, and state changes must precede Logger calls
because an embedder Logger may re-enter host policy.

## What would change our mind

Supersede this record if WebView2 provides an authenticated document identity on
both NavigationStarting and WebMessageReceived, stable across the fallback's
lifetime, so authority no longer depends on the empty-source observation. A host
callback consuming an event value without checking its paired getter error, any
source-only `data:` admission, or any grant outside the seven-method list is an
immediate tripwire. A foreign navigation successfully reporting an empty URI
while a fallback generation is pending remains unverified; observing it steal
the claim is a conditional P2 tripwire.

## Evidence

- `internal/webview2/browser_event_observations_windows_test.go`: distinct fake
  HRESULTs and successful sentinel values survive every observation adapter;
  callback counts and `PutCancel`/`PutHandled` ordering are explicit.
- `host/webview_event_observations_windows_test.go`: production callbacks fail
  closed, transition before diagnostics, reject a foreign successful URI while a
  fallback is pending, and use known success without inventing unknown fields.
- `host/errorsurface_reentrancy_windows_test.go`: the production completion
  callback seals fallback controls before a re-entrant Logger can dispatch an
  empty-source `WindowClose`.
- `host/errorsurface_windows_test.go`, `host/errorsurface_abort_windows_test.go`
  and `host/errorsurface_identity_windows_test.go`: pending/active claim,
  suspension, exact restoration and unknown-completion behavior.
- `host/bridge_windows_test.go`: failed source never dispatches and a claimed
  fallback remains outside trusted-origin admission with exactly seven controls.

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: correct the current exact-or-successful-empty claim-before-pin authority, seven-method restriction, fail-closed cases, and conditional P2 tripwire.
