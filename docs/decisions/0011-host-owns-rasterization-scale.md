# 0011. The host owns the WebView2 rasterization scale

**Status:** Accepted

## Context

The controller is configured for a host that does its own DPI: raw-pixel bounds
mode (`COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS`) and
`PutShouldDetectMonitorScaleChanges(false)`, both set in
`webview2.applyBoundsPolicy`. The reasoning was to stop WebView2 and the host from
each reacting to the same DPI change and compounding the scale.

That handled the window *bounds*. It silently left a second axis unowned. WebView2
exposes two independent DPI-facing properties on `ICoreWebView2Controller3`:
`Bounds` (the surface rectangle) and `RasterizationScale` (the content's
`devicePixelRatio`). Microsoft's own documentation is explicit about who owns the
second one when detection is off:

> "When false, the WebView will not track monitor DPI scale changes, and **the app
> must update the RasterizationScale property itself.**" — `get_ShouldDetectMonitorScaleChanges`

> "This value should be updated when the DPI scale of the app's top level window
> changes (i.e. monitor DPI scale changes or window changes monitor)…" —
> `get_RasterizationScale`

The host set the bounds and never set the scale. So the scale stayed at whatever
the runtime assigned when the controller was created — correct on the starting
monitor, and wrong on every monitor with a different DPI. The window rect resized
correctly on a monitor hop while the content kept the old scale: on a lower-DPI
monitor the page was too large and overflowed with a scrollbar, on a higher-DPI one
too small. A single-monitor session never exposed it. This is issue #1.

## Decision

The host owns the rasterization scale exactly as it owns the bounds. It sets
`RasterizationScale = DPI/96` — `rasterizationScaleForDPI` computes it,
`Browser.SetRasterizationScale` applies it through `ICoreWebView2Controller3`, and
`Host.syncRasterizationScale` is the sibling of `syncWebViewBounds` that drives it —
at two moments:

- **once at embed**, from the window's current DPI, so the first paint is correct
  even when the window opened on a non-primary monitor; and
- **on every `WM_DPICHANGED`**, from the new DPI in the message's `wParam`, before
  the bounds are re-synced.

`ShouldDetectMonitorScaleChanges` stays `false`. The host now owns *both* DPI axes
rather than one.

## Alternatives rejected

**Let WebView2 detect scale changes (`ShouldDetectMonitorScaleChanges(true)`).** The
obvious "just let the runtime do it" — and it would set the scale correctly on its
own. But the host feeds `Bounds` in raw physical pixels and applies the
OS-suggested rect on `WM_DPICHANGED` itself; with detection on, WebView2 would also
react to the same monitor change, and the two owners would fight over the same
transition. Detection is off *on purpose*; the price of that decision is that the
host must set the scale, which is what this record records rather than leaves as a
comment in one function.

**Switch to `COREWEBVIEW2_BOUNDS_MODE_USE_RASTERIZATION_SCALE`.** In that mode
`Bounds` is a logical size and WebView2 derives raw pixels as
`logical × RasterizationScale`. It would couple the two properties WebView2-side.
Rejected because the host computes every rectangle it touches — client rect, hit
test, resize band, `WM_NCCALCSIZE` — in physical pixels, and handing the surface
sizing back to a logical-times-scale product would put a second DPI multiply back
in the path this project spent §7 of `frame-and-dpi.md` removing. Raw pixels keep
the bounds math and the scale math separate and each testable on its own.

## Consequences

**Every DPI transition now has two obligatory host actions, not one:** apply the
suggested rect, and set the scale. A future change that drops the
`syncRasterizationScale` call — mistaking it for redundant next to the bounds sync —
reintroduces issue #1, invisibly on any single-monitor machine and in every
headless test. The `rasterizationScaleForDPI` tests lock the *value* (and that it
cannot compound, being a function of the absolute DPI), but they cannot prove the
call is wired in; that half is a live check, on a mixed-DPI setup, in
`docs/verification.md`.

**Only the DPI half of the scale is set.** Microsoft defines the rasterization
scale as *monitor DPI scale × user text scale*; the host sets `DPI/96` and ignores
the system text-scale factor, consistent with the rest of its DPI-only geometry. An
app run with a raised text scale will not track it. That is an accepted gap, not a
covered case.

**A `QueryInterface` per transition.** `SetRasterizationScale` queries
`ICoreWebView2Controller3` and releases it on each call rather than caching it.
DPI changes are rare (a monitor hop), so the cost is nil and the lifetime stays
trivial.

## What would change our mind

- **A bounds mode, or a runtime contract, where WebView2 and the host do not both
  react to a DPI change.** If the runtime could own the scale without also fighting
  the host over the bounds, `ShouldDetectMonitorScaleChanges(true)` would be
  simpler and this record would be superseded.
- **A report that the missing text-scale factor matters.** The moment a user runs
  a non-default system text scale and the content is visibly off, `DPI/96` becomes
  `DPI/96 × textScale`, the host starts tracking the text-scale-changed signal, and
  this record narrows.

## Evidence

- **Issue #1**: the symptom, the environment (WebView2 150.0.4078.65; a 125% and a
  100% monitor), and the log showing the window resizing across the hop while no
  scale update is emitted, because before this change no code emitted one.
- **The contract is Microsoft's, not inferred**:
  [ICoreWebView2Controller3](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2controller3)
  — the two quotes above, and the `WM_DPICHANGED` sample that sets the scale and
  applies the suggested rect in the same handler.
- **`host/monitor_windows_test.go`**:
  `TestRasterizationScaleForDPIMatchesMonitorScale` (96→1.0, 120→1.25, 144→1.5,
  192→2.0, 0→default) and `TestRasterizationScaleDependsOnlyOnCurrentDPI` (no
  compounding across a shuttle), run headless with no runtime.
- **Live confirmation is the acceptance step this record does not yet carry.** The
  headless tests prove the value; only a human dragging the window across a
  mixed-DPI boundary proves the wiring. That check is pending on issue #1 and is
  owed before the fix is called done, per `docs/verification.md`.
