# 0041. WM_NCHITTEST computes auxiliary geometry only for readers

**Status:** Accepted. Extends [0019](./0019-maximized-hittest-stays-in-process.md)
and records the issue #113 hot-path gate.

## Context

Issue #113 measured the Windows/amd64 pre-fix path on an i7-12650H with `hwnd=0`,
where user32 calls fail fast:

```
windowProc(WM_NCHITTEST)        552 ns/op   336 B/op   8 allocs/op
windowProc(WM_SETCURSOR)        457 ns/op   296 B/op   4 allocs/op
  nativeTooltipTraceReady()     330 ns/op   256 B/op   2 allocs/op
  the hittest Debug Sprintf     333 ns/op    80 B/op   1 alloc/op
  nativeCaptionButtonHit alone   92 ns/op    40 B/op   3 allocs/op   result discarded
  one LazyProc.Call (isZoomed)   78 ns/op    16 B/op   2 allocs/op
```

The message path read `MULLION_TOOLTIP_TRACE` with `os.Getenv` on every sample,
formatted a Debug line before calling a logger, and always computed an auxiliary
caption candidate even when no production reader consumed it. The candidate
repeats `GetWindowRect`, `IsZoomed`, `GetDpiForWindow` and, when maximized,
monitor/rect work. A live `HWND` is strictly more expensive than the measured
`hwnd=0` fast-fail case.

The source audit resolves the candidate-reader conflict: `AllButtons` and
ordinary `MaximizeOnly` policy routing consume the DWM result. The candidate is
read by tooltip trace formatting/tracking, and by the `MaximizeOnly` plus
caption-passthrough diagnostic branch. Therefore policy being non-Disabled is
not, by itself, a reader.

## Decision

Sample `MULLION_HITTEST_DIAG` and `MULLION_TOOLTIP_TRACE` once at package
initialization and never reread them per message. Keep `fmt.Sprintf` inside the
hit-test diagnostic gate because `Logger.Debug` receives a preformatted string,
including for `NopLogger`. Evaluate the candidate query only when
`tooltipTraceReady || (policy == MaximizeOnly && captionPassthroughReady)`;
otherwise pass `HTCLIENT` as a sentinel. Keep the gate as the one predicate that
all future candidate readers must extend. The hit-test's existing native
precedence and default `DefWindowProc` behavior remain unchanged.

## Alternatives rejected

**Read `os.Getenv` per message.** Rejected because Windows performs a real
`GetEnvironmentVariableW` and allocates a temporary UTF-16 buffer; a diagnostic
switch does not justify that cost on every pointer sample. Package-init latches
are immutable in production.

**Format eagerly, then rely on a no-op logger.** Rejected because the Logger API
accepts an already formatted string and a `NopLogger` cannot undo the allocation.
The `fmt.Sprintf` call stays inside the diagnostics branch.

**Query the native candidate unconditionally.** Rejected because the result was
discarded for the default policy and for `AllButtons`/ordinary `MaximizeOnly`.
The query duplicates rect, zoom, DPI and monitor work. A reader-gated composition
seam proves that unread paths make zero calls and each real reader makes one.

**Change `LazyProc.Call` or pointer escape behavior.** Rejected: the measured
16 B/two allocations for one call are the mechanism that keeps pointers pinned
across the syscall. The accepted optimization is fewer calls, not a changed
Win32 calling convention.

## Consequences

The common disabled-diagnostic path pays package-latched boolean reads, pure
project geometry and no diagnostic formatting or auxiliary candidate query. A
trace or caption-passthrough diagnostic still pays the native `LazyProc.Call` and
Win32 geometry cost; that is an accepted observability cost. The candidate
sentinel is safe only when no reader exists. Tests that substitute either latch
must run sequentially and must restore the package state; production never
mutates it.

The policy/candidate relationship is intentionally explicit in
`nativeCaptionButtonHitNeeded` and the composed `nativeCaptionButtonHitIfNeeded`
seam. Any future caption-policy, tooltip, or diagnostic consumer that inspects
`candidateHit` must update that predicate, the policy matrix, the lazy-call
counter test and the canonical [hit-test reference](../hit-test.md). A separate
call-site gate is a trip-wire failure.

## What would change our mind

- A source audit or test showing `AllButtons` or ordinary `MaximizeOnly` begins
  consuming candidate geometry would require extending the reader predicate and
  its headless matrix.
- A new production reader that needs candidate details in the disabled policy
  would invalidate the `HTCLIENT` sentinel assumption.
- A measured live regression showing the package-init latch or gate harms a
  required diagnostic, or a Go/Windows runtime change removes the allocation cost,
  would reopen the design.
- A native API that supplies the same button geometry without the duplicated
  rect/DPI/monitor queries would justify reconsidering the candidate query shape,
  but not moving it back to every `WM_NCHITTEST` message.

## Evidence

- Issue #113's `testing.AllocsPerRun` and amd64 measurements above identify the
  environment, formatting and discarded-candidate costs; the `hwnd=0` setup does
  not claim live Win32 timings.
- `TestNativeCaptionButtonHitNeededOnlyForReaders` locks disabled, trace,
  passthrough, `MaximizeOnly` and `AllButtons` policy combinations.
- `TestNativeCaptionCandidateCompositionIsLazyAndReaderComplete` runs the
  composed production seam headlessly with a counting query: unread is zero,
  tooltip trace is one, and caption passthrough is one.
- `TestNativeCaptionCandidateUnreadPathDoesNotAllocate` locks zero allocations
  for the unread composition path; `TestNativeHitTestDiagnosticDisabledPathDoesNotAllocate`
  locks zero allocations for the disabled pure formatter.
- `TestNativeHitTestDiagnosticMessageGate` and the latch tests lock diagnostic
  gating and no environment reread after package initialization. No test creates
  an `HWND` or calls a Win32 entry point.

> Last updated: 2026-08-15 | Editor: OpenAI (GPT-5.6) | Change: record issue #113 reader gates, costs, rejected alternatives and future trip-wires.
