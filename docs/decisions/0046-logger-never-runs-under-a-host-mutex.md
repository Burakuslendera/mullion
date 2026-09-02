# 0046. The Logger never runs while holding a host-owned non-reentrant mutex

**Status:** Accepted

## Context

`Config.Logger` is embedder code. Its `Info`, `Warn` and `Error` implementations
may do anything, including call back into `Host` methods. The host's mutexes —
`startupMu`, `runMu` and the state they guard — are plain `sync.Mutex` values,
and `sync.Mutex` is not reentrant: a goroutine that locks a mutex it already
holds never returns.

Issue #140 found the one place where that collided. `logStartupTimingSummary`
called `host.log.Info` while holding `startupMu` (host/startup_timing_windows.go,
former line ~80). The chain was deterministic: the Logger callback called `Show`
→ `applyShowAfterEnsure` → `recordStartupWindowVisible` → `recordStartupTiming`
→ `startupMu.Lock()` on the same goroutine that already held the mutex — a
non-reentrant self-deadlock, so `recordStartupFrontendReady` never returned and
startup stalled. Because the wait has no timeout and nothing is logged, the
failure presents as a vanished startup, ending in the runtime's
`all goroutines are asleep - deadlock!` when nothing else can run. A full audit
of every `host.log.*` call site against every host lock found this to be the
only violating site: the rule is already stated where the other re-entry paths
are handled (host/startup_show_windows.go:83-85 and
host/host_windows.go:403-407), and the timing summary was its single exception.

Issue #159's inactive-method re-entry through the `beginRun` drain is a
different mechanism, remains open, and is not covered by this record.

## Decision

No Logger call may run while a host-owned non-reentrant mutex is held. State
that must be read or mutated under the lock — including any once-flag that
gives an emitted line emit-once semantics — is latched into an immutable
snapshot under the lock, and the Logger call happens after the lock is
released. Concretely, `logStartupTimingSummary` splits into
`snapshotStartupTimingSummaryLocked` (runs under `startupMu`, latches
`timing.logged` once, returns an immutable `startupTimingSummary` value) and
`emitStartupTimingSummary` (called after unlocking, producing the
byte-identical message). A constraint comment at the split point states the
invariant: a Logger callback can re-enter Host methods that take the same
non-reentrant mutex, which would deadlock the emitting goroutine.

## Alternatives rejected

**Re-entrant or goroutine-identity admission.** Let the mutex recognise its
holder and let the re-acquire succeed. Go deliberately exposes no goroutine
identity a mutex could compare against, and OS-thread identity is the wrong
key because goroutines migrate between threads. The deeper objection is that
it weakens `endRun`'s wait-for-admitted-work guarantee: a held mutex would no
longer mean one holder finishing its critical section, and re-entrant
acquisition lets Logger code observe and mutate protected state mid-way
through that section.

**Document-and-prevent only.** Keep the call under the lock and rely on the
stated rule. Rejected because a silent deadlock is this project's worst
failure class: the goroutine stops, nothing is logged, and a reader sees a
startup that never completes. Only a fail-first test distinguishes that from
any other hang, and the snapshot/emit split is what makes the re-entry
deterministically testable headlessly rather than a rule everyone must
remember.

**Make `startupMu` re-entrant.** A custom recursive mutex would have let the
issue #140 chain complete — and would have hidden the invariant violation
instead of surfacing it. Every future holder of the lock would pay re-entrancy
bookkeeping to accommodate one bug, and a lock that tolerates re-entry invites
exactly the mid-critical-section re-entry the rule exists to prevent.

## Consequences

The timing summary is built from a small immutable struct, and the emit path
runs outside the lock. The `WarnCount()`/`ErrorCount()` evaluation now happens
just after unlocking instead of under it; the bases themselves were always read
under `startupMu` (in `beginRun`) and reach the emit path inside the snapshot.
The counters are atomic and were never guarded by `startupMu`, so the values
are equivalent.

The cost every future change pays: when lock-protected state must be logged,
snapshot under the lock and emit after unlocking — a Logger call site added
without auditing it against every host lock reintroduces the issue #140
deadlock class. The rule is enforced three ways: the headless re-entry test,
the constraint comment at the split point, and this record.

## What would change our mind

- A requirement that a log line be ordered with a lock-protected event — a
  reader must never observe the state without the line already emitted. The
  answer remains "not a re-entrant mutex": a deferred log pump that queues the
  line under the lock and drains it immediately after releasing preserves the
  invariant while keeping the ordering at the pump boundary.
- An embedder requirement that Logger callbacks legitimately need
  lock-protected timing state. The snapshot struct grows; the lock hold does
  not.
- Go exposing a usable goroutine identity would reopen the admission
  alternative, and only if re-entrant acquisition could still preserve
  `endRun`'s wait-for-admitted-work guarantee.

## Evidence

- [Issue #140](https://github.com/Burakuslendera/mullion/issues/140) (P0
  blocker) reports the deadlock; the chain above is its mechanism.
- `host/startup_timing_reentry_windows_test.go` is headless and creates no
  window. On the unfixed tree the re-entry test failed in 2.01 s:

  ```
  --- FAIL: TestStartupTimingSummarySurvivesLoggerReentrantShow (2.01s)
      startup_timing_reentry_windows_test.go:54: recordStartupFrontendReady deadlocked behind a Logger re-entry while startupMu was held
  ```

  On the fixed tree it passes in 0.01 s. Control
  `TestStartupTimingSummaryEmitsOnceForPlainLogger` asserts the summary is
  still emitted exactly once, and that a repeated readiness record stays
  silent.
- `go build ./...` and `go vet ./...` passed; `go test ./host -run
  'TimingSummary|StartupTiming|FrontendReady' -v -count=1` passed 9/9;
  `go test ./host -count=1` passed (ok 143.9 s); `go test ./... -count=1`
  passed all 7 packages.
- The full `host.log.*` / host-lock audit found no other site calling the
  Logger while holding a host lock.

> Last updated: 2026-09-02 | Editor: ZCode (GLM-5.3-Flash) | Change: create the record — no Logger call runs under a host-owned non-reentrant mutex; snapshot under the lock, emit after unlocking (issue #140).
