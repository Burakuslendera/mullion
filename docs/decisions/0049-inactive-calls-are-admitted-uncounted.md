# 0049. A call made while no Run is active takes no counted admission

**Status:** Accepted

## Context

Public `Host` methods other than `Run` are reachable from any goroutine, and
`Config.Logger` is embedder code that may call any of them back, including
`Run`. An exported method admits itself through a short counted entry
(`enterRun` increments `runCalls`), releases the mutex, and only then invokes
the Logger. While a Run is active that shape is sound: a genuinely concurrent
`Run` is rejected before any wait, and `endRun` drains the count before
teardown completes.

The count was unconditional. Before the first `Run`, or after a previous `Run`
returned, an inactive call such as `Hide` still incremented `runCalls`, and
`beginRun` drained that count before it would start a new session. An inactive
call's synchronous Logger callback could therefore re-enter `Run`: `beginRun`
saw no active Run, waited for `runCalls` to reach zero, and the only side able
to decrement was the outer call's deferred `leaveRun` — unreachable while the
Logger, and therefore the inner `Run`, was still inside it. Neither side could
progress. The cycle is pure Go synchronization: no Runtime, COM, HWND, message
pump, or scheduler timing is involved, and it was deterministic on every
execution.

`df13580` (PR #119) introduced the shape: it counted the public controls'
entries and added the generation-zero drain in `beginRun` alongside the
active-Run teardown drain, and its tests covered an active Run's Logger
re-entry into another method but not an inactive method's Logger re-entry into
`Run`. Decision 0046 solved the adjacent mutex-held emit deadlock (issue #140)
and explicitly left this mechanism open.

## Decision

Only an entry made while a Run is active joins the counted admission set. An
entry made while no Run is active is admitted uncounted: it waits on nothing
and nothing waits on it. `beginRun` no longer drains generation-zero work, so
no `Run` ever begins by waiting on an admission an inactive call holds. An
inactive call's window effects were always absent, and its tagged commands are
still rejected once a Run owns a token — the process-global token/HWND gate is
what keeps stale work out of a later session, so the drain it replaced
protected nothing the gate does not.

An entry's count and its release are paired on the admission value itself
(`counted`), and `enterOriginatingRun` returns the admission to leave, so a
library-owned callback's count cannot be severed from the run it was admitted
into.

## Alternatives rejected

**Keep counting inactive calls and abandon or time out the drain.** A wait
that must be abandonable to avoid a deadlock cannot guarantee the ordering it
exists for, and a timeout turns a deterministic hang into an occasional
corrupted session. Go also exposes no goroutine identity a wait could use to
recognise "the count's holder is the caller", so the drain could never be made
safe for the very re-entrant schedule it deadlocked on.

**Remove the count entirely.** `endRun`'s wait for already-admitted active-Run
methods and callbacks is the teardown completeness guarantee issues #97,
#118 and #119 built; without the count, teardown could poison the token and
reset state under a still-running public call. The active-Run count stays
exactly as it was.

**Keep the drain and count only active-Run entries.** The drain would then
never wait for anything: with inactive entries uncounted, `runCalls` is
already zero whenever `running` is false, because `endRun` drains the count
before it clears `running`. Dead code that reads as a guarantee is worse than
no code.

## Consequences

Between one `Run` returning and the next beginning, public calls no longer
serialize ahead of the next session's reset. The in-process effects of such a
call — a readiness latch, a diagnostic record, a log line — land un-ordered
against that reset, and the next session's own resets decide the final state;
only the tagged-command routes keep their stale-generation rejection, because
the token/HWND identity they compare is keyed to a Run that owned a token. A
future public method that mutates per-Run state outside those routes inherits
this un-ordered window and must either gate itself on the active Run or accept
the reset's verdict.

Inactive calls are marginally cheaper: no counted entry and no condvar
bookkeeping on the way out.

## What would change our mind

- A requirement that an inactive public call's effect be observable and ordered
  before the next Run's reset rather than absent — that would need a
  generation-zero session identity with its own admission, not a resurrected
  unconditional count.
- A mechanism by which an inactive call could still carry a window effect: the
  tagged routes' generation-zero rejection would need re-auditing first.
- Evidence that a library-owned callback can be armed while no Run is active
  with a generation-zero admission and must be drained before the next reset;
  `enterOriginatingRun` currently admits such a capture uncounted, and nothing
  in production arms one.

## Evidence

- [Issue #159](https://github.com/Burakuslendera/mullion/issues/159) (P0
  blocker, regression introduced by `df13580`) reports the cycle; the wait
  chain above is its mechanism.
- `host/inactive_run_admission_windows_test.go` is headless and creates no
  window: a one-shot Logger re-enters public `Run` from an inactive `Hide`,
  against a host whose non-loopback `Config.URL` makes `Run` return a
  deterministic preflight error before runtime discovery. On the unfixed tree
  both schedules — before the first `Run`, and after a completed headless
  generation — failed bounded at 2.00 s with the re-entrant `Run` never
  returning, while the plain-Logger control and the active-Run `host is already
  running` rejection control passed. On the fixed tree all four pass.
- The active-Run schedules are unchanged in their assertions:
  `TestLoggerMayReenterHostMethodWhileTeardownWaits`,
  `TestBeginRunRejectsConcurrentCallDuringTeardown` and
  `TestReadinessAdmittedBeforeTeardownCompletesInsideOriginatingRun` pass
  unmodified.

> Last updated: 2026-09-12 | Editor: ZCode (GLM-5.3-Flash) | Change: create the record — a call made while no Run is active takes no counted admission; beginRun no longer drains generation-zero work (issue #159).
