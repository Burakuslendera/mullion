# Behaviour-fix evidence boundary

**Status:** active

This document is the sole operational authority for the evidence boundary and
reporting contract. Rules, the verification router, the command matrix, the
acceptance checklist, and records link here rather than restating this policy.
Records contain observations; they do not grant an exception.

## Contents

- [Normative contract partition](#normative-contract-partition)
- [Evidence classes and ceilings](#evidence-classes-and-ceilings)
- [Required report and exception schema](#required-report-and-exception-schema)
- [Closed native boundary](#closed-native-boundary)
- [Explicit machine lane](#explicit-machine-lane)
- [Allowed residuals and non-criteria](#allowed-residuals-and-non-criteria)

## Normative contract partition

Every behaviour fix MUST be decomposed into its observable contracts. A
**deterministic observable** is anything exposed by a pure function,
deterministic production seam, build-selected path, or source-level guard. Each
such contract MUST have a focused headless regression that exercises the
production path, fails on the unfixed behaviour, and passes on the fix. The
regression remains subject to the no-window invariant; a test-only hook added
only for convenience is not a production-path proof.

Only the exact residual whose truth is irreducibly visual or is the real
window-manager, shell, or compositor result MAY substitute applicable live
evidence for that residual's regression test. If one changed contract has both
deterministic and external portions, the deterministic portion is still
mandatory headlessly. Live evidence never waives it.

A residual is irreducible only when no permitted pure projection, deterministic
seam, build-selected path, or source-level guard can expose the claimed truth
without changing the contract. The implementer MUST identify the partition and
attempt the smallest existing production boundary first. An independent
reviewer or maintainer approves the proposed irreducibility under the existing
quality gate; the implementer cannot self-approve it. This is one review gate,
not a new approval bureaucracy. A protected rule change also follows
[tiered rule maintenance](../../agents/rule-maintenance.md#tiered-rule-change-authority).

## Evidence classes and ceilings

| Class | Minimum artifact | Proves | Cannot prove |
| --- | --- | --- | --- |
| **Automated/headless** | Focused production-path test, guard, build, or command result with its ID and exit status | Pure decisions, state/token/lifecycle seams, ABI/layout and bounded memory fixtures, loader/bridge/publication contracts exposed by a deterministic seam, and the exact source/build path exercised | A real `HWND`, display, WebView2 Runtime or COM implementation, renderer scheduling, message-pump timing, monitor/DPI state, cursor glyph, shell menu, Snap flyout, compositor result, or visual smoothness |
| **Runtime/live** | Same-run target artifact, source identity, OS/build, Runtime identity, setup, exact action, and raw logs/artifact hashes | The observed implementation under that exact Windows/Runtime/configuration; a live external effect only to the extent the action observes it | Broad platform support, reproducibility, another binary/runtime/display, or any unrecorded contract; a live Runtime/COM observation does not replace a deterministic test |
| **Manual/human** | A human's observation of the real window and display, with the applicable checklist scenario and setup | Visible frame, cursor, gesture, shell, window-manager, and compositor outcomes that the human actually sees | Internal state, exact callback/order/log contracts, source identity, ABI/layout, or a deterministic decision seam; a screenshot of a proxy is not visual proof |
| **Scripted-GUI** | A validated DPI-aware harness plus same-run target identity, passive logs, and visual artifact | The native-frame action and passive result captured by that harness, within its validated target/window and cleanup boundaries | HTML/DOM clicks, bridge execution, navigation, browser rendering, or visual claims absent separate relevant frontend/browser evidence; synthetic input is not a human observation |

A scripted harness MUST be validated with both a positive fixture and a
must-fail fixture. It MUST establish controller/child ownership and class-to-PID
identity, check `PostMessageW` return values, serialize cursor/foreground
observations, wait for frontend readiness where a frontend claim is made, and
quit gracefully. The harness rules remain in
[`gui-traps.md`](./gui-traps.md). A scripted native-frame action MUST NOT be
credited as an HTML click, DOM, bridge, navigation, or browser-rendering proof
without separate same-run evidence.

## Required report and exception schema

Every changed contract is reported as a line item with a stable claim,
scenario, and applicable gate ID. The report has four distinct evidence
branches—`automated_headless`, `runtime_live`, `manual_human`, and
`scripted_gui`. Each branch is present when applicable; when it has no result,
write `none — <reason>`, never a generic `N/A`.

1. **`automated_headless`.** Name the deterministic observable, production entry
   path, focused test or command, gate ID, and fail-before/pass-after result.
2. **`runtime_live`.** Name the real Runtime or OS run, same-run source and
   artifact identity, environment, exact action, and observed result.
3. **`manual_human`.** Name the applicable scenario, real display setup, exact
   human action, and observed visual/window-manager/shell/compositor result.
4. **`scripted_gui`.** Name the validated harness, target window identity,
   exact native-frame action and observed result, frontend-ready signal where
   relevant, passive logs, and visual artifacts.
5. **Approved live residual, if any.** Name the exact residual and exactly one
   class from `visual`, `window-manager`, `shell`, or `compositor`; list the
   pure projection, seam, build-path, or source-guard attempts and why each
   cannot expose that residual; name the independent reviewer or maintainer.
6. **Not covered.** Name every exact unexercised boundary; silence, “N/A”, or a
   broad “live not run” is not a boundary.
7. **Still uncertain.** Label each remaining claim `observed`, `likely`,
   `unverified`, or `assumption`, using [policy.md](../../agents/policy.md).

A reproducible report may use this shape (all four branches and the residual
branch remain explicit):

```yaml
claim_id: <stable claim>
scenario_ids: [<stable scenario>]
gate_ids: [<applicable gate>]
automated_headless:
  result: <fail-before/pass-after or none — reason>
  production_path: <entry path or none>
  test_or_command: <name and command or none>
  source_identity: <commit/HEAD and dirty state or none>
  artifacts: [{path: <file>, sha256: <hash>}] # [] when none — reason
runtime_live:
  result: <observed result or none — reason>
  source_identity: <commit/HEAD, dirty state, tagged/untagged pairing or none>
  artifacts: [{path: <file>, sha256: <hash>}] # [] when none — reason
  environment: <OS/build/arch/GPU/Runtime/Config/monitors/DPI/VM-RDP or none>
  action: <exact action or none>
manual_human:
  result: <observed result or none — reason>
  scenario_ids: [<scenario>] # [] when none — reason
  source_identity: <same-run commit/artifact identity or none>
  display_setup: <real window/monitor/work-area/scale-DPI or none>
  action: <exact human action or none>
  artifacts: [{path: <file>, sha256: <hash>}] # [] when none — reason
scripted_gui:
  result: <observed result or none — reason>
  harness_validation: <positive and must-fail fixture or none>
  target_window: <real class/controller-child/PID or none>
  frontend_ready: <signal or none — reason>
  logs_artifacts: <passive logs/visual artifacts and hashes or none>
  action: <exact native-frame action or none — reason>
approved_live_residual:
  class: <none|visual|window-manager|shell|compositor>
  exact_claim: <one residual or none>
  attempts: <seams/projections/build paths/guards considered>
  approver: <independent reviewer or maintainer, or none>
not_covered: [<exact boundary>]
uncertain: [{claim: <claim>, label: <observed|likely|unverified|assumption>}]
proof_ceiling: <exactly what this record does not establish>
```

## Closed native boundary

The default test suite MUST remain headless. It MUST NOT create or destroy an
`HWND` or window class, require an interactive desktop or display, inspect or
mutate process/display/monitor/DPI/input/shell state, enter a native COM
apartment, call a Runtime-owned COM interface, activate the shell or browser,
discover process-global modules, dispatch a native window/message loop, or
require the WebView2 Runtime. An uncontrolled pump or queue is not a fixture.

The allowlist is closed. A Windows-only default test MAY use only:

1. **Bounded test-owned memory/ABI operations:** fixed-size
   `RtlMoveMemory` copies over Go-owned or explicitly paired memory, explicit
   bounded lengths, and `runtime.KeepAlive`; plus paired test-owned
   `CoTaskMemAlloc`/`CoTaskMemFree` COM-memory fixtures. A Windows `uintptr` or
   external address MUST NOT be converted into a Go pointer, and random process
   memory is forbidden.
2. **Bounded Go-owned callback/vtable fixtures:** finite, valid
   `windows.NewCallback`/`ComProc.Call` dispatch against test-created vtables
   in the architecture whose ABI the fixture pins, only for ABI, ownership, and
   synchronous callback contracts. No COM apartment, Runtime-owned pointer,
   browser callback, or unbounded callback allocation is permitted.
3. **Deterministic injected effect seams:** a production boundary whose effect
   is supplied by a deterministic test fake. The fake proves the decision at
   that boundary; it does not bless the native effect behind it.
4. **The implemented isolated self-test subprocess `WM_QUIT` fixture:** only
   the named fixture may own its queue, with one locked thread covering the
   precheck, post, filtered removal, and final check without unlocking or
   reusing that thread during the fixture. It creates no `HWND`, translates,
   dispatches, waits, pumps, queries, or mutates desktop state. No general
   fresh-goroutine queue test is allowed.

Every other Win32 entry point and indirect call remains forbidden in the default
lane, including DPI-awareness state, `GetWindowRect`, display/monitor queries,
DWM, cursor/foreground/shell APIs, window operations, COM initialization,
Runtime discovery/loading, module state, and `PeekMessage`/`GetMessage`/
`TranslateMessage`/`DispatchMessage`/`MsgWaitForMultipleObjectsEx` outside the
named fixture. `CoTaskMemAlloc`/`CoTaskMemFree` are allowed only in the paired
fixture described above.

Decision [0039](../decisions/0039-public-run-preflight-stays-headless.md)
remains accepted and unsuperseded: public `Run` may use its observed
pre-native deterministic seam only when it returns before runtime discovery,
COM, class/`HWND`, every forbidden native boundary, and pumping. That exception
is not a live-evidence exception and does not enlarge this allowlist.

## Explicit machine lane

Machine Runtime discovery/report observations are a separate opt-in lane, never
default headless evidence. `GATE-WEBVIEW2-MACHINE` with
`MULLION_REQUIRE_WEBVIEW2=1` explicitly covers exactly
`TestFindRuntimeOnThisMachine`,
`TestRuntimeExportsTheEntryPointWeCallDirectly`, and
`TestDescribeRuntimeCannotBeSilentAboutTheExport`; absence must fail in that
lane rather than silently skip. Without initial opt-in, record the lane as `not
run / unverified`, never as a pass.
`TestLoadClientFreesTheModuleWhenTheExportIsMissing` belongs to default
`GATE-TEST`, not this machine lane. It does not establish real Windows
`LoadLibrary`/`GetProcAddress`/`FreeLibrary` behavior; that boundary is
explicitly not covered. Machine tests must not create a window, enter a COM
apartment, call a Runtime interface, start a browser, or pump messages.

A machine result proves only the named machine observation. It does not prove
visual hosting, shell behavior, monitor/DPI behavior, or a deterministic
contract that has not also received a focused headless regression.

## Allowed residuals and non-criteria

Examples of permitted substitution are limited to the exact external result:
real cursor/drag/resize appearance after headless hit-test geometry; a real
system-menu presentation; shell Snap placement/reveal; compositor frame
appearance, smoothness, tearing, rounded corners, shadow, or first-painted
glyph. The acceptance procedure lives in
[`acceptance-checklist.md`](./acceptance-checklist.md), not here.

Snap mechanisms MUST be named separately: `SNAP-WIN-Z` is the Windows 11
shell-owned Snap Layout UI, `SNAP-WIN-ARROW` and `SNAP-EDGE-DRAG` are shell Snap
actions, and none proves `SNAP-MAXIMIZE-HOVER`. The latter is conditional on a
supporting native maximize-button profile and requires real mouse hover with a
visible cursor and flyout. The active client-extended
`caption_sysmenu_nccalc` profile has no such path; record that hover as outside
contract, not as a failed test. See the [Snap boundary](../snap-testing-boundary.md).

The following are NOT irreducibility criteria: “hard to test”, “difficult”,
“timing-dependent”, “flaky”, “needs a Runtime, display, or Windows machine”,
native implementation, test cost, convenience, file location, missing harness
support, or a reporter's confirmation. Runtime, COM, callback scheduling,
navigation, logging, loader, bridge, ABI, ordinary OS-call, and network
behavior remain headless-test mandatory whenever a deterministic observable
exists. If no deterministic observable exists outside the four allowed live
classes, report an uncovered gap; do not claim full acceptance.

Compile success, a green deterministic test, bounds/client-rect logs, DWM
readback, DOM state, frame counts, `IsZoomed`, a keyboard shortcut, synthetic
input, or a scripted native-frame action alone is not visual/window-manager/
shell/compositor evidence. A live observation of generic timing or Runtime/COM
callback scheduling is supplemental or `unverified`, never a test waiver.

This boundary is documentation-only and does not alter production behavior,
accepted decision [0003](../decisions/0003-keep-caption-bits.md), or the
caption-style lifetime explanation. Historical evidence retains its original
claims, hashes, uncertainty, and nonclaims.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: require explicit scripted-GUI action fields and opt-in machine-lane reporting while retaining four distinct evidence branches and the default cleanup classification.
