# 0024. An aborted navigation is not a load failure when mullion serves the assets

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

What the status *means* depends on whether there is a socket in the path. With
`Config.URL` set there is one, and #68's timeline (0020's Context) measured
`ConnectionAborted` as exactly what a dead loopback endpoint produces. Serving
the embedded `fs.FS` in process there is none: the bytes come from
`WebResourceRequested` on this thread, and no endpoint exists that could be
unreachable.

## Decision

An attributed failure completion whose status is `ConnectionAborted` does not
arm the fallback surface when `Config.URL` is empty - when mullion answers the
frontend's requests itself. `benignAbort` is that condition
(`Config.servesAssetsInProcess`), applied in `noteForeignOutcome` after the
absorb branch and before arming, and it logs the suppression at debug.

It applies only where identity applies. `noteOrderedOutcome` - 0020's machine,
the fallback for completions carrying no id - is unchanged.

## Alternatives rejected

- **Exempt `ConnectionAborted` everywhere** (issue #72's second suggestion).
  Refuted by measurement, not taste: with `Config.URL` set that status *is* the
  dead endpoint (#68), so a general exemption removes the surface from the case
  issue #3 opened it for - the user stranded on Edge's chromeless error page with
  the native caption gone and no way to close the window.
  `TestErrorSurfaceAbortStillArmsWhenTheCallerServesTheURL` fails against that
  mutant.
- **Correlate the abort with the `200` served for that navigation** (issue #72's
  first suggestion). There is no key to join on: `NavigationCompleted` args carry
  no URI, `WebResourceRequested` carries no navigation id, and with `Config.URL`
  set the asset provider is not attached at all. The in-process condition is that
  correlation's honest approximation - it holds exactly when every byte came from
  us.
- **Wait before arming, to see whether the runtime restarts the navigation.**
  A timer on the UI thread, a delay before the surface any real failure needs,
  and a new class of ordering bug of the kind 0020 and 0021 spent two issues
  closing.
- **Extend the exemption to the id-less path.** Without an id nothing ties the
  completion to the navigation whose asset was served, so suppressing the surface
  there is a guess that fails open in the one case the surface exists for.
  Absent identity, 0020's machine stands.

## Consequences

- In process, a genuine load failure reported as `ConnectionAborted` no longer
  shows the surface. The exposure is narrow by construction: in that mode a
  missing asset is a `404` document mullion serves (a *successful* navigation)
  and a refused one is an error response, so a failure completion there has no
  known producer left.
- The suppression is logged at debug rather than silently dropped, so a report
  can still show that a completion arrived and what was done with it.
- The error-surface tests that model #68's orderings now build a host with
  `Config.URL` set, which is the setting those timelines were measured in. That
  is a correction, not an accommodation: they were written against `Config{}`
  while modelling a dead socket.

## What would change our mind

- An in-process `ConnectionAborted` traced to a real, reachable-content failure
  would make the condition too coarse, and the fix would move to the correlation
  this record rejected - which needs a runtime that reports either the URI on
  the completion or the navigation id on the resource request.
- The runtime giving the restarted navigation the *same* id as the aborted one
  would make the abort recognisable directly, which is a better rule than the
  mode we are keying on.

## Evidence

- Issue #72: the live log - `asset response served, status=200` followed by
  `navigation failed, status=9` for the same in-origin navigation, then the
  surface, then the same abort on Retry. Reproduced with
  `PinNavigationToOrigin` both off and on, so it is independent of 0023.
- `internal/webview2/interfaces_windows.go`: `WebErrorStatusConnectionAborted`
  is documented as "the status a dead loopback endpoint produces (issue #68,
  observed)" - the measurement this record splits on.
- `host/webview_windows_test.go`: `TestErrorSurfaceAbortDoesNotArmWhenAssetsAreServedInProcess`
  (fails without the rule), `TestErrorSurfaceAbortStillArmsWhenTheCallerServesTheURL`
  (fails against a general exemption), `TestErrorSurfaceOtherFailuresStillArmInProcess`
  (fails if the status check is dropped), `TestErrorSurfaceAbortWithoutIdentityStillArms`
  (fails if the rule reaches 0020's machine, along with eight of its own tests).
- That the live loop stops is `unverified` pending the live probe: the repro is
  non-deterministic, so the check is a run of in-origin navigations with no
  fallback surface in the log.

> Last updated: 2026-07-24 | Editor: Claude (Opus 5) | Change: new record - an aborted navigation is a benign abort, not a load failure, wherever mullion serves the assets itself (issue #72, refining 0021).
