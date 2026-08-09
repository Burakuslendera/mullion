# 0038. Terminal policy owns each error report

**Status:** Accepted

## Context

A WebView2 operation crosses an adapter and a host policy boundary. If the adapter
both reports a failure and returns it, the host wraps and reports the same terminal
failure again. Duplicate errors inflate session counts and obscure which layer
classified the outcome. Some failures cannot be returned: an event adapter can
fail before a callback is safe, and teardown can encounter a secondary error
while returning a different primary failure.

## Decision

A Browser operation that returns an error does not also send that error through
`ErrorCallback`. It returns the HRESULT unchanged for the caller to wrap. The
terminal host policy reports the resulting operation failure once.

The adapter reports locally only when no caller can return the failure: an event
cannot safely reach its host callback, an adapter-only policy operation
fails, or a secondary cleanup failure cannot replace the primary returned error.
`ShuttingDown` likewise reports a controller-close error locally because it has no
error return. Warning-only optional capability misses stay on `WarningCallback`.

## Alternatives rejected

**Report at every layer.** This preserves local context but produces two terminal
records for one failure and makes warning/error counters describe plumbing rather
than events.

**Return every error and remove callbacks.** Event delivery and teardown have no
return path to host policy. Dropping their failures would make terminal adapter
faults invisible.

**Always join cleanup failures into the primary.** Some cleanup occurs after the
primary has already selected its return path, and `ShuttingDown` has no return.
Changing a primary failure into teardown bookkeeping also hides the actionable
cause.

## Consequences

Ownership depends on control flow rather than component identity: adapter code is
not automatically the reporter. New Browser methods must choose return or local
reporting, never both. Host wrappers must add operation context and report at the
terminal policy boundary. Secondary cleanup diagnostics can be separate records
because they are distinct failures, not duplicates of the returned primary.

## What would change our mind

Supersede this record if the public error surface becomes a structured causal
trace that deduplicates one failure identity across layers while preserving every
context frame. A regression test observing `ErrorCallback` for an error that the
same Browser call returns, or two host terminal lines for one operation, is the
trip-wire.

## Evidence

- `internal/webview2/browser_windows_test.go`:
  `TestReturnedSurfaceOperationFailuresAreNotInternallyReported` and
  `TestRegisterEventsFailureTearsDownBrowser` lock return-without-report;
  `TestHandleWebResourceRequestedReportsGetRequestFailure` and teardown tests
  lock adapter-only and non-returnable cleanup reports.
- `host/webview_windows_test.go`:
  `TestFilterFailureCleansUpLogsOnceAndAllowsRetry` proves the mandatory filter
  failure is uncommitted, shut down, returned without an inner report and
  retryable.
- `host/host_windows_test.go`: `TestRunPreStartFailureHasOneTerminalReporter`
  drives the real pre-loop return/defer boundary and counts one terminal line.
- `host/webview_event_observations_windows_test.go`: Show's UI-thread handler owns
  one report; the public `Show` return path adds no generic duplicate.
- `host/errorsurface_logging_windows_test.go` and
  `host/navigation_report_source_test.go`: terminal navigation classification
  owns one report and the completion callback adds none.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: lock Run and Show to one terminal owner and keep returned filter failures out of inner reporters.
