# GUI verification traps

**Status:** active

Automating the manual list is possible but the environment fights back. These
are the failure modes that produced false passes and false failures, and the
rules that avoid them.

**Injected mouse input does not reach the WebView2 child.** Synthetic input
(`mouse_event`, `SendInput` at a `SetCursorPos` location) is delivered to the
native window tree, and the WebView2 child does not process it the way it
processes real hardware input. Consequence: **you cannot click an HTML button
from a script.** What you *can* drive is the native frame — the title bar drag
strip, the resize borders and corners, and the caption buttons — because those
live on the parent `HWND` and are resolved by our own window procedure. So:
script the native frame; do not script the DOM. To verify the frontend/host
bridge instead, have the frontend call one host binding on load and write the
result into the DOM, then assert on the screenshot or on the host-side log.

**Do not measure WebView2 client coverage with "the largest child HWND".**
Chromium creates an intermediate compositing window in addition to the
controller window. After a programmatic resize that intermediate window can
report a **stale rect larger than the client area** for a while — the parent
clips it, so there is no visual defect, but a script comparing "largest child"
against the client rect will report a bogus failure. Enumerate children and
measure **only the controller child** (`Chrome_WidgetWin*` class); ignore the
rest.

**Never run cursor/foreground smokes in parallel.** The cursor position and the
foreground window are *global* machine state. Two scripts that move the mouse or
raise a window at the same time corrupt each other and produce failures that do
not reproduce. Serialise every check that drags, hovers, snaps or focuses. Only
checks that are purely passive (log scraping, build gates) may run concurrently.

**Screenshot acceptance has a contract.** A screenshot is evidence only if all
four hold:
1. the capturing probe is **DPI-aware** (otherwise Windows hands it a scaled,
   blurry bitmap and every pixel assertion is meaningless);
2. the target window is found by its **real window class**, not by "the
   foreground window" or "the biggest window";
3. the capture waits for the **frontend-ready signal** — capturing during load
   photographs a white client area and proves nothing;
4. the crop includes an **outer margin** beyond the window rect, so shadow,
   rounded corners and any leaked native caption are inside the frame.

**Quit gracefully, then clean up.** Post the application's own quit message to
the window first and give it a moment to tear itself down; only then walk the
process tree and force-stop anything left. Force-stopping the process tree
directly kills the WebView2 browser process out from under the controller and
produces teardown error output — noise that looks exactly like a real bug.

**Check the return value of `PostMessageW`.** It fails silently. A script that
posts a quit or a click and never inspects the result will happily report a
clean lifecycle for a message that was never delivered.

**Do not trust the PID you launched.** An application may hand off to another
process (relaunch, elevation, single-instance handoff), so the PID your script
started can exit immediately while the real window belongs to a different
process. Find the window by **class name**, then derive the PID from the window
— not the other way around. When cleaning up, stop only the process tree you
own; if a window with your class is running that you did not start, abort the
run instead of killing someone else's process.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: relocate scripted GUI verification boundaries under verification routing.
