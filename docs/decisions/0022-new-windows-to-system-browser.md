# 0022. New windows are routed to the system browser, never opened in the host

**Status:** Accepted. The launch moved off the UI thread by [0029](./0029-system-browser-launch-off-the-ui-thread.md): the routing this record decides is unchanged, but `ShellExecuteW` now runs on a bounded per-launch goroutine with its own STA, because running it inside the WebView2 event handler parked the message loop until the browser had started (issue #74).

## Context

The bridge and the window controls are injected into every document (0014). A
document - the app's own frontend, or a foreign origin the top frame was steered
to - can ask for a new window: `window.open(...)`, or a `target=_blank` link.
WebView2 raises `NewWindowRequested` for these. Left unhandled, the runtime
creates the new window itself: a second `ICoreWebView2` with no host around it -
no custom title bar, no caption buttons, none of the frame the host builds in
`WM_NCCALCSIZE`. A mullion `Host` is a single window (a second `Run` on one Host
is unsupported), so there is no host frame for that second WebView to live in,
and nothing the host can reach to fix it.

The target URI is also foreign input. `NewWindowRequested` was one of the two
bindings issue #6 named - with the navigation-cancel gate - for keeping foreign
content contained. It was unbound until now.

## Decision

The host binds `NewWindowRequested` and takes every new-window request over from
the runtime. It sets `Handled` on the args first, unconditionally, so the runtime
never creates its detached WebView. It then routes the request: an `http`/`https`
target is opened in the user's default browser with `ShellExecute`; any other
scheme is dropped. `isExternalBrowserSafe` is the gate, and it admits only
`http` and `https` - nothing else reaches `ShellExecute`.

## Alternatives rejected

- **Leave `NewWindowRequested` unhandled and let the runtime open the window.**
  The default. It produces a chrome-less, detached `ICoreWebView2` with no title
  bar and no controls in a single-window host - the broken-frame symptom the
  whole frame layer exists to avoid, now in a window the host cannot reach.
- **Provide our own `ICoreWebView2` as the new window (`put_NewWindow`).** How a
  multi-window WebView2 app keeps popups in-app. mullion is single-window by
  design (0004; a second `Run` is unsupported), so there is no second host frame
  to hand the runtime, and building multi-window support to serve `window.open`
  is a large feature for a rare need.
- **Open every scheme, not just http/https.** `ShellExecute` launches whatever
  handler is registered for a scheme. A foreign document could then name `file:`,
  a custom application protocol (`steam:`, an `ms-*:` handler) or any registered
  handler and have the host launch it on a mere `window.open`. Admitting only
  http/https keeps the routing to the one thing "open a link in the browser"
  means.
- **Route only user-initiated opens (gate on `IsUserInitiated`).** This would
  drop a page's bare scripted `window.open` and keep only clicked links - the
  pop-up-blocker posture. It is not done in this increment: the host issues no
  `window.open` of its own, so the flag is plausibly reliable here - unlike the
  navigation-starting flag, which the host's own `Navigate` also sets - but that
  reliability is unverified until the live probe, and accepting a programmatic
  http/https open matches desktop-webview norms (Electron, Tauri) while staying
  bounded to the browser. The flag is kept on the diagnostic line so the probe
  can measure it, and a gate is a cheap follow-up if it proves reliable and
  wanted.

## Consequences

- A mullion window never spawns a second WebView2. `window.open` and
  `target=_blank` either open in the system browser (http/https) or do nothing
  (any other scheme). An app that wanted an in-app popup does not get one; it must
  render its own in-page surface. This is a permanent constraint of the
  single-window design.
- Suppression is unconditional: even a request the host then drops (an
  unsupported scheme, a failed URI read) has its `Handled` set, so the runtime
  never falls back to its own window. A dropped request is silent to the page.
- This routes, but does not by itself contain, a *top-level* navigation
  off-origin - that is the navigation-cancel gate, issue #6's remaining half,
  which stays open. A foreign `window.open` now opens in the user's real browser,
  which is a safer outcome than loading it chrome-less in the host either way.
- Opening the browser is `ShellExecute`, which a headless test cannot exercise.
  The scheme gate is a pure function and is locked; the launch itself is verified
  live.

## What would change our mind

- A requirement to support genuine multi-window apps (several `ICoreWebView2`
  under one process, each with host chrome) would replace "route away" with
  `put_NewWindow` and a real second host frame.
- A runtime that raised `NewWindowRequested` for a navigation the host itself
  issued would need the routing to recognise its own opens; nothing observed
  suggests it does.
- A measurement that `Handled` does not actually suppress the default window on
  some runtime would move the suppression earlier or add a fallback.

## Evidence

- WebView2.h (Microsoft.Web.WebView2 SDK 1.0.2903.40,
  `build/native/include/WebView2.h`): `ICoreWebView2NewWindowRequestedEventArgs`
  IID `{34acb11c-fc37-4418-9132-f9c21d1eafb9}` and its 11-slot vtable order, the
  handler IID `{d4c185fe-c81c-4989-97af-2d3fa7ab5651}`, and `AddNewWindowRequested`
  at core vtable slot 44. The vtable order and the core slot are locked by
  `TestNewWindowRequestedEventArgsVtblLayout` and `TestCoreWebView2VtblLayout`;
  the IID by `TestInterfaceIDs`.
- `host/systembrowser_windows_test.go`: `TestIsExternalBrowserSafe` locks the
  scheme gate - http/https admitted; `file:`, `javascript:`, `data:`, custom
  protocols, a UNC path and an unparseable URL refused - and fails against an
  allow-all mutant. `TestRouteNewWindowDropsUnsafeSchemes` confirms an unsafe
  scheme never reaches the system-browser route.
- Live-verified (2026-07-24, runtime 150.0.4078.83, `devel (1108a3f)` scaffolding
  build of `examples/basic`): the runtime raises the event, `Handled` suppresses
  the default window, and `ShellExecute` opens the target. A `target=_blank`
  anchor and a scripted `window.open('https://…')` each opened in the system
  browser with no detached window (`new window routed to system browser`, three
  opens), `window.open('mailto:…')` launched nothing (`new window dropped,
  unsupported scheme`), and the session ended with zero warnings and zero errors.
  `IsUserInitiated` read `true` for every clicked open — including one fired from
  a 500 ms `setTimeout`, because Chromium's transient activation outlives the
  click by seconds. Only a genuinely gesture-less open would read `false`; that
  is the signal a future gate would key on.

> Last updated: 2026-07-26 | Editor: Claude (Opus 5) | Change: the live probe ran and confirmed the routing end to end; the unverified marker is resolved with what was observed. Then the status line records 0029, which moved the launch off the UI thread - the index credited it and this header did not.
