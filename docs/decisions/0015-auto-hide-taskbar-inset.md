# 0015. The maximized client rect is inset one pixel on an auto-hide taskbar's edge

**Status:** Accepted

## Context

mullion answers `WM_NCCALCSIZE` itself (decision 0003) and returns a client rect
that covers the whole window, so the frameless client is the painted surface with
no native chrome behind it. For a maximized window it clamps that rect to the
monitor **work area** (`host/nccalc_windows.go`, `clampRectToArea`), which keeps a
*visible* taskbar uncovered.

An **auto-hide** taskbar is different: it reserves no work-area space, so
`GetMonitorInfo` reports the work area as the full monitor rect. The clamp is then
a no-op, and the maximized client covers the exact monitor edge the taskbar hides
against. Windows' shell suppresses the auto-hide reveal-on-hover for a foreground
window that covers the whole monitor edge, so the taskbar becomes unreachable by
mouse for as long as the window stays maximized (the Windows key still shows it).

`DefWindowProc`'s own maximized `WM_NCCALCSIZE` avoids this by leaving a one-pixel
sliver on the auto-hide edge. By overriding `WM_NCCALCSIZE` and returning the full
rect, mullion loses that compensation and owns the edge. This was observed as a
tracked defect (issue #30) before the fix.

## Decision

`applyNativeNCCalcClientRect` insets the maximized client rect by one pixel on each
monitor edge that holds an auto-hide taskbar. It detects the edges through the
shell — `SHAppBarMessage` with `ABM_GETSTATE` (a cheap global gate) and, when some
taskbar is auto-hide, `ABM_GETAUTOHIDEBAREX` per edge against the monitor rect
(`host/appbar_windows.go`). The window still fills the monitor; only the client
gives up the sliver, exactly as `DefWindowProc` does. The detection is Win32; the
inset itself is a pure function (`insetForAutoHideEdges`) that the tests lock.

## Alternatives rejected

- **Shrink the maximized *window* in `WM_GETMINMAXINFO` instead.** Reducing
  `MaxSize` by one pixel would also defeat the shell's full-monitor detection, but
  it leaves a one-pixel strip of desktop showing on the edge and makes the window
  not-quite-maximized. Insetting the *client* keeps the window maximized and
  matches what `DefWindowProc` and every custom-frame browser shell (Chromium's
  `HWNDMessageHandler`, Electron, Windows Terminal) do.
- **`ABM_GETAUTOHIDEBAR` (the non-EX form).** It answers for the primary monitor
  only, so a secondary display with an auto-hide taskbar would be missed. The EX
  form takes the monitor rect and is per-monitor.
- **Document it as a known limitation.** The library exists to get frame edges
  right for its consumers; a maximized window that traps the taskbar is a
  user-visible defect, not a footnote.

## Consequences

- A new dependency on `Shell32!SHAppBarMessage`. It is a stable, long-standing
  shell API, called only on the maximized `WM_NCCALCSIZE` path.
- An invariant future work must preserve: **the maximized client is deliberately
  one pixel short on an auto-hide edge.** A refactor that "cleans up" the inset,
  or that stops calling `autoHideTaskbarEdges`, reintroduces #30. The pure inset
  is test-locked so such a change fails a test rather than silently shipping.
- The one-pixel strip shows the window background colour on the auto-hide edge
  while maximized. It is what `DefWindowProc` produces and is invisible in
  practice.
- `ABM_GETAUTOHIDEBAREX` requires Windows 8.1 or later; mullion targets Windows 10
  and 11, so this is not a constraint. A shell that cannot answer the query falls
  back to no inset — the pre-fix behaviour, not a crash.

## What would change our mind

- A WebView2 or Win32 API that lets a custom frame keep `DefWindowProc`'s auto-hide
  compensation without reimplementing it would make this code redundant.
- Dropping the `WM_NCCALCSIZE` override (decision 0003) — if the frame were ever
  built another way — would remove the reason this inset exists at all.

## Evidence

Issue #30 carries the analysis and the arithmetic. The fix is `host/appbar_windows.go`
(`autoHideTaskbarEdges`, `SHAppBarMessage`) and `host/nccalc_windows.go`
(`insetForAutoHideEdges`, wired into `applyNativeNCCalcClientRect`).
`TestInsetForAutoHideEdgesReservesASliverPerEdge` (`host/nccalc_windows_test.go`)
locks the one-pixel contract and fails against a no-op inset. The shell detection
(`SHAppBarMessage`) and the actual taskbar reveal are Win32/live-only and cannot be
exercised headlessly; they need a live check on a machine with an auto-hide taskbar,
on both a primary and a secondary monitor.

> Last updated: 2026-07-16 | Editor: Claude (Fable 5) | Change: new record for the auto-hide taskbar one-pixel inset (issue #30).
