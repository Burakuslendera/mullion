# Lessons and Dead Ends

An archive of **things that did not work** while building a frameless Win32 + WebView2 window host.

Hosting a browser surface in a custom-framed window that still behaves like a real Windows app — draggable title bar, resize borders, Snap, DPI changes, maximize/restore — looks like a weekend of Win32 plumbing. It is not. Most of the time goes into discovering that an obvious fix does nothing, or fixes a symptom in the wrong layer.

Everything below was built, run, and failed. Each entry: **what was tried → why it looked reasonable → why it failed → what was done instead.**

Working rule throughout: a claim is only "verified" if it was observed at runtime on a real window. Passing tests, clean logs and plausible static analysis have each been wrong here.

## Contents

- [1. The bug that forced us to own the window: maximized title bar drag-down restore](#1-the-bug-that-forced-us-to-own-the-window-maximized-title-bar-drag-down-restore)
- [2. `SWP_FRAMECHANGED` without `SWP_NOMOVE | SWP_NOSIZE` collapses the window](#2-swp_framechanged-without-swp_nomove--swp_nosize-collapses-the-window)
- [3. Showing the parent HWND does not show the WebView](#3-showing-the-parent-hwnd-does-not-show-the-webview)
- [4. WebView2 does not render while hidden](#4-webview2-does-not-render-while-hidden--hide-until-ready-then-show-is-impossible)
- [5. Letting `DefWindowProc` handle `WM_NCCALCSIZE`](#5-letting-defwindowproc-handle-wm_nccalcsize--better-numbers-broken-product)
- [6. The delegated `HTCAPTION` trap](#6-the-delegated-htcaption-trap-the-whole-native-caption-is-a-drag-region)
- [7. Weeks spent in the wrong layer](#7-weeks-spent-in-the-wrong-layer--the-native-tooltip-that-was-a-dom-element)
- [8. Serving frontend assets over `localhost` with a random port](#8-serving-frontend-assets-over-localhost-with-a-random-port)
- [9. Measuring shell motion honestly](#9-measuring-shell-motion-honestly-ab-harness-methodology)
- [10. `MinTrackSize` is not what you think it is](#10-mintracksize-is-not-what-you-think-it-is)
- [11. Injected mouse input never reaches the WebView2 child](#11-injected-mouse-input-never-reaches-the-webview2-child)
- [12. Performance notes: where the WebView2 levers actually are](#12-performance-notes-where-the-webview2-levers-actually-are)
- [13. Building on a third-party WebView2 binding](#13-building-on-a-third-party-webview2-binding--the-slow-squeeze)
- [14. A data: document has no reportable source](#14-a-data-document-has-no-reportable-source)
- [15. The short version](#15-the-short-version)

---

## 1. The bug that forced us to own the window: maximized title bar drag-down restore

**Tried.** The host began on top of an existing Go desktop GUI framework, with a custom HTML title bar drawn over the framework's window. The bug: maximize, grab the custom title bar, drag down. The OS restore-and-move gesture fires, the frame restores correctly — and the WebView2 content does not follow. White gap top-left, content shifted bottom-right, still broken after mouse-up.

Each of these was implemented and measured. None fixed it:

- **Native `WM_NCHITTEST` → `HTCAPTION` title bar adapter** (let Win32 own the drag instead of CSS). It *regressed* mouse-move and title bar double-click, and never touched the offset.
- **`RedrawWindow` on parent + children at `WM_EXITSIZEMOVE`** (it looks like a stale paint, so invalidate everything). The redraw logged every time; nothing changed. A repaint cannot fix a surface whose *bounds* are wrong.
- **Child HWND bounds resync at `WM_EXITSIZEMOVE`.** No effect — and the expected log line sometimes never appeared, which made the child-discovery code itself suspect.
- **A runtime 1px size nudge** (if a real size *change* drives the resize chain, fake one). Logged, no permanent drift, no flicker, no fix.
- **Exact-size sync** (re-set the window to its *current* size at DOM-ready and at the end of every move session, forcing the chain without changing anything). The path ran; the bug survived.
- **Forking the GUI framework** to call `chromium.Resize()` from its own `WM_EXITSIZEMOVE` handler, on the UI thread, undebounced — i.e. the framework's internal `GetClientRect → PutBounds` chain, invoked directly. Logs looked perfect. Still broken.

**Why it failed — all of it.** Every attempt was a workaround applied from *outside* the ownership boundary. The window, the message loop, the controller lifetime and the frame math belonged to someone else; we were poking at the result. Six increasingly invasive pokes produced six clean logs and zero pixels moved.

**Instead.** The project took ownership of the window: top-level HWND, window class, message loop, non-client handling, DPI, resize, Snap and the WebView2 controller are all created and driven by our own code. The misalignment disappeared as a *consequence* — the frame rect and the controller bounds finally came from the same owner, in the same order.

**Lesson.** When four or more independent fixes fail while producing correct-looking logs, stop fixing. The hypothesis is wrong at the ownership level, not the message level.

---

## 2. `SWP_FRAMECHANGED` without `SWP_NOMOVE | SWP_NOSIZE` collapses the window

**Tried.** The standard incantation for recomputing the non-client area after a style change: `SetWindowPos(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED | ...)`.

**Looked reasonable because** every sample does exactly this, and the zeros look like placeholders.

**Failed because** they are not placeholders. Without `SWP_NOMOVE | SWP_NOSIZE` they are a real move to (0,0) and a real resize to 0x0, clamped up to the minimum. The app booted, assets loaded, logs were clean — and the UI appeared as a tiny stamp in the middle of the window. The symptom presents as a *rendering* bug ("tiny render"), so the hunt starts in the wrong place: assets, DPI scale, browser init order. Days. What ended it was instrumentation, not insight: logging the parent client rect and the controller bounds side by side at every sync point. The client rect read `46 x 39`. The window had been resized to nothing, and the browser was faithfully rendering into it.

**Instead.** For a pure frame refresh, always `SWP_FRAMECHANGED | SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE`.

**Lesson.** When the content looks wrong, log the *container* first. A surface correctly filling a broken rect is indistinguishable from a broken surface.

---

## 3. Showing the parent HWND does not show the WebView

**Tried.** Create the window, embed the browser, `ShowWindow(parent)`. Done.

**Looked reasonable because** child windows are visible when their parent is. That is how Win32 works.

**Failed because** the WebView2 *controller* carries its own visibility flag, and it defaults to hidden on this embedding path. The audit was unambiguous: the browser child HWNDs existed, their bounds exactly matched the parent client area, and every one reported `Visible = false`. Bounds right, content loaded, screen blank. Nothing in the bounds logs hints at this, which is exactly why it cost time.

**Instead.** The initial show path explicitly makes the controller visible, *in addition to* showing the parent.

**Lesson.** In an embedded-browser host, "visible" is at least two independent booleans. Assert both.

---

## 4. WebView2 does not render while hidden — "hide until ready, then show" is impossible

**Tried.** Keep the window hidden until the frontend signals ready, then show it fully rendered: no flash of an empty frame, no title bar during loading. The initial show was deferred from "shell ready" to "frontend ready".

**Looked reasonable because** it is a standard desktop pattern, and exactly what Electron does (`show: false`, then show on `ready-to-show`).

**Failed because** Electron renders offscreen and **WebView2 does not**. Hidden window → Chromium throttles → nothing renders → the JS-driven ready signal never fires. Chicken-and-egg: the window waits for `frontend_ready`; `frontend_ready` waits for a render; the render will not happen while hidden. The safety-net timer fired first, revealed the window mid-load with the loading screen visible, and the ready signal arrived seconds *after* that — strictly worse than doing nothing. Confirmed from timestamps, not theory.

**Instead — two parts.** (1) **DWM cloaking (`DWMWA_CLOAK`) instead of hiding:** a cloaked window keeps `WS_VISIBLE`, so Chromium keeps rendering and the ready signal fires normally, but DWM does not composite it to the screen. Uncloak on ready and the window appears fully painted. (2) **A separate frameless loading window:** the main window is invisible while loading, so the loading UI needs its own home — a small `WS_POPUP` window (no caption, `WS_EX_TOOLWINDOW` so it gets no taskbar button), painted natively, created before the message loop starts and destroyed on reveal.

**Follow-on trap in the reveal.** Uncloaking is not enough. The window was shown (and given foreground) *while cloaked*, so it lost foreground to whatever the user clicked next; on uncloak it stayed behind other windows and had to be picked from the taskbar. `SetForegroundWindow` alone does not reliably fix this — the foreground-steal restriction can refuse it. What works is the z-order dance, since a window may always change *its own* z-order: `SetWindowPos(hwnd, HWND_TOPMOST, ...)` then `SetWindowPos(hwnd, HWND_NOTOPMOST, ...)` (both with `SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE`) pulls it visually to the top; `SetForegroundWindow` afterwards is a best-effort attempt at keyboard focus.

**Lesson.** Do not port a lifecycle pattern from Electron to WebView2 without checking whether it depends on offscreen rendering. Several do.

---

## 5. Letting `DefWindowProc` handle `WM_NCCALCSIZE` — better numbers, broken product

**Tried.** The custom frame returns `0` from `WM_NCCALCSIZE` unconditionally, so the client area covers the whole window and the HTML title bar can live in it. Hypothesis: that is also why maximize/restore *animation* looks less fluid than a standard Win32 window. So: an A/B variant behind a build tag — same style bits, `WM_NCCALCSIZE` delegated to `DefWindowProc`.

**Looked reasonable because** it was a testable hypothesis, a one-line change, gated so production could not regress.

**Failed because** — and this is the point — **the measurement supported the hypothesis.** The delegated variant produced meaningfully more intermediate maximize frames than the control. It was rejected anyway: handing `WM_NCCALCSIZE` back to `DefWindowProc` brings the native caption back, so the app rendered a native title bar *and* the custom HTML title bar, with the client surface shifted. Two title bars. Ship-blocking.

**Instead.** The control variant stayed the default; the diagnostic stayed inactive behind its tag. Motion was addressed separately with a *bounded* `WM_NCCALCSIZE` adjustment rather than by giving the frame back.

**Lesson — the important one.** *A metric moving in the right direction is not acceptance.* "More frames" was real, reproducible and completely irrelevant once the visual gate failed. Define the hard gates (no double title bar, no surface shift, Snap works, restore-drag aligned) **before** running the experiment, and let them veto the metric.

**Sibling defect.** Returning `0` for *all* `WM_NCCALCSIZE` cases has a second cost: when Windows maximizes a window it extends the outer rect past the work area by the width of the (now invisible) resize frame. If the whole outer rect becomes client area, the web content inherits that overhang and the top of the custom title bar lands off-screen. Fix: keep the outer-rect overhang (correct Windows behaviour — do not fight it), but when `IsZoomed(hwnd)` is true, intersect the first `WM_NCCALCSIZE` rect with the monitor work area. Restore and Snap paths untouched.

---

## 6. The delegated `HTCAPTION` trap: the whole native caption is a drag region

**Tried.** In an experiment that deliberately kept a *real* native caption (the only reliable way we found to get the Windows 11 Snap Layout flyout on hover — see the note below), `WM_NCHITTEST` was delegated to `DefWindowProc`.

**Looked reasonable because** `DefWindowProc` knows where the caption buttons are; delegating gets minimize/maximize/close hit-testing and Snap behaviour for free.

**Failed because** when maximized, the native caption is *taller* than a title bar looks — noticeably taller than the ~32 logical px strip you expect — and `DefWindowProc` reports `HTCAPTION` for **all** of it. The caption had been themed to the app background with no icon and no title text, so its lower band looked exactly like page content. Users grabbed what they believed was content, and the window moved or restored.

**Instead.** Clamp the delegated result: keep `HTCAPTION` only within the top *N* logical pixels (DPI-scaled) below the window top; below that, return `HTCLIENT`. `HTMAXBUTTON` / `HTMINBUTTON` / `HTCLOSE` and the resize borders pass through untouched, so Snap keeps working.

**Warning about this very fix.** In one configuration the clamp was a **runtime no-op** — the trace showed the parent's `WM_NCHITTEST` returning only resize-border codes, never `HTCAPTION`; the clamp never fired once. The drag came from somewhere else entirely: an injected `position: fixed` resize-edge overlay *inside the web content*, sitting directly below the caption, catching the pointer and starting a top-edge resize that looked like a drag. The clamp was correct, harmless and irrelevant. Only the trace log proved it.

> Background: with a fully extended client area, a custom `WM_NCHITTEST` returning `HTMAXBUTTON` does **not** produce the Snap flyout; it appears to require a real DWM-managed non-client caption. Chromium-based shells get around this because the renderer lives in the window hierarchy the shell owns, so its hit-test can cooperate with `DwmDefWindowProc`. WebView2 lives in a *separate* child HWND that masks the top-level hit-test on hover. A fully custom HTML title bar **plus** native Snap may simply not be reachable in a WebView2 host.

---

## 7. Weeks spent in the wrong layer — the "native" tooltip that was a DOM element

The most expensive lesson here, and the least technical.

**Symptom.** A black `Restore` tooltip balloon over the window. It looked exactly like a native non-client caption tooltip.

**Tried,** in order, each defensible:

- Removed the `title` attribute from the maximize/restore button (kept `aria-label`), theorising a stale browser tooltip surviving a state change. No effect.
- Widened the resize-cursor boundary smoke to 8 logical px. Proved a boundary; explained nothing.
- Refactored the frontend window-control module — purely structural, could not change behaviour.
- Switched the native frame profile: disabled `WS_SYSMENU`, `WS_MINIMIZEBOX` and `WS_MAXIMIZEBOX`, kept `WS_CAPTION` and `WS_THICKFRAME`. The style audit confirmed the bits were exactly as intended. The balloon was still there.
- Built a dedicated GUI smoke: popup-window detector, env-gated native tooltip trace, build tag to remove the caption entirely. The first unfiltered run reported `fail` — but it was counting the app's own window and a pseudo-console window as "popups", i.e. a detector bug. Filtered runs came back `inconclusive`. Days of instrumentation, zero evidence.

**Actual root cause.** An extra restore element had been accidentally added to `index.html`. Deleting that one DOM node fixed it completely.

**Why it took so long.** The symptom *looked* native — black system-style balloon, drawn over the frame, correlated with maximize state — so every hypothesis was a native hypothesis: DWM, `WS_CAPTION`, `WM_NCCALCSIZE`, caption tooltips. The web layer was never seriously suspected because it "obviously" was not a web problem. It obviously was.

**The rule now, and it is a rule:** on any title bar/caption visual regression, **search the title bar DOM first.** Open a `WS_CAPTION` / `WM_NCCALCSIZE` / DWM spike **only after** the DOM is proven clean.

Corollary: none of the automated harnesses — Go tests, frontend tests, build matrix, cursor smoke, resize smoke — could see this bug. They all passed, the whole time. When the suite is green and the user is still looking at a broken window, the suite is measuring the wrong thing.

---

## 8. Serving frontend assets over `localhost` with a random port

**Tried.** The first asset pipeline was an HTTP server bound to `127.0.0.1` on an OS-assigned random port, with the WebView navigating to it.

**Looked reasonable because** it is the path of least resistance: every web toolchain speaks HTTP, so relative URLs, `fetch` and source maps just work — and plenty of embedded-browser samples do it.

**Failed because** it is a listening socket on the user's machine, for a desktop app that has no reason to have one: an attack surface reachable by any other local process, a firewall prompt on first run, port-collision failure modes, and a security question you get to keep answering forever. Pure cost, no benefit.

**Instead.** No port. Assets are served from an embedded filesystem through WebView2's `WebResourceRequested` interception: navigation targets a virtual host name, the request is answered in-process, nothing binds to the network stack. Enforced statically — a grep for `net.Listen`, `http.Server`, `127.0.0.1:0` and `localhost` in the host package is part of the verification checklist and must return nothing.

**Implementation trap worth knowing.** The convenience helper for building a `CreateWebResourceResponse` had stream-lifetime behaviour that did not survive the async request path: the *first* request (`index.html`) was served, its body arrived empty, and the DOM came back with `scripts=0, stylesheets=0`. That presents as "the frontend never boots" and sends you hunting through the JS module graph, where nothing is wrong. `CreateWebResourceResponse` takes **no** reference on a stream handed to it, so releasing the stream on the way out frees the body before anything reads it. Fix: own the stream — create it explicitly, attach it with `PutContent` (which *does* take a reference), and release your own references only once `PutResponse` has run and the runtime holds everything it needs. **If the first asset request is answered but its body is empty, suspect response stream lifetime, not the frontend.**

The follow-on lesson took longer. The first fix was to retain every response and stream until the message loop exited — safe, obviously correct, and a monotonic memory leak: memory grew with every asset request served. It stood as a "known limitation" for a while, on the reasoning that releasing early needed a completion signal the binding did not expose. It did not need a completion signal; it needed the refcount rules, which only became visible once we owned the COM lifetime ourselves (§13). **A "known limitation" that exists because a dependency will not tell you something is worth re-examining the day you stop depending on it.**

---

## 9. Measuring shell motion honestly (A/B harness methodology)

Not a dead end — a method, recorded because several misleading measurements preceded it. "Does our window feel as smooth as a real Windows window?" is answerable neither by logs nor by screenshots.

**Harness.** Three targets, driven through the *same* gestures with the *same* timing on the *same* machine, in one run: (1) a plain Win32 window created and owned by the harness itself — the "what does the OS do natively" control; (2) a reference host on the mainstream GUI framework — the "what does a normal framework do" baseline; (3) the real application window. Gestures: maximize, restore, drag-down restore, Snap. For each, count unique shell frames and compare aggregates.

**Counted messages:** `WM_ENTERSIZEMOVE`, `WM_EXITSIZEMOVE`, `WM_WINDOWPOSCHANGING`, `WM_WINDOWPOSCHANGED`, `WM_SIZE`, `WM_MOVE`, `WM_MOVING`, `WM_DPICHANGED`. Only the event name, a sanitized source label, a counter and an elapsed value are recorded; where a rect is needed, width/height only.

**Traps found while building it:**

- **Picking the wrong window.** The harness originally chose "the largest visible window in the process tree" and happily grabbed an unrelated foreground window, producing complete, plausible, meaningless numbers. Fix: prefer the top-level HWND of the started process, plus a **foreground guard** — if the intended target is not foreground when the gesture is about to be sent, the smoke *fails* rather than measuring something else.
- **Silent post failures.** `PostMessageW`'s return value was not checked, so a message that never arrived counted as a successful step.
- **Screenshots are opt-in**; default output carries aggregate counts only.
- **Serialize GUI smokes that touch global state.** Cursor- and foreground-dependent smokes were once run in parallel and produced spurious failures by fighting over the global cursor.

---

## 10. `MinTrackSize` is not what you think it is

**Tried.** An automated smoke verified the window's minimum size by calling `SetWindowPos` with a tiny size and asserting the window clamped to the minimum.

**Looked reasonable because** `WM_GETMINMAXINFO` sets `ptMinTrackSize`, so the window should refuse to go below it.

**Failed because** `MinTrackSize` applies only to **user tracking** — an actual mouse-driven resize. A programmatic `SetWindowPos` **bypasses it entirely**. The window shrank happily below the minimum, and the test reported a real-looking failure for a bug that did not exist.

**Second defect in the same field.** The minimum size was written as a *logical* value into a field Windows reads as *physical* pixels, so at 125% scaling the effective minimum was 20% too small. Fix: scale the min-track size by the window's real DPI, and apply it *after* the previous window procedure has run — otherwise it is overwritten.

**Instead.** Minimum size comes from a single DPI-scaled source at `WM_GETMINMAXINFO` time, and the automated "minimum size" assertion was deleted, because it cannot be written honestly (§11).

---

## 11. Injected mouse input never reaches the WebView2 child

**Tried.** GUI smokes that drive the app as a user would: `SetCursorPos` + `mouse_event` to click buttons, drag the title bar, exercise the web UI.

**Looked reasonable because** synthetic input is the standard way to automate a Windows GUI, and it works on ordinary Win32 controls.

**Failed because** input injected this way **does not reach the WebView2 child window**. Not "is flaky" — does not arrive. In the failing runs the session log did not even contain the first entry of the drag handler. The smoke was not failing; it was *inconclusive*, which is worse, because an inconclusive smoke read as a pass is a lie.

**What does work.** Native frame regions — resize borders, caption strip, system menu — live on the **parent** HWND, and injected input reaches those normally. Anything handled in `WM_NCHITTEST` / `WM_NCLBUTTONDOWN` on the parent is automatable.

**Instead.** Split the test strategy explicitly. *Automatable:* window lifecycle (show/hide/minimize/maximize/restore/close), message-level behaviour, native frame hit-testing, bounds/visibility/style audits of the parent and its browser children, DPI-aware screenshots, motion counters. *Not automatable, and labelled as such:* anything requiring interaction with content **inside** the WebView — evidence for that comes from a real human clicking, or it does not exist.

**Lesson.** Write down which behaviours your harness *cannot* observe. A named untestable area is a known risk; an unnamed one is a false green.

---

## 12. Performance notes: where the WebView2 levers actually are

Read the binding's source before optimizing — whether it is a third-party one or your own. Several things you might be tempted to "fix" in a WebView2 host are already done, and are done the same way in every binding worth using:

- The controller is created in **raw-pixel bounds mode** (`COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS`) with **monitor-scale auto-detection disabled** (`PutShouldDetectMonitorScaleChanges(false)`). That is the correct configuration for a host that owns its own DPI handling, and already the resize-performance-friendly setting. Nothing to gain.
- Autofill and password autosave already default to off — no overhead to reclaim.
- The controller background colour is already set.
- Swipe navigation can be disabled (`PutIsSwipeNavigationEnabled(false)`), but the effect is minor. Treat it as correctness — a desktop app should not back-navigate on a trackpad gesture — not as a performance win.

**The real lever is `AdditionalBrowserArguments`** — Chromium command-line flags, carried on the environment options and therefore fixed *before* the environment is created, let alone the controller embedded. That is where feature disabling and memory reduction actually happen.

**Treat it as an experiment, not a config tweak.** Browser args change production behaviour and invalidate any memory baseline you hold. The discipline that worked: (1) put the args behind a diagnostic gate or build tag; (2) A/B them against the existing baseline with a memory harness — measuring working set *and* private bytes, for the host **and** the distribution across WebView2's child processes, because measuring only the host process will mislead you; (3) only then decide.

Also cheap and worth doing: Chromium user zoom (Ctrl+scroll) is an *uncontrolled* scale source sitting underneath all your DPI math. If the host does its own scaling, disable it (`PutIsZoomControlEnabled(false)`). Two builds once differed on exactly this, and the discrepancy stayed invisible until someone went looking for why they scaled differently.

---

## 13. Building on a third-party WebView2 binding — the slow squeeze

**Tried.** Depend on an existing Go WebView2 binding and stay out of the COM business entirely. It embeds the browser, wraps the settings object, and works.

**Looked reasonable because** hand-writing COM bindings in CGo-free Go is exactly the kind of work a library is supposed to save you: vtables, IIDs, refcounts, callback trampolines. Taking a dependency is what you are meant to do. And for the first weeks it was completely correct.

**Failed because** the boundary was not where it looked. The pressure came from four directions and never stopped:

- **A missing interface.** `ICoreWebView2Settings9` (`IsNonClientRegionSupportEnabled`) is what a custom title bar is built on, and the bindings stop at `Settings6` — on their main branch too, so waiting for a release does not help. The options are a fork, or reaching around the abstraction into raw COM *while it still owns the object*. Both were done. Neither is a place to live.
- **A missing lifetime.** The stream-lifetime leak in §8 stayed a "known limitation" because releasing a response at the right moment needs the binding to tell you when the runtime is done with it, and it does not. The limitation was an artifact of the dependency, not of WebView2.
- **A shipped DLL.** The binding's path runs through `WebView2Loader.dll`, which has to be shipped beside the executable — an odd thing for a library whose entire pitch is a single static binary.
- **The compat gate is not where it is documented to be.** The runtime's own DLL exports `CreateWebViewEnvironmentWithOptionsInternal` and enforces no version floor at all; the floor lives in the loader. That is only discoverable by leaving the loader behind.

**Instead.** The binding was written from scratch (`internal/webview2`): runtime discovery from the registry, the environment created by calling the runtime DLL's export directly, every interface derived from Microsoft's MIDL-generated `WebView2.h`, and the event handlers implemented as Go COM objects. The module now depends on `golang.org/x/sys` and nothing else, ships no loader DLL, and the memory leak of §8 is gone — because releasing at the right moment is a decision we are now in a position to make.

It is more code, and the ABI parts of it fail by crashing rather than by returning an error (§4 of `snap-and-nonclient-region.md` is the recipe, and it is not optional reading). It is still the smaller cost. Every problem above was the *same* problem wearing a different hat.

**Lesson.** A dependency's real cost is not what it does; it is **what it does not expose and cannot be made to expose.** A binding that covers 90% of an API is a fine dependency until the remaining 10% is precisely your product — and if you have already forked it once and reached around it once, the honest accounting says you own it, and you are merely paying for the pretence that you do not. Count the escape hatches you have opened into a dependency: two is a signal, three is an answer.

---

## 14. A data: document has no reportable source

**Symptom.** The fallback error surface loads, the window appears — and every
bridge message the page posts is rejected, `untrusted source, origin=:unknown`,
ten in a row: dead caption buttons on the one page whose whole job is having
working caption buttons (issue #56).

**Tried and dead.**

- **Recognise the surface by its message source.** The `WebMessageReceived`
  args' `GetSource` returns the **empty string** for a data: document — not the
  data: URI, not `null` (measured live, runtime 150.0.4078.65).
- **Ask the core at message time.** `ICoreWebView2.get_Source` — the current
  top-level document's URI — returns the empty string for the same document.
  The runtime erases the data: URI at both levels, so there is nothing to
  match; a `GetSource` binding written for this was deleted as dead code.

A diagnostic trap on the way: the rejection log's origin form collapses every
schemeless source — empty and `null` alike — to the same `:unknown`, so the raw
value can only be learned from a live probe. The rejection path now logs the
reduced raw source at debug for exactly this reason.

**Instead.** The host itself knows when it navigated to its own surface, so
identity comes from a UI-thread state machine (`noteNavigationOutcome`,
`errorSurfaceActive` in `host/webview_windows.go`): the empty source is admitted
only while the surface is the current document, and only for the reserved
window controls — `Config.Bridge` stays origin-gated (decisions/0014). The
accepted costs of that identification, and what would retire it, are recorded
in decisions/0017.

**Lesson.** When the runtime's own identity channel reports nothing, parsing
harder is not the answer; the identity you need must come from state you
already own.

---

## 15. The short version

1. **Search the DOM before the frame.** Native-looking symptoms are frequently web bugs. (§7)
2. **Log the container, not the content.** A tiny render is usually a tiny rect. (§2)
3. **Visibility is two booleans:** the parent's and the controller's. (§3)
4. **WebView2 does not render offscreen.** Any lifecycle trick that assumes it does — Electron's hide-until-ready above all — is dead on arrival. Cloak, do not hide. (§4)
5. **An improving metric is not acceptance.** Set the visual and behavioural hard gates before the experiment, and let them veto. (§5)
6. **Name what your harness cannot see.** Injected input does not reach the WebView; `MinTrackSize` ignores programmatic resizes. A test that cannot fail honestly should be deleted, not trusted. (§10, §11)
7. **When five fixes produce clean logs and no change, the ownership model is wrong** — not the message handling. (§1)
8. **No listening sockets in a desktop app.** Intercept the resource request instead. (§8)
9. **Count your escape hatches into a dependency.** When you have forked it once and bypassed it once, the abstraction is already gone; owning the binding is cheaper than pretending otherwise. (§13)
10. **A data: document reports no source.** Identify your own surfaces from navigation state you already hold, not by parsing the source harder. (§14)

> Last updated: 2026-07-18 | Editor: Claude (Fable 5) | Change: new §14 — the data: error surface has no reportable source at either GetSource level (issue #56, measured live), so admission comes from the host's own navigation state; the short version renumbered to §15.
