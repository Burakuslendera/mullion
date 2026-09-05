# Snap Testing Boundary

**Status:** active

## 10. Testing boundary

The Settings9 IID/vtable layout, `BOOL` calling convention, startup ordering,
frame geometry, and routing decisions are deterministic headless contracts and
remain mandatory focused production-path tests. A live Snap observation never
replaces a headless test for ABI, policy, geometry, or routing.

The actual `HTMAXBUTTON` delivery through the WebView child, shell Snap
placement, caption drag/system-menu presentation, and compositor/DPI appearance
are live residuals only to the extent their truth is the real
window-manager/shell/compositor result. They require a live Windows session with
the target WebView2 Runtime. `Win+Z`, `Win`+Arrow, and edge-drag are valid
shell-Snap actions when named with their placement and restore result; none
proves the maximize-button hover flyout.

`SNAP-MAXIMIZE-HOVER` applies only when a deliberately selected supporting
native profile exposes a real maximize-button path. It then requires real mouse
hover with the cursor and flyout visible in the recording or frame. A keypress,
hit-test trace, readback, synthetic input, or another profile's diagnostic
result is not a substitute. The active client-extended
`caption_sysmenu_nccalc` profile has no such path; record the hover as outside
contract, not as a failed acceptance item.

Name the exact residual, artifact identity, OS/build/Runtime, profile, monitor
topology and scale/DPI, action/result, uncovered boundary, and uncertainty in
the report under the [evidence boundary](./verification/evidence.md). The
acceptance procedure remains in
[`verification/acceptance-checklist.md`](./verification/acceptance-checklist.md);
this file does not duplicate it. Historical Snap records retain their original
mechanism labels and nonclaims.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: distinguish deterministic Snap contracts, named shell actions, and the conditional maximize-hover live residual.
