# 0015. Maximized geometry insets 1px on an auto-hide taskbar edge

**Status:** Accepted

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

On maximize, mullion insets the work area by exactly **1px** on each edge of the
window's monitor that holds an auto-hide appbar, and feeds that inset work area to
all three maximized paths — `WM_GETMINMAXINFO` (`applyMonitorWorkArea`),
`WM_NCCALCSIZE` (`applyNativeNCCalcClientRect`) and the maximized hit-test.

- Detection is `SHAppBarMessage` (shell32): `ABM_GETSTATE` is a cheap global gate —
  if no auto-hide bar exists anywhere, nothing is queried and nothing is inset —
  then `ABM_GETAUTOHIDEBAREX` per edge, given the monitor rect, reports which edge
  holds one. The monitor comes from the same `monitorInfo` the frame code already
  uses, per the `MonitorFromWindow` warning in §5.
- The inset itself is a pure function, `insetForAutoHideEdges(area, edges)`, locked
  by a headless test on the 1px geometry. It is the identity when no edge has an
  auto-hide bar, so a visible taskbar or none maximizes byte-for-byte as before —
  the change is inert unless an auto-hide bar is actually present.
- All three paths draw their geometry from one inset work area, so the sliver stays
  consistent across them; because `clampRectToArea` is min/max, feeding it the
  already-inset window rect does not inset a second time.

## Alternatives rejected

**Inset only in `WM_GETMINMAXINFO`.** The maximized window would be 1px short, and
`WM_NCCALCSIZE`/hit-test would still clamp to the full work area. It happens to
work because the clamp is idempotent, but it leaves two of the three paths reasoning
about a different rectangle than the window actually occupies — a trap for the next
change. Insetting the shared work area keeps all three honest.

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
- An invariant on the maximized geometry: **the maximized work area is the monitor
  work area inset 1px per auto-hide edge.** Any future change to the three maximized
  paths must route through `maximizeMonitorInfo`, or the sliver is lost again with
  no test to catch it on a machine without an auto-hide taskbar.
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
- The three call sites now read `maximizeMonitorInfo`: `applyMonitorWorkArea`
  (monitor_windows.go), `applyNativeNCCalcClientRect` (nccalc_windows.go),
  `windowRectForMaximizedHitTest` (hittest_windows.go).
- **Not yet verified live.** The reveal behaviour must be confirmed on a real
  machine with an auto-hide taskbar — maximized, the taskbar still pops up on hover
  on the auto-hide edge — on both a primary and a secondary monitor, per
  docs/verification.md. Filed as issue #30 rather than a blind change for exactly
  this reason.
