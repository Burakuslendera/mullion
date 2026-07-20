# 0019. The maximized hit-test never queries the shell

**Status:** Accepted. Narrows the invariant of [0015](./0015-maximize-insets-for-autohide-taskbar.md).

## Context

Decision 0015 routed all three maximized-geometry paths — `WM_GETMINMAXINFO`,
`WM_NCCALCSIZE` and the maximized hit-test — through `maximizeMonitorInfo`, whose
auto-hide detection calls `SHAppBarMessage`. That call is synchronous cross-process
IPC to Explorer.

The first two paths fire once per maximize. The third does not: `WM_NCHITTEST`
fires continuously while the pointer is over the non-client band, and the window
procedure calls both `nativeHitTest` and `nativeCaptionButtonHit` on each one. On a
maximized window every hit-test therefore made **two** `SHAppBarMessage` round
trips (`ABM_GETSTATE`), and up to **ten** when an auto-hide bar was present
(`ABM_GETSTATE` + four `ABM_GETAUTOHIDEBAREX`, twice) — on the UI thread, on the
hottest input path in the library. If Explorer is busy or hung, hit-testing — and
with it caption interaction and drag — stalls. Before 0015 this path was pure
in-process arithmetic (`MonitorFromWindow` + `GetMonitorInfoW`). Filed as issue
#36, a regression introduced by 0015's own fix.

## Decision

The maximized hit-test derives its rect from `monitorInfoForWindow` — the
in-process monitor query — and clamps the actual window rect to the **un-inset**
work area. It never calls `maximizeMonitorInfo`. The two maximize-geometry paths
(`WM_GETMINMAXINFO`, `WM_NCCALCSIZE`) keep routing through `maximizeMonitorInfo`
unchanged; they are the paths that size the window, they fire rarely, and they must
stay fresh.

The reveal sliver is not lost by this. The window rect the hit-test clamps was
already inset when the window was sized (`WM_GETMINMAXINFO` applied the inset work
area), and `clampRectToArea` is min/max: clamping an inset rect to the un-inset
work area returns it unchanged. When the two sources disagree — the monitor query
failed at maximize time, so the window truly covers the full work area — the
hit-test now reasons about the rect the window *actually* occupies, which is the
more correct answer for input routing.

0015's invariant is narrowed accordingly: **the maximize-geometry paths route
through `maximizeMonitorInfo`; the hit-test path must stay in-process, and a change
that reintroduces a shell query on it is a regression of issue #36.** The routing
is locked headlessly: `monitorInfoForWindow` and `autoHideEdgesForMonitor` are seam
variables (test-substituted, never reassigned in production), and
`TestWindowRectForMaximizedHitTestStaysInProcess` counts shell probes on the real
hit-test path — the monitor seam succeeds headlessly, so the probe path is
reachable and the zero is deterministic on any machine.

## Alternatives rejected

**Cache the auto-hide edges and invalidate on notification.** Keeps 0015's
"one geometry source" symmetry and was the issue's first suggestion. Rejected on
invalidation surface: `WM_SETTINGCHANGE` covers taskbar setting changes, but a
third-party appbar registering or unregistering (`ABM_NEW`/`ABM_REMOVE`) notifies
only registered appbars — mullion is not one — so no broadcast reaches the cache.
A stale cache either kills the reveal sliver or steals a pixel, silently, in
exactly the configuration 0015 exists for; that is this library's worst failure
class. The cache also adds mutable state and an invalidation matrix that cannot be
verified headlessly, to optimise two paths (`WM_GETMINMAXINFO`, `WM_NCCALCSIZE`)
that fire once per maximize and can afford the fresh query.

**Keep querying per event.** The regression itself. No precedent does this:
`DefWindowProc`, Chromium and Windows Terminal consult appbar state on frame
calculation, never on `WM_NCHITTEST`.

**Route the hit-test through the inset area but memoise per maximize.** A cache by
another name, with the same staleness and more moving parts tied to window state
transitions.

## Consequences

- The three maximized paths no longer share one literal geometry source — the
  symmetry 0015 bought is spent. What they share instead is the invariant that the
  clamp cannot undo the inset, and that is now locked by a test rather than by
  code shape (`TestWindowRectForMaximizedHitTestStaysInProcess`,
  `TestMaximizeMonitorInfoInsetsAutoHideEdges`).
- Two package-level seam variables exist solely so the routing is provable under
  decision 0006 (no test creates a window). Production code never reassigns them;
  a new call site for either should default to the variable, not the `query*`
  function behind it.
- If the hit-test ever genuinely needs auto-hide awareness — a hit region *on* the
  1px sliver, say — it must come from cached state, never from a per-event shell
  query. That future design pays the invalidation cost this record declined.

## What would change our mind

- A Windows build whose `SHAppBarMessage` is no longer cross-process (in-process
  shell state, or a documented fast path) would remove the latency argument —
  though not the Explorer-hang robustness argument.
- A reliable broadcast that covers third-party appbar registration would make the
  cache alternative sound, and with it the restored symmetry of one geometry
  source for all three paths.
- A measured input-latency problem on the maximize paths themselves (they still
  pay the IPC once per maximize) would reopen the cache design with the same
  constraints.

## Evidence

- Issue #36: the regression report, with the full call path and round-trip counts
  verified against the merged code of `f7c29ac`.
- `host/hittest_windows.go` (`windowRectForMaximizedHitTest`): the in-process
  routing and the invariant comment. `host/monitor_windows.go` /
  `host/appbar_windows.go`: the seam variables.
- `TestWindowRectForMaximizedHitTestStaysInProcess` (`hittest_windows_test.go`):
  fails against the pre-fix routing with `shell probed 2 times on the hit-test
  path, want 0` *and* a 1px geometry shift — observed by temporarily restoring
  `maximizeMonitorInfo` in the hit-test and watching both assertions fire.
- `TestMaximizeMonitorInfoInsetsAutoHideEdges` (`appbar_windows_test.go`): the
  maximize-geometry paths still inset — the 0015 wiring, previously live-only, now
  locked headlessly.
- Live check, 2026-07-20, on the fix build (`devel (bbcf145)`, `examples/basic`,
  120 DPI): maximized caption buttons, titlebar drag, snap-maximize and restore all
  correct with a visible taskbar (log: `SessionWarnCount=0, SessionErrorCount=0`);
  with the taskbar set to auto-hide, the reveal still pops on hover while
  maximized — the sliver survives the re-route. Primary monitor; the sizing paths
  that produce the sliver are byte-identical to the 0015 code verified live on
  both monitors at `f7c29ac`.
