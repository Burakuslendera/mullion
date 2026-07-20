# Frame and DPI

## Contents

- [1. The frameless frame](#1-the-frameless-frame)
- [2. `WM_NCCALCSIZE`](#2-wm_nccalcsize)
- [3. `SetWindowPos` and the `SWP_FRAMECHANGED` trap](#3-setwindowpos-and-the-swp_framechanged-trap)
- [4. `WM_NCHITTEST`](#4-wm_nchittest)
- [5. `WM_GETMINMAXINFO`](#5-wm_getminmaxinfo)
- [6. Per-monitor DPI v2](#6-per-monitor-dpi-v2)
- [7. `WM_DPICHANGED`](#7-wm_dpichanged)
- [8. Restore flicker (`WM_ERASEBKGND`)](#8-restore-flicker-wm_erasebkgnd)
- [9. Bounds sync](#9-bounds-sync)
- [10. Measuring client coverage (a tooling trap)](#10-measuring-client-coverage-a-tooling-trap)
- [11. Chromium zoom must be off](#11-chromium-zoom-must-be-off)
- [Debugging checklist](#debugging-checklist)

How mullion builds a frameless window out of a perfectly ordinary Win32 window,
and why almost every line of that code exists because of a specific bug. Read this
before changing `host/hittest_windows.go`, `host/nccalc_windows.go`,
`host/monitor_windows.go`, `host/dpi_windows.go` or `host/style_windows.go`. The
rules below look arbitrary and are
not; each one is written as **symptom -> root cause -> fix**.

Relevant config: `TitlebarHeight` (default 36), `CaptionControlsWidth` (138),
`ResizeBorder` (8), and the escape hatches `HitTestTitlebarHeight` /
`HitTestCaptionControlsWidth`. All values are **logical** pixels. Set
`MULLION_HITTEST_DIAG=1` to log every hit-test decision.

## 1. The frameless frame

A frameless window is not a window without a caption. It is a normal captioned
window whose client area has been extended over the caption.

**Keep `WS_CAPTION` and `WS_SYSMENU` set.** The style bits mullion applies are
`WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX`.

- **Symptom of dropping them:** minimize, maximize and restore snap between
  states with no animation; the window feels like a dialog from another era. Snap
  behaviour (edge snap, Snap Layouts, the shell's window-management affordances)
  degrades or disappears.
- **Root cause:** DWM and the shell decide what a window *is* from its style
  bits. A window without `WS_CAPTION` is not a captioned top-level window as far
  as the shell is concerned, so it gets no shell animation and no full Snap
  contract. `WS_SYSMENU` is what makes the system menu (right-click on the title
  bar, `Alt+Space`) and its shell plumbing exist at all.
- **Fix:** keep the bits, and remove the *visual* frame elsewhere — in
  `WM_NCCALCSIZE`, by growing the client rect over the non-client area. The shell
  still sees a well-formed captioned window; the user sees only your HTML.

## 2. `WM_NCCALCSIZE`

This is the message that makes the frame disappear. Windows sends the proposed
**client** rect for a given **window** rect and asks you to shrink it by the
frame. Return the rect unshrunk and the client area covers the whole window,
caption included. Two rules:

**Handle every `wParam`.** Windows sends `WM_NCCALCSIZE` with `wParam == TRUE`
(rects valid, full negotiation) and with `wParam == FALSE` (single rect). Extend
the client area only in the `TRUE` case and the `FALSE` case silently hands the
frame back, so a thin strip of native caption flickers into existence during some
transitions. Preserve the client extension in both.

**Clamp to the work area when — and only when — maximized.**

- **Symptom:** maximized, the top of the custom title bar is cut off; the window
  looks pushed a few pixels off the top of the screen. Restored, it is fine.
- **Root cause:** when a window is maximized, Windows deliberately positions the
  HWND so the invisible resize frame hangs *outside* the monitor work area — a
  standard-frame window hides that overhang under its own border. A frameless
  window has no border to hide it with, so the overhang eats your title bar.
- **Fix:** if `IsZoomed()` is true, intersect the first proposed rect with the
  nearest monitor's **work area** before writing it back. Do not do this when
  restored — the restored rect is already correct, and clamping there just moves
  the window.

## 3. `SetWindowPos` and the `SWP_FRAMECHANGED` trap

After changing style bits you must call
`SetWindowPos(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED | ...)` so the new frame is
recalculated. The flags you pass alongside it are load-bearing:

```
SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE | SWP_FRAMECHANGED
```

- **Symptom:** the app launches, the window is the right size, and the web
  content renders into a postage stamp in the corner — a client rect of roughly
  46x39 px. The frame looks correct. The WebView looks broken. You will spend a
  long time inside WebView2 before you find this.
- **Root cause:** the `0, 0, 0, 0` position and size arguments above are not
  ignored by default. Without `SWP_NOMOVE | SWP_NOSIZE` they are *applied*, and
  the window collapses to its minimum tracking size. What you then see is a
  correctly sized outer window (repainted later) whose client rect was computed
  while it was 46x39.
- **Fix:** never call `SWP_FRAMECHANGED` without `SWP_NOMOVE | SWP_NOSIZE`. If a
  bounds log ever shows `client_width=46`, this is the bug — look here first.

## 4. `WM_NCHITTEST`

The hit-test is the entire interaction contract of a frameless window. It runs on
the **native** side, against the window rect, in physical pixels.

**Resize band.** `ResizeBorder` is 8 *logical* px, scaled by the active window's
DPI at hit-test time (`GetDpiForWindow`); corners take priority over edges. Do
**not** implement this band in CSS: a CSS band drifts away from the native regions
the moment any scale factor is involved (DPI, a CSS transform, browser zoom), and
the user gets a resize cursor over a region that does not resize. The native hit
test owns the geometry; the frontend may paint matching cursor affordances on top
of it. **Skip the band when maximized** — a maximized window cannot be edge-resized,
and leaving it live produces resize cursors along the screen edge.

**The title bar strip is `HTCAPTION`; the caption button cluster is `HTCLIENT`.**

- **Symptom:** the close/maximize/minimize buttons in the HTML title bar cannot
  be clicked — or worse, clicking them drags the window.
- **Root cause:** `HTCAPTION` means "non-client caption", and mouse input over it
  goes to the frame, never to the WebView. Anything the frontend must handle has
  to be `HTCLIENT`.
- **Fix:** the rightmost `CaptionControlsWidth` logical px of the title bar band
  return `HTCLIENT`; the rest of the band returns `HTCAPTION` and gives you native
  drag, double-click-to-maximize and the system menu for free.

**The delegated-`HTCAPTION` trap (maximized).**

- **Symptom:** maximized, the user click-drags somewhere in the *content* — well
  below the title bar — and the window restores and starts following the mouse.
- **Root cause:** any hit-test result you leave to `DefWindowProc` is computed
  against the *native* caption geometry, and for a maximized window Windows can
  report a caption region far larger than your 36px strip. Anything reported as
  `HTCAPTION` is a drag handle, and dragging a maximized window restores it.
- **Fix:** never return a delegated caption result unclamped. Clamp `HTCAPTION` to
  exactly the title bar strip — `top <= y < top + TitlebarHeight` of the
  work-area-clamped window rect (§2). Everything below it is `HTCLIENT`, whatever
  `DefWindowProc` thinks.

If the frontend's title bar is scaled by CSS and can no longer match the native
band, set `HitTestTitlebarHeight` / `HitTestCaptionControlsWidth` rather than
skewing the CSS. That is what those fields are for; most applications never touch
them.

## 5. `WM_GETMINMAXINFO`

A maximized frameless window must not cover the taskbar, so the natural fix is to
fill `MINMAXINFO` from the monitor's **work area** (`MaxPosition` relative to the
monitor origin, `MaxSize` = work-area size). That is correct as far as it goes,
but two things make an override actively dangerous:

- **Do not swallow the message in a subclass.** Handling `WM_GETMINMAXINFO` and
  returning `0` from a subclass proc means the handler underneath you (a
  framework's, a toolkit's) never runs, and its min-size logic silently
  disappears. Pass the message down the chain.
- **`MonitorFromWindow` can lie during maximize.** Mid-maximize it may still
  report the *previous* monitor while the frame calculation elsewhere in the same
  operation uses `MonitorFromRect` and gets the new one. On a multi-monitor setup
  the window rect and the client rect then get computed against two different
  monitors, and you get a window sized for monitor A on monitor B.

In practice the `MINMAXINFO` the system fills in is already right — it already
uses the correct monitor's work area, and the maximized client rect already lands
exactly on it. **An unnecessary override is a net loss.** Override only if you
have measured an actual taskbar overlap, and if you do, take the monitor from the
same source the rest of your frame code uses.

**The auto-hide taskbar exception.** An auto-hide taskbar reserves *no* work area,
so `rcWork == rcMonitor` and the clamp above sizes the maximized window to the whole
monitor. The shell then treats it as a fullscreen app and stops revealing the
taskbar on hover — it becomes unreachable by mouse. The fix is the same one
`DefWindowProc` and Chromium apply: leave a 1px sliver on the auto-hide edge. mullion
detects an auto-hide appbar per monitor edge (`SHAppBarMessage`) and insets the
maximized work area by 1px there, feeding that inset area to the two paths that size
the window (`WM_GETMINMAXINFO`, `WM_NCCALCSIZE`). It is inert when no auto-hide bar
is present. See docs/decisions/0015. The maximized hit-test deliberately does *not*
run the `SHAppBarMessage` probe — `WM_NCHITTEST` is the hottest input path and the
probe is synchronous shell IPC; it clamps the already-inset window rect to the
un-inset work area instead, which preserves the sliver because the clamp is min/max.
See docs/decisions/0019.

## 6. Per-monitor DPI v2

**Set process DPI awareness before any HWND exists.**
`SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)` runs at the very top of host
construction — before the window class is registered, before the main HWND, before
any tray/hidden HWND, and long before the WebView2 child.

- **Symptom:** text renders noticeably heavier and blurrier than in a reference
  browser or terminal. Nothing in the CSS explains it. Layout is subtly larger
  than it should be.
- **Root cause:** the process is DPI-*unaware*. mullion creates the controller
  with raw-pixel bounds (`COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS`) and
  monitor-scale auto-detection **off**, which is correct for a host that owns its
  own DPI handling — but only if the process itself is per-monitor aware. In an
  unaware process the OS virtualises coordinates, that raw-pixel contract is fed
  the wrong numbers, and the whole chain runs at the wrong scale. The CSS is
  innocent.
- **Fix:** set awareness first, and *verify it* before you go looking at
  stylesheets. A diagnostic table from the frontend settles it in one shot:

| Variant | `devicePixelRatio` | `innerWidth/Height` | `outerWidth/Height` | Process awareness |
| --- | ---: | --- | --- | --- |
| Unaware (bug) | `1` | `1200x800` | `1200x800` | `0 / 0` |
| PMv2 (correct, on a 150% monitor) | `1.5` | `1200x800` | `1200x801` | `2 / 2` |

If `devicePixelRatio` is `1` on a scaled monitor, stop reading CSS: the awareness
call did not happen, or something created an HWND before it did. Awareness cannot
be changed once a window exists.

The latch also means a *second* host in the same process — or an application that
declared `PER_MONITOR_AWARE_V2` itself before constructing the host — sees
`SetProcessDpiAwarenessContext` refuse a context the process is already in. Since
issue #48 that already-correct context counts as success: `New` checks the current
context before treating the refusal as an error (`host/dpi_windows.go`), and `Run`
re-verifies the awareness on the thread that creates the window
(`host/host_windows.go`). A context that is genuinely different stays the fatal
error it always was.

**Initial size and placement are computed, not defaulted.**

- **Symptom:** the window opens near the primary monitor's top-left, at a
  slightly different spot each launch, and on a 125% monitor it is 20% smaller
  than the configured size. `WM_DPICHANGED` never repairs it — it fires on a
  DPI *change*, not at birth.
- **Root cause:** `CreateWindowEx` received `CW_USEDEFAULT` (the shell's
  cascade) and the raw logical `Config.Width`/`Height` as physical pixels, so
  the logical-pixel contract was never applied at creation (issue #59).
- **Fix:** before the `HWND` exists, resolve the primary monitor, read its
  effective DPI (`GetDpiForMonitor` — valid because awareness is already set,
  above), scale the logical size and center it in the monitor's **work area**
  (`host/placement_windows.go`; the math is the pure `centeredPlacement`, pinned
  by headless tests). An unresolvable monitor falls back to `CW_USEDEFAULT` with
  a logged warning. See [decisions/0018](./decisions/0018-initial-placement-centered-on-primary.md).

## 7. `WM_DPICHANGED`

Windows hands you a **suggested rect** in `lParam`: the position and size the
window should have at the new DPI, already scaled, same physical size. **Apply it
verbatim** — `SetWindowPos` to exactly that rect, then re-sync the WebView bounds.
Do not multiply it by anything.

- **Symptom:** dragging the window from a 100% monitor to a 150% monitor makes it
  grow *more* than the scale factor. Moving it back and forth gives inconsistent
  sizes — hysteresis: it shrinks on one monitor and comes back bigger on the other.
- **Root cause:** compounding. Windows already scaled the suggested rect; applying
  your own `* dpi / 96` on top of it multiplies the error at every crossing, and it
  never converges back.
- **Fix:** keep the "apply the suggested rect" step in a **pure function** so the
  contract is testable. mullion keeps two: `dpiChangedTargetSize(suggested)` — the
  size actually applied, which is the suggested size unchanged (add a scale factor
  here and the identity test fails); and `dpiRescaleLength(length, from, to)` — the
  *model* of how a length should scale, never used for layout, existing only so
  tests can state the contract.

Lock the behaviour down with three tests:

1. **Identity** — the applied size equals the suggested size, exactly.
2. **Round trip** — 96 -> 120 -> 96 is lossless (no hysteresis).
3. **No compounding** — repeating a transition N times does not drift.

Log the transition too — `old_dpi`, `new_dpi`, `zoomed`, previous rect, suggested
rect — on every `WM_DPICHANGED`. One line makes a double-scale bug visible
immediately; without it, a user report of "the window grows" is unfalsifiable.
Caveat: by the time `WM_DPICHANGED` arrives Windows has **already** updated the
window's DPI, so `GetDpiForWindow` returns the *new* value. Track the last known
DPI yourself if you want a truthful `old_dpi`.

**The window rect is only half of a DPI change — the WebView content has its own
scale.** Applying the suggested rect resizes the *window*; it does nothing about the
scale the *frontend* renders at. That scale is the WebView2 controller's
**rasterization scale** — the page's `devicePixelRatio` — and mullion owns it for
the same reason it owns the bounds.

- **Symptom:** drag the window from a 125% monitor to a 100% one (or throw it there
  with `Win`+`Shift`+`←`) and the page is suddenly too large for the window,
  overflows, and grows a scrollbar; the window frame itself is the right physical
  size. The reverse — 100% to 125% — makes the content too small. A single-monitor
  session never shows it, which is why it survived until a mixed-DPI report.
- **Root cause:** the controller runs in raw-pixel bounds mode with
  `PutShouldDetectMonitorScaleChanges(false)` (see
  [decisions/0011](./decisions/0011-host-owns-rasterization-scale.md)). Microsoft is
  explicit that in that configuration *"the app must update the RasterizationScale
  property itself"*, and that the scale *"should be updated when the DPI scale of the
  app's top level window changes (i.e. monitor DPI scale changes or window changes
  monitor)"*. Turning detection off without then setting the scale leaves the content
  frozen at whatever scale the controller was created with — correct on the starting
  monitor, wrong on every other.
- **Fix:** on `WM_DPICHANGED`, and once at embed, set
  `controller.PutRasterizationScale(dpi/96)`. `rasterizationScaleForDPI` is the pure
  function that computes it and `syncRasterizationScale` pushes it. Do this **and**
  apply the suggested rect — neither substitutes for the other. In raw-pixel mode the
  two are independent (Microsoft: *"Changing the rasterization scale in this mode
  won't change the raw pixel size"*), so their order does not matter, but both must
  happen.

The scale is a function of the **absolute** DPI (`dpi/96`), never of a delta, so it
cannot compound across monitor hops the way a mishandled rect can (the double-scale
trap above). That is the invariant the `rasterizationScaleForDPI` tests pin.

> **Known gap:** Microsoft's rasterization scale is *monitor DPI scale × user text
> scale*; mullion sets only the DPI part, consistent with the rest of its DPI-based
> geometry. A non-default system **text** scale is therefore not reflected yet.
> `unverified` — there is no report against a raised text-scale factor to size the
> gap.

## 8. Restore flicker (`WM_ERASEBKGND`)

- **Symptom:** maximize -> restore shows a one-frame flash of blank background.
  Subtle, but it is the difference between "native" and "web app in a window".
- **Root cause:** `WM_ERASEBKGND` left to `DefWindowProc` makes Windows erase the
  client area with the class background brush; the WebView then repaints at the
  new size one frame later.
- **Fix:** the WebView covers the entire client area, so there is nothing to
  erase. Handle `WM_ERASEBKGND` and `return 1` (erased, nothing to do).

## 9. Bounds sync

Nobody maintains the WebView2 controller's bounds but you. Re-sync them (client
rect -> `controller.PutBounds`) on **all** of:

| Trigger | Why |
| --- | --- |
| embed | first bounds after controller creation |
| show | the window may have been resized while hidden |
| navigation completed | content exists; size must be right before the first paint |
| frontend ready | last chance to catch a mismatch — warn if the surface is tiny |
| `WM_SIZE` | the obvious one |
| `WM_MOVE`, `WM_MOVING` | monitor change mid-drag; also notify position changed |
| `WM_DPICHANGED` | the client rect changes with the frame |

Miss one and you get the classic failure: correct window, content offset or
clipped. `WM_MOVING` in particular is what keeps the surface aligned while the
user drags a restored window across a monitor boundary. Move/size sync is hot —
coalesce the *logging*, not the sync.

## 10. Measuring client coverage (a tooling trap)

Verification scripts that check "does the WebView cover the client area" must pick
the right child HWND.

- **Symptom:** an automated resize check reports a coverage FAIL after a
  programmatic resize. The window looks perfect on screen and the controller's own
  bounds are correct.
- **Root cause:** Chromium creates several child HWNDs. The one named
  *"Intermediate D3D Window"* reports a **stale** (often much larger) rect after a
  programmatic resize. A script that measures "the largest child HWND" picks that
  one and compares a stale rect against the new client rect.
- **Fix:** measure only the controller child — the one whose class starts with
  `Chrome_WidgetWin`. Never select a child by size. This is a bug in the *test*,
  not in the window; the "largest child" pattern is latently broken and must not
  be reintroduced.

## 11. Chromium zoom must be off

On the WebView2 settings object — note that the two live on *different* interfaces
in the settings chain, so pinch zoom is reached through a `QueryInterface` for
`ICoreWebView2Settings5` and is a no-op on a runtime that does not offer it:

```go
settings.PutIsZoomControlEnabled(false)   // ICoreWebView2Settings
settings5.PutIsPinchZoomEnabled(false)    // ICoreWebView2Settings5
```

- **Symptom:** after an accidental `Ctrl+scroll`, the title bar no longer lines up
  with the drag region, the caption buttons are near but not under the cursor, and
  the resize band is off by a few pixels.
- **Root cause:** user zoom rescales the CSS layer only. The native hit-test
  regions (`TitlebarHeight`, `CaptionControlsWidth`, `ResizeBorder`) are computed
  from logical px and DPI and know nothing about it. The two coordinate systems
  drift apart, and no event lets the native side follow along reliably.
- **Fix:** disable zoom control and pinch zoom at controller setup. If you need a
  zoom feature, scale the UI with your own CSS variables — a scale factor you own
  can be mirrored into `HitTestTitlebarHeight` / `HitTestCaptionControlsWidth`;
  Chromium's cannot.

## Debugging checklist

| Symptom | Look at |
| --- | --- |
| Content in a tiny corner (~46x39 client) | `SWP_FRAMECHANGED` without `SWP_NOMOVE\|SWP_NOSIZE` (§3) |
| Title bar clipped when maximized | `WM_NCCALCSIZE` work-area clamp (§2) |
| Caption buttons not clickable | hit-test returns `HTCAPTION` over the button cluster (§4) |
| Maximized window restores when dragging content | unclamped delegated `HTCAPTION` (§4) |
| Text heavy/blurry, layout too big | DPI awareness not PMv2, or set too late (§6) |
| Window grows across monitors / hysteresis | double-scaling the suggested rect (§7) |
| Content too big/overflows (scrollbar) after moving to another monitor | rasterization scale not updated on the DPI change (§7) |
| Blank flash on restore | `WM_ERASEBKGND` not handled (§8) |
| Content offset after a drag or DPI change | a missing bounds-sync trigger (§9) |
| Hit regions off after `Ctrl+scroll` | Chromium zoom still enabled (§11) |
| Coverage check fails but the app looks fine | the script measures "Intermediate D3D Window" (§10) |

> Last updated: 2026-07-19 | Editor: Claude (Fable 5) | Change: §6 gains the computed initial placement — DPI-scaled size centered in the primary work area, `CW_USEDEFAULT` only as fallback (issue #59, decision 0018).
