# 0047. Verification evidence is contract-partitioned and native fixtures are closed

**Status:** Accepted

## Context

The verification rules carry two truths that must be read together. A behaviour
fix has deterministic contracts—geometry, state, routing, ABI, logging, bridge,
and source-guard decisions—that can be observed without a desktop. Other
contracts are true only as a visible result of a real window manager, shell, or
compositor. The former need a focused production-path regression; the latter
need the applicable live observation.

The old wording made those truths compete. An absolute “every behaviour fix is
locked by a test” sentence could reject an irreducibly visual result, while a
broad “live-only” description could let a Runtime, callback, timing, routing, or
ABI contract skip a deterministic test. The default headless rule also had to
be precise about a few existing test-owned ABI and memory fixtures without
turning “no `HWND`” into permission to inspect desktop state or run an
uncontrolled message pump.

The documentation split creates the same maintenance pressure. Evidence
ceilings were repeated in rules and templates, while records mixed observations
with instructions. One operational owner is needed so a boundary is updated in
one place, and all callers can remain concise without losing the no-window
invariant or the existing independent-review quality gate.

## Decision

Every behaviour fix is partitioned into observable contracts. Each contract
exposed by a pure function, deterministic production seam, build-selected path,
or source-level guard has a focused headless regression that exercises the
production path and fails before the fix and passes after it. Only the exact
residual whose truth is irreducibly visual or is the real window-manager, shell,
or compositor result may substitute applicable live evidence. Mixed contracts
retain their mandatory headless portion. Irreducibility is proposed by the
implementer and approved by the existing independent reviewer or maintainer;
“hard to test” and implementation difficulty are not approval criteria.

The default native boundary is closed to four cases only: bounded test-owned
memory/ABI copies and paired test-owned `CoTaskMemAlloc`/`CoTaskMemFree`;
bounded Go-owned callback/vtable fixtures with no COM apartment,
Runtime-owned pointer, browser callback, or unbounded allocation; deterministic
injected effect seams; and the implemented isolated self-test subprocess
`WM_QUIT` fixture. The last fixture owns one locked thread for its post,
filtered removal, and final check and performs no window operation, pump,
translation, dispatch, wait, or desktop query. DPI/display/DWM/shell state,
`HWND`s, real Runtime/COM apartments, process-global module discovery, and
uncontrolled queues remain outside the default lane.

`docs/verification/evidence.md` is the sole operational authority for this
partition, the four evidence classes, proof ceilings, closed native allowlist,
explicit machine lane, and reproducible exception/report schema. Rules and the
PR template state concise obligations and link to it; the verification router,
command matrix, and acceptance checklist own navigation and procedures; records
own observed results only. This is a responsibility-preserving documentation
consequence, not a second approval system or a product-behaviour change.

Decision [0039](./0039-public-run-preflight-stays-headless.md) remains accepted
and unsuperseded. Its deterministic pre-native public `Run` exception does not
expand through this decision. Accepted decisions, including [0003](./0003-keep-caption-bits.md),
retain their existing meaning.

## Alternatives rejected

**Keep the absolute test sentence and require no live substitution.** This
would make an exact cursor, shell, or compositor outcome impossible to prove by
its own observable while preserving the words “locked by a test.” It confuses a
headless proof ceiling with a lack of evidence.

**Label a whole fix, Runtime path, or native file “live-only.”** This would let
pure policy, geometry, callback, navigation, logging, bridge, loader, ABI, or
source-guard contracts escape a regression. A live launch cannot prove a hidden
wrong branch did not occur.

**Allow any Win32 call that avoids an `HWND`.** DPI/process state, monitor and
DWM queries, shell activation, module discovery, and queue operations remain
machine-global or scheduling-sensitive. Absence of a window is not isolation.

**Use a fresh goroutine, a production pump, or a real Runtime as a general
fixture.** Thread scheduling does not by itself establish queue ownership, and
translation/dispatch/pumping can consume unrelated messages or block. Runtime
and COM implementation behavior belongs to the explicit machine or live lane.

**Copy the full boundary into each rule, template, or record.** Duplicated
obligations drift and make records read like acceptance rules. A single evidence
owner with short links preserves the contract without adding approval
bureaucracy.

**Supersede 0039 or alter 0003 to make the new wording fit.** The pre-native
`Run` exception and caption-bit invariant remain valid; this decision explains
how evidence is partitioned around them rather than changing either choice.

## Consequences

Every changed contract now carries a line-item proof burden: a production-path
headless test for each deterministic observable, or a narrowly named live
residual with the exact class, seam attempts, independent approver, same-run
identity and environment, action/result, not-covered boundary, and uncertainty
label. Live Runtime, COM, callback-scheduling, timing, or machine observations
are supplemental when their deterministic portion is testable.

The closed native list permits maintainable ABI and ownership fixtures without
blessing desktop state. Its permanent cost is that a proposed native test not
matching one of the four cases is rejected from the default lane; it needs a
pure seam, an explicit machine lane, or a future decision. The missing-export
production cleanup seam belongs to default `GATE-TEST` evidence, while real
Windows `LoadLibrary`/`GetProcAddress`/`FreeLibrary` behavior remains
uncovered. The opt-in Runtime machine lane has exactly three discovery/report
tests.

The one-owner topology imposes a durable maintenance rule: edit
`evidence.md` for boundary semantics, the command/checklist owners for
procedures, and records for what actually happened. Rule files and templates
must link rather than grow duplicate taxonomies. This decision itself records
why that ownership is necessary; it does not turn the router or records into a
second evidence authority.

## What would change our mind

- A permitted deterministic seam or pure projection reliably exposes a residual
  currently classified as visual, window-manager, shell, or compositor. That
  contract would require a focused headless regression, and the boundary would
  be narrowed through a new decision.
- A live failure shows that an allegedly external residual is actually caused by
  deterministic policy, routing, geometry, ABI, or state. The residual would be
  removed from the exception and its deterministic portion made mandatory.
- A maintained, reproducible isolation mechanism proves an additional native
  fixture cannot inspect or mutate desktop state, cannot enter a Runtime/COM
  apartment, cannot pump or dispatch, and cannot depend on process-global module
  state. Its evidence would justify a separately reviewed boundary change.
- A product requirement makes the permanent reporting and closed-lane cost
  unacceptable while supplying a reliable deterministic or isolated replacement
  for the proof currently obtained by live evidence.

## Evidence

- The current verification material separates deterministic headless contracts
  from the live acceptance checklist in [acceptance-checklist.md](../verification/acceptance-checklist.md#manual-acceptance-checklist);
  its recorded boundary is the reason this decision makes the partition explicit
  rather than treating a file location as proof.
- [0039](./0039-public-run-preflight-stays-headless.md) records the accepted
  pre-native public `Run` exception and its requirement to observe forbidden
  seam non-reachability; this decision preserves that requirement.
- Existing production-path proof patterns include the source-plan and
  unsupported-architecture preflight tests, fixed-size memory tests in
  `host/memory_windows_test.go`, Go-owned COM callback/vtable tests in
  `internal/webview2/*_windows_test.go`, deterministic completion seams in
  `internal/webview2/script_completion_windows_test.go`, and the deterministic
  missing-export cleanup seam `TestLoadClientFreesTheModuleWhenTheExportIsMissing`.
  The real Windows loader calls that the seam does not exercise are explicitly
  not covered; this record makes no unrun test or live-result claim.
- The Snap-specific proof ceiling remains in
  [snap-testing-boundary.md](../snap-testing-boundary.md), and uncertainty and
  independent-review obligations remain in [policy.md](../../agents/policy.md).
- The Tier-3 change and this combined decision were explicitly approved for the
  2026-09-04 verification cutover. No production frame, caption, Runtime, or
  COM behavior is changed by this documentation decision.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: align the decision with the explicit four-branch report schema, default cleanup seam, and three-test Runtime machine lane.
