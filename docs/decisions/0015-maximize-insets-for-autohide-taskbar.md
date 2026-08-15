# 0015. Maximized geometry insets 1px on an auto-hide taskbar edge

**Status:** Accepted. Invariant narrowed by [0019](./0019-maximized-hittest-stays-in-process.md): the maximized hit-test no longer routes through `maximizeMonitorInfo` (issue #36).

## Context

A maximized frameless window is sized to the monitor **work area** so it does not
cover the taskbar (docs/frame-and-dpi.md §2, §5). That is correct for a *visible*
taskbar, which reserves work-area space: `GetMonitorInfo` returns a `rcWork`
smaller than `rcMonitor`, and clamping to it leaves the taskbar on screen.

An **auto-hide** taskbar reserves **no** work area — `rcWork == rcMonitor`. A
window sized to that work area therefore covers the entire monitor, and the shell
suppresses the auto-hide reveal-on-hover for as long as a window exactly covers the
monitor (its fullscreen-app detection). The taskbar becomes unreachable by mouse;
only the Windows key still summons it (issue #30).

`DefWindowProc`'s own maximize path leaves a one-pixel sliver on the auto-hide edge
precisely to keep the reveal alive, and Chromium
(`HWNDMessageHandler::GetClientAreaInsets`), Electron and Windows Terminal all do
the same. mullion overrides both `WM_GETMINMAXINFO` and `WM_NCCALCSIZE`, so it
bypasses that inset and must reimplement it. It did not.

## Decision

At the time of this decision, the inset work area fed all three maximized paths:
`WM_GETMINMAXINFO` (`applyMonitorWorkArea`), `WM_NCCALCSIZE`
(`applyNativeNCCalcClientRect`) and the maximized hit-test. **That three-path
wording is historical after [0019](./0019-maximized-hittest-stays-in-process.md):**
the two sizing paths still receive the inset work area, while the hit-test
clamps the already-inset window rect to the un-inset work area without the
shell probe. This preserves the reveal sliver and keeps `WM_NCHITTEST`
in-process.
On maximize, the current implementation insets the work area by exactly **1px**
on each monitor edge holding an auto-hide appbar. That inset feeds the two
sizing paths (`WM_GETMINMAXINFO` and `WM_NCCALCSIZE`); the hit-test clamps the
already-inset window rect to the un-inset work area in-process.

- Detection is `SHAppBarMessage` (shell32): `ABM_GETSTATE` is a cheap global gate —
  if no auto-hide bar exists anywhere, nothing is queried and nothing is inset —
  then `ABM_GETAUTOHIDEBAREX` per edge, given the monitor rect, reports which edge
  holds one. The monitor comes from the same `monitorInfo` the frame code already
  uses, per the `MonitorFromWindow` warning in §5.
- The inset itself is a pure function, `insetForAutoHideEdges(area, edges)`, locked
  by a headless test on the 1px geometry. It is the identity when no edge has an
  auto-hide bar, so a visible taskbar or none maximizes byte-for-byte as before —
  the change is inert unless an auto-hide bar is actually present.
- The two sizing paths draw their geometry from one inset work area. The
  hit-test clamps the already-inset window rect to the un-inset work area;
  because `clampRectToArea` is min/max, it preserves the same sliver without
  querying the shell on every pointer sample.

## Alternatives rejected

**Inset only in `WM_GETMINMAXINFO`.** The maximized window would be 1px short,
and `WM_NCCALCSIZE` would still reason about a different rectangle. **This
alternative and its three-path wording are historical before 0019:** the
current hit-test intentionally clamps in-process, while the two sizing paths
retain fresh `maximizeMonitorInfo` data.

**Inset unconditionally by 1px whenever maximized.** Simpler — no shell query — but
it steals a pixel from every maximize on the common case (visible taskbar or none),
for no benefit, and would show as a 1px client-height difference nobody asked for.
The inset must be conditional on an auto-hide bar actually being on that edge.

**Leave it as a documented limitation.** Rejected: docs/verification.md already
treats "the taskbar must still be visible while maximized" as an acceptance item,
and an unreachable taskbar is the same class of defect for the auto-hide
configuration. It is a bug, not a boundary.

## Consequences

- A new dependency on **shell32 `SHAppBarMessage`**. It is a stable, decades-old
  shell API and the query is read-only, but it is the first appbar call in the tree
  and is recorded here as a dependency taken on.
- An invariant on the maximized geometry: **the two sizing paths use the
  monitor work area inset 1px per auto-hide edge; the hit-test clamps the
  already-inset window rect to the un-inset area without shell IPC.** Any future
  change to these paths must preserve that split, or the sliver or hit-test
  latency invariant is lost with no test to catch it on a machine without an
  auto-hide taskbar.
- The detection and the actual reveal are Win32/live-only and cannot be exercised
  headlessly (0006). The pure inset is tested; the shell query and the reveal
  behaviour are a live-check obligation on any change to `appbar_windows.go`.

## What would change our mind

- If a future runtime reports an auto-hide taskbar's edge through the ordinary work
  area (a non-zero reserved strip), the clamp alone would suffice and this inset
  would be redundant. It is not the case on any current Windows.
- If the 1px sliver stops being enough for the shell's reveal heuristic (a heuristic
  Microsoft has changed before), the constant `autoHideRevealInsetPX` is the single
  knob to revisit — not the structure.

## Evidence

- `host/appbar_windows.go`: `insetForAutoHideEdges` (pure), `autoHideEdgesForMonitor`
  / `autoHideBarOnEdge` (shell query), `maximizeMonitorInfo` (the shared inset work
  area). `host/appbar_windows_test.go` locks the 1px math headlessly: the no-edge
  identity, each edge independently, all four at once, a secondary-monitor origin,
  and the inversion guard. It fails when the inset is reduced to the identity.
- The two sizing call sites read `maximizeMonitorInfo`: `applyMonitorWorkArea`
  (monitor_windows.go) and `applyNativeNCCalcClientRect` (nccalc_windows.go).
  `windowRectForMaximizedHitTest` (hittest_windows.go) is deliberately
  in-process per [0019](./0019-maximized-hittest-stays-in-process.md).
- **Not yet verified live.** The reveal behaviour must be confirmed on a real
  machine with an auto-hide taskbar — maximized, the taskbar still pops up on hover
  on the auto-hide edge — on both a primary and a secondary monitor, per
  docs/verification.md. Filed as issue #30 rather than a blind change for exactly
  this reason.

> Last updated: 2026-08-15 | Editor: OpenAI (GPT-5.6) | Change: correct pre-0019 three-path wording, state the current two-sizing-path split, and preserve the shell-free hit-test.
