# 0055. Startup visibility is a controller-first bounded transaction

**Status:** Accepted

## Context

The parent HWND and WebView2 controller have independent visibility. The old
show path exposed and foregrounded the parent before `PutIsVisible(TRUE)`. A
controller failure therefore left a blank parent visible, and the startup gate
re-armed without a bound.

## Decision

Visibility is controller-first. The current Run, HWND and Browser must remain
the same after every external effect. Only a successful controller show permits
parent exposure, update, bounds publication and the visible log.

Automatic startup owns one retry per Run: at most two application attempts.
Retry is allowed only from a proven safe-hidden state. Failure to establish that
state, or the second failed attempt, tears the window down through the ordinary
owner and makes `Run` return `ErrWindowVisibilityUnavailable`. Explicit `Show`
never creates or spends the automatic budget. Cancellation, stale ownership and
destruction are not visibility failures and never re-arm the gate.

Each Show or Hide dispatch establishes the newest per-Run visibility intent.
External effects and Logger calls must return to the same intent before an
operation may publish, roll back or retry. Hide invalidates both pending timers
and already-posted startup attempts, so an older automatic Show cannot reopen a
window against the caller's newer intent.

Terminal visibility teardown invalidates the current intent before diagnostics
and refuses every later Show admission. Destruction is requested before the
terminal Error Logger runs, so Logger reentrancy cannot revive the transaction.

## Alternatives rejected

**Terminal on the first failure.** Smallest, but it discards a transient
one-shot controller failure even though the existing gate intentionally permits
recovery.

**Unbounded hidden retry.** It avoids the visible blank parent but can retain a
Run forever and repeatedly invoke a failing Runtime operation.

**Parent-first plus rollback.** It still permits a compositor-visible blank
interval. Controller-first removes that exposure for the primary failure.

## Consequences

The retry counter is Run state, not a second meaning for `ShowTimeout`. A parent
or update failure after controller success rolls controller and parent back; an
unprovable rollback is terminal. Public `Show` and the private WM result remain
compatible: success is still 1 and failure 0, with the UI-thread owner reporting
the cause once.
An explicit `Show` does not retry, but an unsafe rollback is still terminal;
the visibility transaction cannot leave a potentially exposed partial state.

## What would change our mind

A supported Runtime contract that makes controller visibility atomic with its
parent, or field evidence that one retry materially harms recovery, would reopen
the order or budget. A real transient needing more than one retry requires its
own measured policy; it does not justify an unbounded loop.

## Evidence

- Issue #160 and the production show transaction tests.
- Headless tests cover ordering, safe-hidden rollback, the one-retry bound,
  terminal ownership, explicit Show, re-entrant Show/Hide intent, stale timer
  rejection and cancellation.
- Real Runtime HRESULT state, pixels, compositor timing, foreground behaviour
  and first paint remain live-only evidence.

> Last updated: 2026-09-22 | Editor: OpenAI (GPT-5.6) | Change: make startup visibility controller-first, bounded and fail-closed (issue #160).
