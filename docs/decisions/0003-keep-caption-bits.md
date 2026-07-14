# 0003. The frameless frame keeps `WS_CAPTION` and `WS_SYSMENU`

**Status:** Accepted

## Context

The obvious way to remove a title bar is to remove the style bits that make one:
clear `WS_CAPTION`, clear `WS_SYSMENU`, and the frame is gone. It compiles, it
runs, and the window really does come up without a caption.

It is also wrong, and the symptoms do not point at the cause. Minimise, maximise
and restore snap between states with no shell animation, and nothing in the
rendering code explains it. The system menu stops existing: right-click and
`Alt`+`Space` do nothing, and so does a right-click on a WebView2
`app-region: drag` region, because `GetSystemMenu` has nothing to show. Snap
behaviour degrades.

The root cause is that DWM and the shell decide what a window *is* from its style
bits. A window without `WS_CAPTION` is not a captioned top-level window as far as
the shell is concerned, and it does not get the captioned-window contract.

## Decision

The window is created with
`WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX` and
keeps every one of those bits for its whole life. The frameless *appearance* is
produced elsewhere: `WM_NCCALCSIZE` returns a client rect that covers the
non-client area, so the shell still sees a well-formed captioned window and the
user sees only HTML.

## Alternatives rejected

**Clear the caption bits.** One line, no message handling, and the immediate
result looks correct - which is exactly why a reasonable engineer picks it and
why it must be written down. The price is the shell contract above: animations,
system menu and part of Snap. A style-profile experiment that stripped
`WS_SYSMENU`, `WS_MINIMIZEBOX` and `WS_MAXIMIZEBOX` is recorded in
[lessons-and-dead-ends.md](../lessons-and-dead-ends.md) section 7; the audit
confirmed the bits were exactly as intended, and the bug being chased was in the
DOM the whole time.

**Keep the bits but let `DefWindowProc` handle `WM_NCCALCSIZE`.** Tried as an A/B
variant, and the measurement *supported* it: the delegated build produced more
intermediate maximize frames than the control. Rejected anyway - handing back
`WM_NCCALCSIZE` hands back the native caption, so the app renders a native title
bar *and* the HTML one, with the client surface shifted. Two title bars is a
ship-blocker no metric can outvote (section 5).

## Consequences

Keeping the bits means owning everything they would have handled:

- `WM_NCCALCSIZE` must preserve the client extension for **every** `wParam`, not
  only `TRUE`; the `FALSE` case silently hands the frame back and a strip of
  native caption flickers in during transitions.
- When maximised, Windows pushes the (now invisible) resize frame outside the
  work area. The proposed rect must be intersected with the monitor work area, or
  the top of the title bar lands off-screen. Restored windows need a one-pixel
  bottom compensation instead.
- Any hit-test result delegated to `DefWindowProc` must be clamped to the title
  bar strip. A maximised window reports `HTCAPTION` far below the visible bar,
  and `HTCAPTION` is a drag handle.
- `DefWindowProc` does not update system-menu item states on `WM_INITMENU`, so a
  maximised window will happily offer "Maximize" unless the state is forced by
  hand.

None of that goes away. It is the standing cost of looking frameless while
remaining, to the shell, an ordinary window.

## What would change our mind

A Windows release in which DWM no longer keys the shell animations, the system
menu and the Snap contract off these bits - that is, a window with the bits
cleared animates, snaps and menus identically to one with them set.

That is measurable, not a matter of opinion, and the measurement does not exist
in this repository yet: **it would have to be built.** An A/B harness - a plain
Win32 window as the native control, `examples/basic` as the subject - driven over
maximize, restore, drag-down restore and snap, with the bits and without,
comparing unique shell frames; then the system menu states and the Snap flyout
checked by hand, because neither is visible to a frame counter.

Anyone claiming the bits are now dead weight owes that comparison. Until then
this record stands, and clearing the bits is a regression, not a cleanup.

## Evidence

- `docs/frame-and-dpi.md` sections 1 and 2: symptom, root cause and fix, plus the
  work-area clamp.
- `nccalc_windows.go` / `nccalc_windows_test.go`: the clamp when `IsZoomed` and
  the restored-frame compensation.
- `style_windows_test.go`: `TestFormatNativeWindowStyleLog` pins the native style
  audit line that the running window emits - `ws_caption=true`,
  `ws_sysmenu=true`, `ws_thickframe=true`, `ws_minimizebox=true`,
  `ws_maximizebox=true`.
- `docs/snap-and-nonclient-region.md` section 9: without `WS_SYSMENU`, the
  right-click menu on a drag region silently does nothing.
