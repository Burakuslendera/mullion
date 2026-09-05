# Verification records

**Status:** index

This is the sole index and lifecycle router for dated and issue-specific
verification records. Records preserve what a run observed; they do not replace
acceptance rules or evidence boundaries. Use the [evidence boundary](./evidence.md)
for the required proof classes, report fields, and proof ceilings.

## Current

- [2026-09 records](./records/2026-09.md) — **Status:** active; append current
  command results, live observations, and explicit coverage boundaries here.

## Archived months

- [2026-08 records](./records/2026-08.md) — **Status:** archived; frozen
  historical observations and nonclaims.

## Issue records

- [Issue #135 paired exact-tree live verification](./records/issues/issue-135-paired-live.md)
  — **Status:** archived; durable artifact identity, chronology, and proof
  ceilings for the paired acceptance.

## Lifecycle

The active month is the destination for new dated records. When a month closes,
mark it `archived`, create the next active month, and update this index. Issue
records hold durable evidence whose identity or chronology must remain discoverable
independently of a monthly file. A monthly entry may link an issue record, but the
same evidence body must not be copied into both.

Corrections are append-only: record a new dated correction with its source
identity, exact changed claim, and remaining uncertainty rather than silently
rewriting a historical observation. Keep records subordinate to the applicable
subsystem authority, decision, acceptance checklist, and [evidence boundary](./evidence.md).

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: establish the canonical dated and issue-record index and lifecycle router.
