# 0040. Public preflight errors belong to callers

**Status:** Accepted; refines [0038](./0038-terminal-policy-owns-error-reporting.md)

## Context

Decision 0038 assigns returned Browser/native operation failures to the inner
host terminal policy and reserves adapter-local reports for failures that cannot
be returned. Some public `Run` and `Show` failures occur before that owner exists:
source-plan rejection, unsupported architecture, runtime-discovery architecture
rejection, `beginRun` admission refusal, and the non-Windows unsupported-platform
return.

These preflight and admission paths have a complete return path to the caller.
Reporting them through `Config.Logger` as well would create two owners when the
caller logs the returned error. It would also invoke arbitrary, potentially
re-entrant Logger code from paths that have not entered, or are refusing to
enter, the native lifecycle.

## Decision

Public preflight, admission, and unsupported-platform errors that return before
the inner WebView/native terminal owner are return-only and caller-owned. `Run`
and `Show` do not send those errors through `Config.Logger`.

After the inner boundary, 0038 remains unchanged: returned Browser/native
operation failures are wrapped and reported once by host terminal policy, while
adapter-only or otherwise non-returnable failures are reported locally. This
record refines the outer boundary; it does not supersede Browser/native
ownership.

## Alternatives rejected

**Log every public return through `Config.Logger`.** The documented caller may
already log the returned error, producing duplicate terminal records for one
failure. A Logger may also re-enter host methods, expanding admission and
preflight control flow without adding information the returned error lacks.

**Move every error to caller ownership.** Event delivery, teardown, and inner
Browser/native policy include failures with no usable public return path. This
would discard diagnostics and contradict 0038.

**Report publicly only when a Logger is configured.** Logger presence does not
transfer ownership or reveal whether the caller will also report the return, so
the duplicate and re-entrancy problems remain.

## Consequences

Callers must handle and report public preflight and admission returns. The basic
sample does so with `log.Fatalf("run: %v", err)`. A configured `Config.Logger`
therefore does not imply that it observes every public return.

The ownership transition must stay explicit in future paths: before the inner
terminal owner, return to the caller without logging; after it, preserve 0038's
single terminal owner. New public early returns must not be added to both paths.

## What would change our mind

Supersede this refinement if the public API adopts a structured failure identity
that deduplicates caller and Logger reporting across re-entrant boundaries. A
preflight return also appearing in `Config.Logger`, or an inner non-returnable
failure becoming silent, is the trip-wire.

## Evidence

- Explicit human direction on 2026-08-06 assigns the named public preflight,
  admission, architecture, and platform returns to callers.
- `examples/basic/main.go` reports the error returned by `Run` with
  `log.Fatalf`, independently of the configured `Config.Logger`.
- [Decision 0038](./0038-terminal-policy-owns-error-reporting.md) records the
  retained Browser/native and non-returnable ownership rules.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: record caller ownership for public pre-inner returns while preserving decision 0038's Browser/native terminal boundary.
