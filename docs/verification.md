# Verification and Acceptance

Status: index

## Contents

- [Automated gates](./verification/automated-gates.md#automated-gates)
- [Evidence boundary](./verification/evidence.md#normative-contract-partition)
- [Manual acceptance checklist](./verification/acceptance-checklist.md#manual-acceptance-checklist)
- [Diagnostics](./verification/diagnostics.md)
- [GUI verification traps](./verification/gui-traps.md)
- [Verification records](./verification/records.md)
- [Startup gates and watchdog](./startup-gates-and-watchdog.md)
- [Bug reports](./bug-reports.md)
- [Snap testing boundary](./snap-testing-boundary.md)
- [Snap and non-client behavior](./snap-and-nonclient-region.md)
- [Guard verification](./guard-verification.md)
- [Publication evidence](./publication-evidence.md)

How a change to `mullion` is proved correct. The evidence boundary is owned by
[evidence.md](./verification/evidence.md): every deterministic observable
contract needs a focused headless regression, and only an approved irreducibly
visual, window-manager, shell, or compositor residual may substitute applicable
live evidence. The stricter rules in [agents/window.md](../agents/window.md)
remain in force for frame, DPI, and Snap changes.

The executable command matrix lives only in
[automated-gates.md](./verification/automated-gates.md). The complete manual
actions and stable scenario IDs live only in
[acceptance-checklist.md](./verification/acceptance-checklist.md). Diagnostic
switch semantics, GUI-harness limits, and recorded observations have their own
owners; this file only routes to them.

## 1. Automated gates

Compatibility pointer: use
[automated-gates.md](./verification/automated-gates.md#automated-gates), the sole
command matrix and gate-ID authority.

## 2. Why "it compiles" is not acceptance

Compatibility pointer: use
[evidence.md](./verification/evidence.md#evidence-classes-and-ceilings) for
proof classes, ceilings, and required reporting.

## 3. Manual acceptance checklist

Compatibility pointer: use
[acceptance-checklist.md](./verification/acceptance-checklist.md#manual-acceptance-checklist)
for all live actions, preconditions, observables, ceilings, and scenario IDs.

## 4. Traps when scripting GUI checks

Compatibility pointer: use [gui-traps.md](./verification/gui-traps.md), the sole
GUI-harness boundary.

## 5. Diagnostic build tags and env switches

Compatibility pointer: use [diagnostics.md](./verification/diagnostics.md), the
sole diagnostic switch and tagged-build reference.

## 6. What a good bug report contains

Compatibility pointer: use [bug-reports.md](./bug-reports.md), the retained
issue-report authority.

## 7. 2026-08 verification records

Compatibility pointer: use [records.md](./verification/records.md), which routes
to the current and archived month/issue records. Records are observations, not
acceptance rules.

Startup behavior remains owned by
[startup-gates-and-watchdog.md](./startup-gates-and-watchdog.md); Snap behavior
and proof ceilings remain owned by the retained Snap documents; guard/publication
authorities remain at their root paths. Do not copy their rules or historical
evidence into this router.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: reduce verification to a stable index and route commands, evidence, live scenarios, diagnostics, GUI traps, and records to single owners.
