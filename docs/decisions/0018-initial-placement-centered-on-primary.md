# 0018. The first window is centered on the primary monitor's work area, DPI-scaled

**Status:** Accepted

## Context

`CreateWindowEx` was called with `CW_USEDEFAULT` for position and the raw
`Config.Width`/`Height` for size. Measured live (issue #59, three launches of
`examples/basic` on a 1920x1080 primary at 125%): the window landed at
(96,96), (224,224) and (128,128) — the shell's cascade, drifting per launch,
never centered — and its physical size was exactly the configured 980x640,
although `Config` documents Width and Height as **logical** pixels, which at
125% promises 1225x800 physical. `WM_DPICHANGED` never corrects the size,
because it only fires when the DPI *changes*, and the window is born on the
monitor it is born on. So the size contract was broken on every monitor above
100%, and the position was whatever the cascade counter happened to hold.

## Decision

The host computes the creation rect itself (`host/placement_windows.go`).
Before the `HWND` exists, it resolves the **primary monitor**, reads its
effective DPI (`GetDpiForMonitor` — the process is already Per-Monitor-V2
aware, decision at architecture step 1), scales `Config.Width`/`Height` from
logical to physical, and centers the result in the monitor's **work area**.
The math lives in a pure function (`centeredPlacement`) so the contract is
headlessly testable (decision 0006). If the monitor cannot be resolved, the
call falls back to the old `CW_USEDEFAULT` behaviour and logs a warning: a
degraded position, never a failed launch.

## Alternatives rejected

- **The monitor under the cursor.** Arguably better multi-monitor UX — the
  window appears where the user is looking — but nondeterministic: the same
  program launched twice lands in two places, and a test or a screenshot
  pipeline cannot predict where. The reporter's expectation in issue #59 was
  the primary's center. Cursor-relative placement remains available to a
  future `Config` option if someone asks for it.
- **Keep `CW_USEDEFAULT` and reposition after creation.** A `SetWindowPos`
  between creation and show would also center, but it is a two-step dance that
  briefly exists at the wrong place and size, repositions relative to a rect
  the cascade chose, and runs the `WM_DPICHANGED` machinery if the cascade
  position and the final position straddle monitors. Computing the rect first
  has no intermediate state.
- **Scale but do not center (fix only the size half).** The size half is the
  contract breach, but leaving the cascade position keeps the user-visible
  complaint of issue #59 and would revisit this file within the month.
- **A `Config.Placement` knob now.** No caller has asked for one. Adding API
  on speculation contradicts the library's compatibility posture — every knob
  is a promise. The decision here fixes the default; a knob can be added
  compatibly later.

## Consequences

- Programs get a window that is *larger* in physical pixels than before on
  every monitor above 100% — that is the documented contract finally being
  honoured, but a caller that had hand-tuned logical values to compensate for
  the old unscaled behaviour will see a bigger window.
- The placement is deterministic: identical launches produce identical rects.
  Anything that relied on the cascade offsetting successive windows (two
  hosts in two processes) now gets them exactly stacked.
- `Config.Width`/`Height` larger than the work area are clamped to it: the
  window opens flush with the work-area origin instead of hanging off-screen.
- The primary monitor is a fixed choice: launching from a secondary monitor
  still opens the window on the primary. That is the deliberate,
  deterministic default; changing it is an API-visible behaviour change and
  needs its own record.

## What would change our mind

- A real caller needing cursor-relative or explicit placement — that adds a
  `Config` option (compatibly), and this default stays.
- Windows changing `GetDpiForMonitor` semantics for PMv2 processes, or a
  monitor topology where `MONITOR_DEFAULTTOPRIMARY` at (0,0) does not resolve
  the primary — either would surface as the fallback warning
  (`mullion: initial placement unresolved`) in startup logs.
- Evidence that centering over the work area misplaces the window under an
  auto-hide taskbar setup (work area equals the monitor rect there); the 1px
  sliver machinery of decision 0015 is maximize-only and deliberately not
  consulted at creation.

## Evidence

- Issue #59: the live three-launch measurement of the defect, with the
  environment (`mullion doctor`) and the mechanism walk.
- Before the fix: (96,96)/(224,224)/(128,128), 980x640 physical at dpi 120.
  After: (347,110), 1225x800 physical, identical across launches; startup log
  `mullion: initial placement, x=347, y=110, width=1225, height=800, dpi=120`.
  Client rect 1225x801 — the +1 restored-frame compensation, unchanged.
- Headless: the `TestCenteredPlacement*` suite in
  `host/placement_windows_test.go` pins scaling, centering, negative-origin
  work areas, the zero-DPI fallback, oversize clamping and the log format.
