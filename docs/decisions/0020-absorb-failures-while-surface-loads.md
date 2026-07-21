# 0020. Failure completions are absorbed while the error surface loads

**Status:** Superseded by [0021](./0021-error-surface-navigation-identity.md): completions are attributed by navigation id, and this record's order-based machine survives only as the fallback for completions whose id is unavailable.

## Context

The 0017 state machine identifies the fallback error surface by navigation
state, because the runtime reports no source for a `data:` document. It has no
navigation identity either: `noteNavigationOutcome` classifies completions by
order alone, and it read any failure completion arriving after arming as the
surface's own load dying — seal (`fallback error surface load failed, not
retrying`) and disarm.

Observed live (issue #68, runtime 150.0.4078.83): a Retry against a still-down
server delivers **two** `ConnectionAborted` (9) failure completions 23ms apart.
The first armed the surface; the second was read as the surface dying and
sealed; the surface's real success completion, ~110ms later, was then read as a
navigation *away*. End state: mullion's own surface on screen, unadmitted,
every caption button dead — the issue #56 symptom on an ordering 0017 did not
consider.

The identity of the second failure completion is open (`unverified`): official
documentation says a superseded navigation completes `OperationCanceled` (14),
not the observed `ConnectionAborted` (9), so it is not a cancellation echo; the
leading candidates are an error-document commit completion or a second
connection attempt against the `[::1]` endpoint. What is settled is that the
machine cannot attribute it — and that a rapid Retry double-click adds more of
the same.

## Decision

While `errorSurfaceLoading` is true — from the decision to navigate to the
surface until the next success completion — a failure completion is logged at
debug (`navigation failure absorbed while the error surface loads`) and
otherwise ignored: no state change, no re-navigation. The premise is that the
surface is a `data:` URL whose load realistically cannot fail (0017's recursion
guard already leans on this), so a failure in that window belongs to some other
navigation, whatever its exact identity. Absorption is unbounded on purpose:
any fixed bound N is defeated by N+1 failures, which a rapid Retry double-click
can deliver.

The seal branch is kept, fail-closed, for the state it can still express —
`errorPageShown` set while `errorSurfaceLoading` is not — even though the
current transitions cannot reach it: arming sets both flags and only a success
completion clears them, so the absorb branch shadows it. A future path that
clears the loading flag early lands there and drops the admission rather than
keeping it against a document the machine cannot explain.

## Alternatives rejected

- **Absorb exactly one failure.** Assumes the straggler is provably singular,
  which rested on the refuted cancellation-echo attribution. A rapid Retry
  double-click delivers a third failure inside the window and re-creates the
  dead surface one click deeper. Locked against by
  `TestErrorSurfaceSurvivesARapidRetryDoubleClick`.
- **A "last Navigate was the surface" flag, re-admitting on success.**
  Re-admits on *any* success completion while the flag is up, which widens
  0017's success-first mis-admission instead of narrowing it.
- **Navigation identity via `GetNavigationID`.** The terminal fix: correlate
  the surface's own `Navigate` with its completions and stop classifying by
  order at all. The vtable slot is already declared
  (`interfaces_events_windows.go`, layout-test-locked), but `Browser.Navigate`
  returns no id and `NavigationStarting` is unbound — learning the surface's id
  is issue #6's COM event-binding work, not a P1 hotfix. Trip-wire below.
- **Keeping the seal (status quo).** Dead-ends the visible surface's caption
  buttons on a failed Retry — the defect itself.

## Consequences

- **If the surface's load ever fails to deliver its success completion** —
  the cannot-fail premise proving wrong, or the host's `Navigate` call itself
  failing synchronously (`warnIf`-logged; no completion will ever come) — the
  armed admission no longer seals: it persists, completion-counted rather than
  time-bounded. Failure completions are absorbed; the admission survives the
  first success completion (read as the surface's own load) and drops only at
  the one after it. Exposure is the reserved methods only — the window
  controls plus the frontend ready/phase/diagnostic signals, whose worst
  reachable action is closing the window; an empty source never reaches
  `Config.Bridge` (`messageSourceTrusted` accepts no empty source, observed).
- **A narrow widening of 0017's mis-admission residue.** Previously, a failure
  completion racing the surface's load sealed the machine, so a foreign
  success landing next found the admission already dropped. Absorbed instead,
  that foreign success is read as the surface's own load and its document
  stays admitted — the same reserved-methods bound as above — until the next
  successful navigation. Reachable when a navigation initiated from the departing
  document succeeds while the surface's load is in flight: for example, the
  server coming up exactly between two Retry clicks.
- `errorPageShown` now rises and falls with `errorSurfaceLoading`; the field
  stays for the fail-closed branch, and the branch is pinned by
  `TestErrorSurfaceSealsFailClosedOutsideTheLoadingWindow` constructing the
  otherwise-unreachable state directly.

## What would change our mind

- **`NavigationStarting`/`NavigationCompleted` bindings exposing navigation
  identity** (issue #6): a positive match of the surface's own navigation
  replaces both of 0017's ordering assumptions and this record's absorb window.
  This is the intended end state, not a hypothetical — when #6's event binding
  lands, revisit this record.
- **A live observation of a `data:` surface load failing.** The premise breaks,
  the completion-counted armed admission above stops being theoretical, and the
  absorb window needs a bound or an identity after all.
- **The NavigationId probe settling the second completion's identity** in a way
  that contradicts the "some other navigation's completion" reading.

## Evidence

- Issue #68: the live log timeline — two `status=9` completions 23ms apart, the
  seal line, twelve `untrusted source` rejects within 100ms, the success 110ms
  later (build `devel (e89ccb7, modified)`, runtime 150.0.4078.83) — and the
  deep-analysis comment: the state-machine replay, the refutation of the
  cancellation-echo attribution against official documentation, and the
  bound-1 defeat.
- `host/webview_windows_test.go`: `TestErrorSurfaceSurvivesAFailedRetry`,
  `TestErrorSurfaceSurvivesARapidRetryDoubleClick`,
  `TestErrorSurfaceAbsorbsAFailureStorm` and
  `TestErrorSurfaceStaysAdmittedWhenAFailureRacesItsOwnLoad`, proved
  fails-before against the sealing machine;
  `TestErrorSurfaceSealsFailClosedOutsideTheLoadingWindow` pinning the
  defensive branch; the five pre-existing `TestErrorSurface*` locks unchanged
  and green.
- Live re-verification on the issue's repro rig (2026-07-21, build
  `devel (a7c8bfd, modified)`, runtime 150.0.4078.83): three failed-Retry
  rounds each delivered two `ConnectionAborted` completions ~20ms apart, the
  second absorbed at debug every time; the surface's caption buttons worked
  after every round, with zero `web message rejected` and zero seal lines. On
  recovery — the server started while the surface was up — Retry loaded the
  frontend with a clean success completion and the bridge flowed. The rapid
  double-click burst remains covered headlessly only.

> Last updated: 2026-07-22 | Editor: Claude (Fable 5) | Change: status line superseded by 0021 (navigation-id attribution); the body is unchanged, per the supersede rules.
