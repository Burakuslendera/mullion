# 0024. An aborted navigation mullion served itself is not a load failure

**Status:** Accepted

## Context

0021 attributes navigation completions by id, and every attributed failure that
is not the surface's own arms the fallback error surface. Which failures deserve
that was never examined: the machine reads *any* failure status as "could not
load".

Issue #72, observed live in `examples/basic`: a renderer-initiated same-origin
*document* navigation on the trusted virtual host completes `ConnectionAborted`
(9) although its asset was served `200`, and the runtime then starts the same
navigation again by itself - the "two navigations, the second aborts" shape 0021
and #68 already recorded for Retry. Arming on that abort replaced the live
frontend with the fallback page; the surface's Retry aborted the same way, so it
looped until an attempt happened to commit. It is a race: some attempts at the
identical URL commit cleanly.

Whether that status *can* mean "could not load" depends on where the bytes for
that particular navigation came from. Two things have to be true before an abort
is provably not a failure, and the first cut of this record checked only the
first - which the pre-merge audit refuted (see the rejected alternatives).

## Decision

An attributed failure completion whose status is `ConnectionAborted` does not arm
the fallback surface when **both** hold:

- `Config.URL` is empty, so mullion answers the frontend's requests itself from
  the embedded `fs.FS` through `WebResourceRequested`
  (`Config.servesAssetsInProcess`); and
- the navigation was headed for the **trusted origin**. `Config.URL` being empty
  does not keep the top frame there - `PinNavigationToOrigin` is opt-in and off
  by default (0023), so a link or a script assignment can take the top frame to
  any origin, and such a navigation is a real socket load mullion serves none of.

A completion carries no URI, so `noteNavigationTarget` records the answer at
`NavigationStarting`, keyed by navigation id. The exemption requires that id to
still match: a completion for an older navigation cannot borrow another
navigation's target and falls through to the ordinary failure path, which arms.

`benignAbort` is that condition, applied in `noteForeignOutcome` after the absorb
branch and before arming, and it logs the suppression at debug. It applies only
where identity applies: `noteOrderedOutcome` - 0020's machine, the fallback for
completions carrying no id - is unchanged.

## Alternatives rejected

- **Key on the config mode alone** (`Config.URL == ""`), the first cut of this
  record. Refuted by two independent audit passes: with the cancel gate off, a
  frontend link to a foreign origin is a real socket load, and swallowing its
  abort strands the user on a chromeless foreign page with no caption buttons -
  issue #3, the state the surface exists to prevent.
  `TestErrorSurfaceAbortOffOriginStillArms` fails against that mutant.
- **Exempt `ConnectionAborted` everywhere** (issue #72's second suggestion).
  With `Config.URL` set the caller serves the frontend over a socket, and a dead
  endpoint has been observed producing this status (#68), so a general exemption
  removes the surface from the case it exists for.
  `TestErrorSurfaceAbortStillArmsWhenTheCallerServesTheURL` fails against it.
- **Correlate the abort with the `200` served for that navigation** (issue #72's
  first suggestion). Not available as stated: `WebResourceRequested` carries no
  navigation id, so the resource and the completion cannot be joined directly.
  What *is* available is the join this record uses - `NavigationStarting` carries
  both the URI and the id - which answers where the navigation was going, not
  whether its bytes were actually served.
- **Wait before arming, to see whether the runtime restarts the navigation.**
  A timer on the UI thread, a delay before the surface any real failure needs,
  and a new class of ordering bug of the kind 0020 and 0021 spent two issues
  closing.
- **Extend the exemption to the id-less path.** Without an id nothing ties the
  completion to any recorded target, so suppressing the surface there is a guess
  that fails open in the one case the surface exists for. Absent identity, 0020's
  machine stands.

## Consequences

- In process and on the trusted origin, a genuine load failure reported as
  `ConnectionAborted` no longer shows the surface. The exposure is narrower than
  the mode alone would make it, but it is **not** empty: `webResourceRequested`
  returns without setting a response on several paths (a nil request, args or
  environment, a failed `GetUri`, a failed response construction) and the request
  filter registration is only `warnIf`'d, and in each case the runtime falls back
  to a real network fetch of the virtual host name. Those are genuine "could not
  load" conditions, and this record only claims that their *likely* statuses are
  not 9.
- The suppression is a pure no-op besides its log line: it must not clear
  `errorSurfaceActive`. The surface itself can be on screen when an abort arrives
  - its Retry aborting is issue #72's own loop - and dropping the admission there
  would kill the visible surface's caption buttons, the issue #56 symptom.
  `TestErrorSurfaceAbortLeavesAVisibleSurfaceAdmitted` locks it.
- Because it is a no-op, it also does not repair state the way arming did.
  Arming both asserted the admission and put mullion's own page back on screen,
  which ended any mis-admission 0020's id-less rules had produced; a suppressed
  abort leaves such a stale admission standing until a navigation succeeds. The
  exposure is unchanged in kind (the reserved window controls, never
  `Config.Bridge`) and longer in time.
- A suppressed abort also skips `requestStartupShow("navigation_failed")`, which
  sits after the machine's early return. If the *initial* navigation aborts, the
  window waits for `Config.ShowTimeout` instead of appearing at once. Accepted:
  nothing is broken to report yet - the runtime restarts the navigation - and the
  timeout is the safety net for exactly this.
- The exemption is scoped away from `noteSurfaceOwnOutcome` as well as from
  `noteOrderedOutcome`. The surface's own aborted load still seals, even in
  process. Sealing is the conservative outcome (drop the admission, do not
  re-navigate), and re-navigating a surface that just aborted is the loop 0017's
  recursion guard exists for.
- The error-surface tests that model #68's orderings now build a host with
  `Config.URL` set, which is the setting those timelines were measured in. That
  is a correction, not an accommodation: they were written against `Config{}`
  while modelling a dead socket. `TestErrorSurfaceSealsInProcessToo` keeps one
  seal locked in the in-process mode they left behind.

## What would change our mind

- An in-process, on-origin `ConnectionAborted` traced to a real failure would
  make the condition too coarse, and the fix would move to a true
  resource-to-navigation correlation - which needs a runtime that reports the
  navigation id on the resource request.
- The runtime giving the restarted navigation the *same* id as the aborted one
  would make the abort recognisable directly, which is a better rule than either
  condition here.

## Evidence

- Issue #72: the live log - `asset response served, status=200` followed by
  `navigation failed, status=9` for the same in-origin navigation, then the
  surface, then the same abort on Retry. Reproduced with `PinNavigationToOrigin`
  both off and on, so it is independent of 0023.
- Issue #68 and 0020's Context: a Retry against a still-down loopback endpoint
  delivered two `ConnectionAborted` completions. That establishes that a dead
  endpoint *can* produce this status - enough to keep the arming in URL mode -
  not that the status implies a dead endpoint. 0020 records the second of those
  two completions as `unverified` to this day.
- `host/errorsurface_windows_test.go`, each failing against its own mutant:
  `TestErrorSurfaceAbortDoesNotArmWhenAssetsAreServedInProcess` (the rule
  itself), `TestErrorSurfaceAbortOffOriginStillArms` (the target condition),
  `TestErrorSurfaceAbortWithAStaleIdStillArms` (the id match),
  `TestErrorSurfaceAbortStillArmsWhenTheCallerServesTheURL` (the mode
  condition), `TestErrorSurfaceOtherFailuresStillArmInProcess` (the status
  check), `TestErrorSurfaceAbortWithoutIdentityStillArms` (the id-less scope),
  `TestErrorSurfaceSealsInProcessToo` (the seal scope) and
  `TestErrorSurfaceAbortLeavesAVisibleSurfaceAdmitted` (the no-op property).
- That the live loop stops is `unverified` pending the live probe: the repro is
  non-deterministic, so the check is a run of in-origin navigations with no
  fallback surface in the log.

> Last updated: 2026-07-24 | Editor: Claude (Opus 5) | Change: new record - an aborted navigation is benign only where mullion served it and it was on-origin (issue #72, refining 0021); the mode-only first cut is recorded as rejected.
