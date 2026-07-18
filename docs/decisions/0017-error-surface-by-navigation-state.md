# 0017. The error surface is identified by navigation state, not by its source

**Status:** Accepted

## Context

The 0014 dispatch gate admits a `data:` source so the fallback error surface's
caption buttons work. Measured live (issue #56, WebView2 runtime 150.0.4078.65),
the runtime reports a `data:` document's source as the **empty string** — at the
`WebMessageReceived` args and at `ICoreWebView2.get_Source` alike — so the
`data:` branch never matches there, and every message the surface posted was
rejected: dead caption buttons on the one page whose whole job is having working
caption buttons. The runtime provides no representation of the surface to match
(the dead ends are lessons-and-dead-ends.md §14).

## Decision

The host identifies its own error surface from state it already owns. A
UI-thread state machine — `noteNavigationOutcome` in `host/webview_windows.go` —
arms `errorSurfaceActive` when the surface is navigated to (before its load
completes, because the injected diagnostics post from document creation), holds
it through the surface's own success completion (`errorSurfaceLoading`), and
disarms it when a navigation leaves the surface or the surface itself fails to
load. An empty source is admitted only while the flag is up, and only for the
reserved window controls; `messageSourceTrusted` is unchanged, so `Config.Bridge`
stays origin-gated (0014).

This imposes an invariant: **every path that navigates the top frame must keep
`noteNavigationOutcome`'s bookkeeping true.** `NavigationCompletedCallback` is
currently the single funnel; a navigation entry point added past it either
reopens #56 (surface rejected again) or, worse, leaves the admission armed
against a foreign document.

## Alternatives rejected

- **Recognise the surface by its message source** — the 0014 `data:` branch.
  The runtime reports `""`, measured live. The branch is kept as belt and
  braces for a runtime that does report the URI.
- **Ask the core for the top document's URI at message time**
  (`ICoreWebView2.get_Source`) — also `""` for the same document, measured
  live; the binding written for the attempt was deleted as dead code.
- **A blanket empty-source allowance.** Empty is also what other opaque
  documents report — a `data:` or sandboxed iframe a script creates — so an
  unconditional allowance would hand every such frame the window controls at
  all times, instead of only while mullion's own page is up.

## Consequences

Two accepted costs, both bounded to the reserved window controls — an empty
source never reaches `Config.Bridge`:

- **The pre-commit window.** The flag arms at the decision to navigate, so
  until the surface's document commits, the departing document can post an
  empty-source message and be granted the controls. On this path that document
  is a failed load or mullion's own about:blank; after a real frontend has
  loaded, its own empty-source subframes hold the controls for the commit
  duration.
- **The first-success-after-arming assumption.** The machine treats the first
  success completion after arming as the surface's own load; it has no
  navigation identity to check. A page-initiated navigation that supersedes the
  surface's `Navigate` and lands its success completion *first* would leave the
  admission armed against that document until its next top navigation. The
  observed runtime ordering — a superseded navigation completes with `false`
  before the winner commits, which routes through the surface-failed branch and
  clears the flags — self-heals this; the residue is that the gate's tightness
  rests partly on that ordering (`likely`, not verified against a hostile
  schedule).

## What would change our mind

- A runtime that reports a real URI for `data:` documents at either `GetSource`
  level — the 0014 `data:` branch then carries the surface and this state
  machine becomes redundant.
- A `NavigationCompleted` binding exposing navigation identity (correlating the
  surface's own `Navigate` with its completion) — replaces the
  first-success-after-arming assumption with a positive match.
- A live observation of a success completion overtaking a superseded
  navigation's failure completion — the `likely` above becomes real, and the
  correlation fix stops being optional.

## Evidence

- Issue #56: the live report (ten rejections per run), and the probe
  measurements — `""` at both `GetSource` levels, runtime 150.0.4078.65.
- `host/webview_windows_test.go`: the six `TestErrorSurface*` state-machine
  tests, proved fails-before against a neutralised gate (the three admit-side
  tests fail, the three reject-side tests hold).
- The live re-run after the fix: zero rejections; the surface's
  phase/diagnostic messages flow through the same
  notify → postMessage → gate → reserved-method chain the caption buttons use.
- lessons-and-dead-ends.md §14: the dead ends this record's decision replaced.

> Last updated: 2026-07-18 | Editor: Claude (Fable 5) | Change: new record for the navigation-state identification of the error surface (issue #56).
