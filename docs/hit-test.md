# WM_NCHITTEST: native frame hit-testing

## Contents

- [4. `WM_NCHITTEST`](#4-wm_nchittest)
- [2. Issue #113 hot-path invariants](#2-issue-113-hot-path-invariants)
- [3. Headless test contract](#3-headless-test-contract)
- [References](#references)

This is the canonical reference for `WM_NCHITTEST` geometry, diagnostics and
caption-candidate evaluation. The frame overview in
[`frame-and-dpi.md`](./frame-and-dpi.md) links here rather than duplicating this
section. Read this before changing `host/hittest_windows.go`,
`host/windowproc_windows.go` or the caption-policy readers.

## 4. WM_NCHITTEST

The hit-test is the entire interaction contract of a frameless window. It runs
on the **native** side against the window rect in physical pixels; CSS may paint
matching affordances, but it never owns this geometry.

**Scale exactly, then bound to the rect.** For every positive `int32` Config
metric `m`, keep the scaled value in `int64`; there is no additional metric cap:

```
effectiveDPI = dpi, except dpi == 0 means 96
scaled(m, dpi) = ceil(int64(m) * int64(effectiveDPI) / 96)
               = (int64(m) * int64(effectiveDPI) + 95) / 96
```

The ceiling is intentional. Do not narrow after the multiply: a legal metric can
exceed `int32` at high DPI without exceeding the geometry that ultimately bounds
it.

**Construct one bounded geometry.** A rect is valid only when `left < right` and
`top < bottom`, and a cursor is eligible only inside the half-open rect
`[left,right) x [top,bottom)`. An invalid rect or outside cursor is `HTCLIENT`.
For width `w = right-left` and height `h = bottom-top`, all in `int64`, use:

```
title    = min(scaled(titleMetric, dpi), h)
controls = min(scaled(controlsMetric, dpi), w)
resizeX  = min(scaled(resizeMetric, dpi), floor(w/2))
resizeY  = min(scaled(resizeMetric, dpi), floor(h/2))
```

Rect extents, cursor coordinates and every interval endpoint remain `int64`;
only the final `HT*` result is `int32`. Independent horizontal and vertical
half-extent saturation keeps opposite resize edges disjoint even for enormous
metrics or a narrow rect.

**Classify from that geometry.** When restored, test corners before edges, then
the rightmost `controls` pixels of the bounded title strip, then the remaining
title strip, then client. Corners therefore retain priority; caption controls
remain `HTCLIENT`, while only the drag portion is `HTCAPTION`. When maximized,
skip resize classification entirely, but preserve the same controls/title/client
classification. A maximized content point must never be delegated to the native
caption geometry.

Diagnostics have two distinct formats. The titlebar-drag line
(`source=titlebar_drag`) uses the shared constructor: invalid geometry emits
`geometry_valid=false` (with `maximized` and `hit`); valid geometry emits
`geometry_valid=true`, `cursor_y`, `window_top`, effective `side_border`,
`top_border`, `titlebar_height`, `controls_width`, `maximized`, and `hit`.
The per-message native `WM_NCHITTEST` formatter is separate: when
`MULLION_HITTEST_DIAG=1` (or its accepted boolean spellings) is enabled, it
emits its own `zoomed`, `cursor`, `rect`, `dpi` and `hit` fields. Do not expect
`geometry_valid` or effective bands on that per-message line, or the
`zoomed`/cursor/rect/DPI fields on every titlebar-drag diagnostic row. The
switch is latched when the package initializes so disabled input does not
format diagnostic text. `MULLION_TOOLTIP_TRACE=1` is latched the same way, and
its caption candidate geometry is computed only when the active caption policy
or tooltip trace can consume it.

The injected resize overlay mirrors the independent half-extent bounds in CSS
coordinates; its eight zones opt out of ancestor `app-region: drag`, and valid
event coordinates outrank stale DOM targets. CSS hit-test overrides still use the
rules above.

## 2. Issue #113 hot-path invariants

Issue #113 measured the pre-fix `WM_NCHITTEST` path at 552 ns/op, 336 B/op and
8 allocs/op on amd64 with `hwnd=0`; a live window is strictly more expensive.
The fix preserves hit-test precedence while removing work that no reader needs.

- `MULLION_HITTEST_DIAG` and `MULLION_TOOLTIP_TRACE` are sampled once into
  package-init latches. Production code never calls `os.Getenv` per message.
  Tests substitute the latches only sequentially and must not use `t.Parallel`.
- `fmt.Sprintf` stays inside the hit-test diagnostic gate. `Logger.Debug`
  accepts a preformatted string, so even `NopLogger` pays eager formatting if a
  caller formats before checking the latch. The disabled pure formatter contract
  is zero allocations; it is not a live Win32 measurement.
- The caption candidate query is auxiliary: it repeats `GetWindowRect`,
  `IsZoomed`, `GetDpiForWindow` and, when maximized, monitor/rect work already
  performed by the project hit-test. It is evaluated only for a real reader:
  tooltip tracing, or `MaximizeOnly` plus the caption-passthrough diagnostic.
  `AllButtons` and ordinary `MaximizeOnly` routing consume the DWM result and do
  not read candidate geometry.
- The production composition uses the single
  `nativeCaptionButtonHitNeeded` predicate. If no reader exists, `HTCLIENT` is
  the safe sentinel because neither policy decision nor trace formatting may
  inspect it. Every future candidate reader must extend that predicate and its
  composed-path tests; do not add an independent gate at a call site.
- The accepted cost remains the native `LazyProc.Call`/Win32 query cost when a
  reader is enabled. The optimization is to avoid unread work, not to change
  pointer lifetime or syscall calling conventions.

Rejected alternatives are per-message `os.Getenv`, eager `fmt.Sprintf`, and an
unconditional native candidate query. The durable rationale and trip-wires are
recorded in [decision 0041](./decisions/0041-wm-nchittest-reader-gates.md), while
[decision 0019](./decisions/0019-maximized-hittest-stays-in-process.md) remains
the authority that hit-testing never routes through shell IPC.

## 3. Headless test contract

No test creates an `HWND`, enters a message pump, or calls a Win32 entry point.
Pure geometry tests lock the half-open bounds, saturation, precedence and
maximized behavior. The issue-#113 production-composition tests additionally
call `nativeCaptionButtonHitIfNeeded` with a counting query: the unread matrix
must make zero candidate calls, and each real reader must make one. The disabled
diagnostic formatter must return an empty string with zero allocations, while the
enabled contract checks its fields. These tests prove lazy composition and
formatting gates without faking live Win32 timings.

The headless boundary does not prove the cost or behavior of a live `HWND`, DWM,
monitor topology, WebView child hit routing, tooltip visuals, or shell cursor
messages. Those remain live verification obligations in
[`verification.md`](./verification.md) and its
[continuation records](./verification-records.md).

## References

- [`host/hittest_windows.go`](../host/hittest_windows.go)
- [`host/windowproc_windows.go`](../host/windowproc_windows.go)
- [`host/dwm_caption_windows.go`](../host/dwm_caption_windows.go)
- [`docs/decisions/0041-wm-nchittest-reader-gates.md`](./decisions/0041-wm-nchittest-reader-gates.md)
- [`docs/decisions/0019-maximized-hittest-stays-in-process.md`](./decisions/0019-maximized-hittest-stays-in-process.md)

> Last updated: 2026-08-15 | Editor: OpenAI (GPT-5.6) | Change: make issue #113 hit-test reader, allocation and headless-test contracts canonical.
