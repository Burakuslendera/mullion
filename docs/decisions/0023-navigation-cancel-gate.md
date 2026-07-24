# 0023. A top-level navigation off the trusted origin is cancelled, opt-in

**Status:** Accepted

## Context

0014 gated the bridge at message dispatch: a web message from a foreign origin
is dropped, so an origin the top frame is steered to cannot act through the
bridge. But the bridge is still *injected* into that origin
(`AddScriptToExecuteOnDocumentCreated` is origin-independent), and the foreign
document still *loads* - replacing mullion's custom frame with a chromeless
foreign page, and carrying `window.<ns>` onto an origin that can at least
fingerprint it. 0014 named the stronger containment as its follow-up: cancel the
top navigation so the foreign origin never loads. `NavigationStarting` is now
bound (0021), and its args carry `put_Cancel`.

The frontend is steered off-origin by an external link with no `target`,
`window.location = …`, an OAuth or open-redirect on a trusted link, or a server
redirect. For a single-window frameless host a top-frame navigation off-origin
is almost always unintended: it replaces the app's own chrome with a foreign
page the host cannot frame.

## Decision

When `Config.PinNavigationToOrigin` is set, the host cancels any top-level
navigation whose target is neither the trusted origin (the virtual host, or the
`Config.URL` origin - any path on it) nor mullion's own `data:` error surface,
and it opens an http/https foreign target in the system browser instead (any
other scheme is dropped) - the same routing as `NewWindowRequested` (0022). The
gate is **off by default**: the zero value cancels nothing, so an existing
consumer is unaffected. `NavigationStartingCallback` returns whether to cancel;
the `internal/webview2` layer calls `put_Cancel`, and the host never touches the
COM args.

## Alternatives rejected

- **On by default.** A top-frame OAuth or open-redirect flow an app runs on
  purpose would be cancelled, so a forced default is a behaviour break for a
  library. 0014 already contains the reach-into-Go threat, so this is
  defense-in-depth; off-by-default keeps the strong containment available
  without changing what any existing app does.
- **Cancel silently, without routing.** A cancelled external link that goes
  nowhere reads as a dead link. Routing http/https to the system browser (as
  `NewWindowRequested` does) means a clicked external link still goes somewhere,
  and the two paths behave identically.
- **Gate on `IsUserInitiated`.** It is true for host-driven navigations too
  (0021), so it cannot separate a genuine off-origin click from anything else;
  the trusted-origin allow-list is the honest boundary, not a gesture flag.
- **A per-URL allow-list in `Config`.** More flexible, more permanent surface.
  The single trusted-origin invariant covers the threat; if a real need for
  finer control appears it becomes a `NavigationPolicy func` callback, not a
  list.

## Consequences

- With the gate on, a frontend cannot navigate its top frame off-origin: a
  top-frame OAuth redirect flow breaks (run it through a popup - which
  `NewWindowRequested` routes to the system browser - or the system browser
  directly). This is the permanent cost, and it is why the gate is opt-in.
- The surface's own navigations always pass. The error-surface identity claim
  runs first, and a claimed start is never cancelled - even when the runtime
  reports it with an empty or truncated URI (the forms `surfaceURIMatches`
  tolerates), which a bare origin check would treat as off-origin. And a cancelled
  navigation completes with `OperationCanceled`: that completion is recognised by
  its id (`noteGateCancelledOutcome`) and kept out of the error-surface machine,
  so cancelling a foreign click does not read as a load failure that arms the
  fallback surface and tears the live frontend down into it. Both were review
  findings on the first cut of this gate, not designed in from the start.
- Off by default, this record imposes nothing on an app that does not opt in.

## What would change our mind

- A runtime that does not raise `NavigationStarting` for an HTTP redirect would
  let an open-redirect slip the gate; the containment would then need the
  completion side or `NavigationStarting` on sub-frames too. The live probe
  checks a redirect specifically.
- A common need for top-frame off-origin flows would argue for a per-navigation
  `Config.NavigationPolicy func(uri) Decision` callback rather than a boolean.
- The runtime raising `NavigationStarting` for the host's own initial `Navigate`
  in a form this mis-classifies as off-origin would need an initial-navigation
  exemption; nothing observed suggests it does (the initial target is the
  trusted origin, which the allow-list passes).

## Evidence

- WebView2.h (SDK 1.0.2903.40): `put_Cancel` is slot 8 of the
  `NavigationStarting` args vtable; the `PutCancel` wrapper's offset is locked by
  `TestNavigationStartingEventArgsVtblLayout`.
- `host/loopback_test.go` `TestNavigationOffOrigin` locks the pure gate decision:
  off cancels nothing; on passes the trusted origin (any path, the default-port
  and case-insensitive host forms) and the `data:` surface, and reports foreign
  https, a scheme downgrade, a userinfo spoof and the blob:/file:/about:blank
  forms off-origin. It fails against a gate-never-cancels mutant.
- `host/systembrowser_windows_test.go` `TestShouldCancelNavigation`: an
  off-origin non-http(s) target is cancelled and dropped, never routed; the
  trusted origin is never cancelled.
- `host/webview_windows_test.go`: `TestGateCancelledCompletionDoesNotArmTheSurface`
  - a cancelled navigation's `OperationCanceled` completion is consumed and never
  arms the surface, while an unrelated foreign failure still does;
  `TestNoteAndGateNavigationNeverCancelsTheSurface` - a surface start reported
  with an empty URI is claimed and not cancelled. Both fail against a mutant that
  drops the respective guard.
- That the runtime raises `NavigationStarting` for a foreign navigation
  (including a redirect), that `put_Cancel` actually abandons it, and that
  `ShellExecute` opens the routed target are `unverified` pending the live probe.

> Last updated: 2026-07-24 | Editor: Claude (Fable 5) | Change: new record - the opt-in navigation-cancel gate that pins the top frame to the trusted origin (issue #6, the second half of 0014's follow-up).
