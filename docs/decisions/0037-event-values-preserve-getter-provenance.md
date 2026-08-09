# 0037. Event values preserve getter provenance before granting fallback authority

**Status:** Accepted

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
URI getter succeeded and reported the exact generated URL or the observed empty
representation, and the pending generation is claimed. The next top-level start
suspends active controls immediately. A matching, positively classified benign
abort or confirmed cancel restores the still-visible fallback exactly; a
completion whose success or required identity is unavailable clears the
capability and fails closed; a known success does not require an error status.
Unissued, pending and active are distinct throughout.

The source plan never admits `data:` as an origin, and no blanket `data:` rule is
added. The claimed fallback receives only six methods: start drag, start resize,
minimise, toggle maximise, query maximised and close. It never reaches readiness,
diagnostics or `Config.Bridge`. This change adds no random capability token; the
successful source/URI observation plus claimed generation is the authority.

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
callback consuming an event value without checking its paired getter error, or
any source-only `data:` admission, is an immediate trip-wire.

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
  fallback remains outside trusted-origin admission with exactly six controls.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: add successful-value, foreign-URI and production re-entrancy trip-wires to the event-provenance evidence.
