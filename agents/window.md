# Window Acceptance Rules

Hard rules for changing the frame, the hit-test, the non-client area or anything
that affects what the user actually sees. Read [AGENTS.md](../AGENTS.md) first.

These are the short forms. The reasoning — symptom, root cause, fix — is in
[docs/frame-and-dpi.md](../docs/frame-and-dpi.md) and
[docs/snap-and-nonclient-region.md](../docs/snap-and-nonclient-region.md); the
things already tried and abandoned are in
[docs/lessons-and-dead-ends.md](../docs/lessons-and-dead-ends.md). Every rule below
is here because breaking it produced a bug that took real time to find, and each
one is cheap to obey and expensive to relearn.

This section is protected core: it changes only with explicit human approval.

## Code rules

**`SWP_FRAMECHANGED` never travels without `SWP_NOMOVE | SWP_NOSIZE`.** The zeroed
position and size arguments are *applied*, not ignored, and the window collapses to
its minimum tracking size — a client rect around 46x39 px, with an outer window that
still looks correct. If a bounds log shows a tiny client rect, look here first,
before you look at WebView2.

**`WS_CAPTION` and `WS_SYSMENU` are never cleared.** DWM and the shell decide what a
window *is* from its style bits. Drop them and you lose shell animation, the system
menu, and the full Snap contract. The visual frame is removed in `WM_NCCALCSIZE`, by
extending the client area — never by removing the styles that make the window a
window.

**`WM_NCCALCSIZE` preserves the client extension for every `wParam`.** Windows sends
it with `wParam == TRUE` and with `wParam == FALSE`. Handle only the `TRUE` case and
the frame comes back in the other one, as a strip of native caption that flickers
into existence during transitions.

**Per-Monitor-V2 DPI awareness is set before any `HWND` exists** — before the window
class, before the main window, before any hidden or helper window a caller might
create, and long before the WebView2 child. Awareness is latched by the first window
in the process and a later call silently fails. The symptom is heavy, blurry text and
subtly oversized layout, and it will send you into the CSS, which is innocent.

**Resize-cursor zone synchronisation is debounced, and must not race the maximised
state.** During a maximise/restore transition the `resize` event can fire *before*
the native maximised state has settled, so an asynchronous "am I maximised?" query
can answer with the previous state and leave the zones wrong. Keep a synchronous
maximised flag as the source of truth for the pointer logic, and keep the guard that
uses it. This one looks like a race that "cannot happen" until it does.

## Acceptance rules

**Showing the parent window is not enough.** The WebView2 controller has its own
visibility flag; a visible parent hosting an invisible controller is a blank window
that reports success at every step. Show the controller explicitly. Bounds and
client-rect logs are *not* evidence that anything is on screen.

**A DWM corner readback is not visual proof.** Reading back the corner preference
tells you what you asked for, not what was drawn. Rounded corners, drop shadow,
caption buttons and shell animation are accepted from a screenshot or a recording,
by a human, and by nothing else.

**`Win+Z` is not Snap evidence.** A keyboard shortcut, or a screenshot of a hover
that never happened, does not demonstrate that the Snap Layouts flyout opens from the
maximise button. Acceptance requires the flyout actually opening under a real mouse
hover. If a report claims hover, it must say that it was a mouse hover — and the
cursor must be visible in the frame or recording.

**The maximise glyph must be correct in the first painted frame.** Not correct after
a repaint, not correct once the DOM settles. Frame counts, an `IsZoomed` value, a DOM
state or a bounds log are not acceptance evidence for a visual property — they are
evidence that the code ran.

**Maximised, the custom title bar must not be clipped.** Extending the outer `HWND`
is not sufficient: the client area must begin inside the monitor work area, and the
title bar must survive at the top. Verify the client origin; do not infer it.

**Visual smoothness across maximise, restore, drag-down restore and Snap is a hard
gate.** Flicker, tearing, a doubled title bar or a surface that shifts is a rejection
even when every number in the log looks better than before. There is no measurement
that outranks the window looking wrong.

## Verification and reporting

Every change to frame, hit-test, DPI, snap or paint behaviour is verified with the
live demo checklist in [docs/verification.md](../docs/verification.md), on a real
machine, by a human looking at a real window. A passing test suite does not
substitute; the tests are headless by design and cannot see any of this.

Report it in four parts, and never merge them:

1. what the **tests** cover (and which of them you added or changed);
2. what was **verified live** — which checklist items, on what display setup,
   including whether a scaled or multi-monitor configuration was involved;
3. what was **not covered**, explicitly;
4. what remains **uncertain**, with a label from [policy.md](./policy.md).

Two specific traps worth naming in any report:

- Multi-monitor and mixed-DPI behaviour is not covered by a single-monitor check.
  If you only had one display, say so — do not let silence imply coverage.
- A verification *script* can be the thing that is broken. Before trusting a failing
  automated visual check, confirm the harness itself is measuring what it claims to;
  a harness that reports a false pass is worse than no harness, because it is
  believed.

If a change makes one of the rules above obsolete, that is a Tier 3 rule change: it
needs explicit human approval and the evidence that retired the rule. Propose it;
do not quietly edit it out.
