# Architecture

## Contents

- [Overview](#overview)
- [Bootstrap contract (order matters)](#bootstrap-contract-order-matters)
- [Threading model](#threading-model)
- [Message routing](#message-routing)
- [Talking to WebView2, and serving assets](#talking-to-webview2-and-serving-assets)
- [Bridge protocol](#bridge-protocol)
- [Startup gates and watchdog](#startup-gates-and-watchdog)
- [Known limitations](#known-limitations)

## Overview

`mullion` hosts a WebView2 control inside a Win32 window that the library owns end
to end: it registers the window class, creates the `HWND`, draws no system frame,
and takes responsibility for the title bar, resize borders, snap behaviour, system
menu and DPI transitions itself. The package is pure Go — every Win32 and COM entry
point is reached through `golang.org/x/sys/windows` lazy DLL procs and syscall
callbacks — so a build needs no C toolchain and produces a single static binary. It
also never opens a listening socket: the frontend is an `fs.FS` compiled into the
executable and served to the WebView through `WebResourceRequested` on a synthetic
origin. No port means no firewall prompt, no collision between concurrent instances,
and no local HTTP surface reachable by any other process on the machine.

## Bootstrap contract (order matters)

Each step depends on state the previous one established, and several of those
dependencies are process-wide and irreversible.

1. **`SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)` — before any window
   exists.** DPI awareness is a per-process property that Windows latches the first
   time the process creates a window. Any `HWND` created earlier — a message-only
   window, a tray icon's hidden window, a splash — freezes the process into the mode
   in effect at that moment, and the later call silently fails. The WebView2 child
   inherits that mode, so getting this wrong yields a blurry, bitmap-stretched
   WebView on high-DPI monitors with no error reported anywhere. `New` applies the
   context immediately and stores the result; `Run` treats a failure as fatal rather
   than continuing into a window that can never be correct. An already-per-monitor-v2
   process — a second host, or an application that declared the context itself — is
   recognised as success rather than refused, and the awareness is re-verified on the
   `Run` thread before anything is created. A caller with windows of
   its own must construct the host first.

2. **`runtime.LockOSThread()`.** A Win32 window belongs to the thread that created
   it: message queue, window procedure and every COM call in an STA apartment are
   thread-affine. The Go runtime may migrate a goroutine between OS threads at any
   preemption point, moving message-pump calls onto a thread that owns no queue.
   Locking pins the goroutine for the rest of `Run`.

3. **COM init (`CoInitializeEx`, apartment-threaded).** WebView2 is a COM component
   and must be created on an initialised STA thread. An already-initialised apartment
   is tolerated, not fatal — an embedding application is allowed to have called
   `CoInitializeEx` first. Only a genuine failure aborts.

4. **Window class registration.** The class name and title are both converted to
   UTF-16 before class ownership or callback allocation, so an embedded NUL cannot
   strand the class or spend a callback-table slot before a corrected retry.
   `Config.ClassName` must be unique among live windows in the process. The class
   is unregistered when `Run` returns; every post-create exit destroys first and
   drains any quit posted by teardown, so a later session can register it again.

5. **`HWND` creation.** The window procedure is bound at class registration, so the
   first messages the window ever receives — `WM_NCCALCSIZE` among them — already
   reach the library's routing. The frameless geometry is therefore correct on the
   first frame instead of being corrected afterwards. The creation rect is computed
   first: `Config.Width`/`Height` are scaled by the primary monitor's effective DPI
   and centered in its work area, falling back to the shell's default position only
   when the monitor cannot be resolved
   ([decisions/0018](./decisions/0018-initial-placement-centered-on-primary.md)).

6. **WebView2 embed.** The controller is created as a child of an `HWND` that already
   exists and is already DPI-aware. Every callback (web message, web resource requested,
   navigation starting, navigation completed, process failed, new window requested) and
   every injected startup script is registered
   **before** the first `Navigate`; a callback registered after navigation begins can
   miss the requests and messages the first document produces — a race that reproduces
   only on fast machines, or only on slow ones, depending on where the gap lands.
   The embed is **single-flight, and window destruction cancels it**: environment and
   controller creation pump the message loop while they wait, so a message dispatched
   mid-embed can re-enter this path or destroy the window outright. A re-entrant attempt is
   refused rather than racing a second browser for the one `host.browser` commit,
   and a browser that completes after `WM_DESTROY` is torn down instead of being
   committed to a window that no longer exists. See
   [decisions/0016](./decisions/0016-single-flight-embed.md).

7. **Show.** Parent window and WebView2 controller are both made visible explicitly.
   Showing the parent alone is not enough: the controller has an independent
   visibility flag, and a visible parent hosting an invisible controller renders as a
   blank window. Under `Config.StartHidden`, steps 6 and 7 defer to the first `Show`.

8. **Message loop and teardown.** `GetMessage` / `TranslateMessage` /
   `DispatchMessage`, owned by the library, pump on the locked thread until
   `WM_QUIT`. Every post-create exit owns a final live-window check: pre-loop
   failures, `GetMessage == -1`, a bare thread `WM_QUIT` (`GetMessage == 0`) and
   panics after loop entry all destroy and drain when `WM_DESTROY` did not run.
   Normal `WM_DESTROY` clears the stored HWND before browser shutdown, then nils
   the browser after shutdown. Pending `WM_QUIT` ownership is tracked separately
   from that cleared handle, so a quit consumed and re-posted by the embed pump is
   still drained without ever destroying a recycled HWND. `Run` blocks for the
   life of the window and must be called from the process main-thread goroutine.
   The same `Host` supports sequential `Run` calls; each session resets its
   destruction, startup-gate, timing, diagnostic and navigation state while
   retaining stable Logger/diagnostic objects for older goroutine-safe calls.
   Deferred bounds posts carry the session generation and original HWND, so they
   cannot cross into a later Run even if Windows recycles the numeric handle.
   Concurrent calls to `Run` on one `Host` are rejected.

## Threading model

Public methods on `Host` are callable from any goroutine and none of them touch the
`HWND` directly. Each is expressed as a Win32 message delivered to the UI thread,
where the window procedure applies it:

| Method | Message | Delivery |
| --- | --- | --- |
| `Show()` | `WM_APP+21` | send — the caller needs the visible/not-visible result |
| `Hide()` | `WM_APP+22` | post |
| `Quit()` | `WM_APP+23` | post |
| `Minimise()` | `WM_APP+24` | post |
| `ToggleMaximise()` | `WM_APP+25` | post |
| `StartDrag()` | `WM_APP+26` | post |
| `StartResize(edge)` | `WM_APP+27` | post; edge travels in `wParam` as a hit-test code |
| deferred bounds resync; `MarkFrontendReady()` / `MarkFrontendShellReady()` bounds sync | `WM_APP+28` | post; the source label travels in `wParam` |

The pattern generalises: **the only thread allowed to call a window-affine Win32
function is the thread that pumps the queue.** `PostMessage` is the asynchronous
form, used wherever no result is needed; `SendMessage` is the synchronous form, safe
from a non-UI thread and used only where the caller must observe the outcome.
Read-only queries Windows documents as cross-thread safe (`IsZoomed`, behind
`IsMaximised()`) are called directly.

Two threads enter a COM apartment, not one. The UI thread's is the process's
(`initializeCOM`, step 3 above). The second belongs to the system-browser launch:
`ShellExecuteW` blocks until the target application has started, and running it
inside a WebView2 event handler parked the message loop for as long as that took,
so each launch gets a goroutine that pins its OS thread, enters its own STA,
launches and returns
([decisions/0029](./decisions/0029-system-browser-launch-off-the-ui-thread.md)).
Eight may be in flight; over that a launch is dropped and said out loud. It
touches no window, so the rule above is unaffected — nothing window-affine runs
there.

That worker is why `Config.Logger` carries a concurrency contract: an
implementation must be safe to call from more than one goroutine. The launch
reports from its own thread, and the render watchdog and the startup show gate
write from `time.AfterFunc` timers. `NopLogger` is trivially safe and
`SlogLogger` inherits whatever `*slog.Logger` guarantees; a Logger of the
embedder's own that holds a buffer, a file handle or a counter of its own needs
its own lock.

Drag and resize are handed back to the window manager rather than emulated:
`StartDrag` releases capture and sends `WM_NCLBUTTONDOWN` with `HTCAPTION`;
`StartResize` sends the same message with the edge's hit-test code. Snap, aero shake
and edge magnetism then work because Windows, not the library, runs the modal
move-size loop.

## Message routing

One window procedure switch routes everything.

- **`WM_NCCALCSIZE`** — returns a client rect spanning the whole window. This is what
  removes the system frame; the frontend draws the title bar in the space that opens.
- **`WM_NCHITTEST`** — the heart of a frameless window. A coordinate maps to
  `HTCAPTION` (title bar band, so the window manager provides drag, double-click
  maximise and snap layouts natively), `HTCLIENT` (caption buttons and interactive
  regions, so clicks reach the WebView), or one of the eight resize codes (`HTLEFT`,
  `HTTOPRIGHT`, …) inside the DPI-scaled border band. The wrong code here is the
  difference between a window that snaps and one that does not.
- **`WM_GETMINMAXINFO`** — clamps the maximised rect to the monitor work area. Without
  it a frameless window maximises over the taskbar, because the default maximised size
  assumes a system frame that no longer exists.
- **`WM_DPICHANGED`** — applies the rect Windows suggests, pushes the new scale into
  the WebView's rasterization scale, and resyncs the WebView bounds. Fires when the
  window crosses monitors with different scale factors.
- **`WM_SIZE`, `WM_MOVE`, `WM_MOVING`, `WM_WINDOWPOSCHANGING`, `WM_WINDOWPOSCHANGED`,
  `WM_ENTERSIZEMOVE`, `WM_EXITSIZEMOVE`** — resync the controller's bounds to the
  parent client rect; the WebView2 controller does not follow its parent automatically.
- **`WM_ERASEBKGND`** — returns 1 without painting; the WebView covers the whole client
  area, so erasing the background only produces flicker.
- **`WM_INITMENU`** — syncs system-menu item state with real window state as the menu
  opens, since the library, not the default frame, decides what is currently possible.
- **`WM_CLOSE`** — offered to `Config.OnClose` first; returning true consumes the
  message, which is how a close-to-tray application keeps its process alive.
- **`WM_DESTROY`** — records the destruction first, so a WebView2 embed still pumping
  cannot later commit a browser to a window that is gone (decision 0016); then stops
  the render watchdog, shuts the WebView down, posts `WM_QUIT`.

Everything else falls through to `DefWindowProc`.

## Talking to WebView2, and serving assets

The in-house COM binding — runtime discovery, the loader-bypass traps, the
event-handler constraints — and the whole asset path, boundary matrix and COM
stream lifetime included, moved verbatim to
[webview2-and-assets.md](./webview2-and-assets.md).

## Bridge protocol

The frontend calls into Go over the WebView's web-message channel with one JSON
envelope, wrapped by the injected shim as a promise API:

```js
// request                      // response
{ id, method, args }            { id, ok: true,  result }
                                { id, ok: false, error }

window.mullion.invoke(method, ...args) // -> Promise<result>
```

A monotonic sequence supplies `id`, and a pending map keyed by `id` settles the matching
promise when the response arrives. That map and the message listener live on a single
object on `window`, so multiple injected scripts and frontend modules share one channel
instead of each installing a listener of its own.

Go-side dispatch splits the method namespace. A reserved set is answered by the library
and never reaches the application:

```
WindowShow   WindowHide   WindowClose   WindowMinimise   WindowToggleMaximise
WindowIsMaximised   WindowStartDrag   WindowStartResize
WindowShellReady   WindowReady   WindowPhase   WindowDiagnostic
```

The first eight are the window controls. The last four are the signals the injected
scripts send back: the show gate (`shellReady`), the render watchdog (`ready`), and
the frontend diagnostics (`phase`, `diagnostic`).

Everything else is handed to `Config.Bridge` as the raw request JSON; it returns the raw
response JSON, or `""` to stay silent. `Bridge` may be nil — window controls
(`window.mullion.window.minimise()` and friends) work before the application has
implemented a single bridge method. Dispatch is origin-gated first
([decisions/0014](./decisions/0014-bridge-origin-at-dispatch.md)): only the trusted
origin reaches `Config.Bridge`; a `data:` document reaches the reserved window
controls alone, and its non-reserved calls — like every message from a foreign
origin — are dropped with a log line and **no reply to correlate**. An unknown
method with a bridge configured is the application's to answer; with none, it
yields `ok: false`. A malformed request is logged and dropped, never a panic.

Frontend-controlled method names, phases, diagnostic kinds/details and asset
labels are a separate projection of that raw protocol. Before the host logs or
retains one, it selects bounded input and emits at most 2,000 bytes. The first
reducible http(s) URL is reserved with its authority whole, so a long
`window.onerror` prefix cannot erase the URL and no cut can manufacture a
believable host prefix. Retained file names and reduced strings are detached
from the request's backing storage. This diagnostic boundary does **not** rewrite
the raw request passed to `Config.Bridge`
([decision 0035](./decisions/0035-frontend-diagnostics-are-bounded.md)).

## Startup gates and watchdog

A WebView2 control can embed successfully, navigate successfully, report
navigation-completed successfully — and still paint nothing. That white window is the
single most common field failure of this architecture, and neither the OS nor the
runtime reports it. Two independent timers exist because of it.

**Show gate.** After the embed the window is not shown immediately: the host waits for
the frontend to call `shellReady()`, which maps to `Host.MarkFrontendShellReady()` and
posts the show message, keeping the user from seeing an empty window while the first
document is still parsing. The wait is bounded by `Config.ShowTimeout`; when it expires
the host shows the window anyway and logs the reason. Shell readiness may arrive while
the embed pump is still running, before `Run` reaches the arm point; the once-only
release is remembered, so an already released gate cannot arm afterward. A fired or
stopped timer is detached immediately, including across sequential runs.

**Render watchdog.** Armed before `Navigate`, cancelled by `Host.MarkFrontendReady()` —
the frontend's `ready()` call, made only after it has actually rendered. If
`Config.RenderTimeout` elapses first, the host logs an error carrying everything it knows:

```
phase=<last frontend phase>   asset=<last asset served>
asset_category=…   asset_status=…
document=<n>  stylesheet=<n>  script=<n>
last_bridge=<method:status>
```

The counts are what make the payload diagnostic rather than decorative.
`document=1, stylesheet=0, script=0` is an asset-serving failure — the stream lifetime
bug in [webview2-and-assets.md](./webview2-and-assets.md) produces exactly this shape. `document=0` is a navigation or filter failure.
Healthy counts with `phase` stuck early is a frontend fault. `last_bridge=unknown` means
no bridge call ever arrived — the injected shim never ran. One line separates four root
causes that all present as the same white rectangle. Both timeouts are configurable; a negative value disables the mechanism.

**Read the counts knowing what they count.** They are bucketed from the
`Content-Type` mullion answered with, not from the file name and not from
WebView2's resource context: `text/html` increments `document`, `text/css`
`stylesheet`, anything containing `javascript` `script`, and **everything else is
counted nowhere**. The content type in turn comes from the name (decisions/0031),
so the chain is name → type → bucket, and a name mullion cannot classify breaks
it at the first link.

Two consequences a reader chasing a blank window needs, and neither is obvious
from the line itself:

- **Healthy counts do not mean the assets arrived.** Images, fonts, `.json`,
  `.wasm` and anything served `application/octet-stream` fall in the unbucketed
  class. A frontend whose real payload is a WebAssembly module reports
  `script=0` while working perfectly, and reports `script=0` when the module
  404s too.
- **A successfully served asset in that class produces no log line at all.**
  `logAssetResponseDebug` skips it deliberately — one page load can pull dozens
  of images and fonts, and logging each buries the three lines that say whether
  the document, its stylesheets and its scripts arrived. The suppression applies
  only to responses under `400`. A *failed* request is always logged, at `WARN`
  for `4xx` and `ERROR` for `5xx`, whatever its type, so a missing font is
  visible even though a present one is not.

## Known limitations

**WebView2 does not render while hidden.** Under `Config.StartHidden` the WebView is not
created until the first `Show`, and even once created a hidden window produces no frames.
`MarkFrontendReady` will not fire — and the render watchdog means nothing — until the
window is actually shown. An application that starts in a tray must treat the first
`Show`, not `Run`, as the moment its frontend begins to exist.

**Windows/amd64 only.** WebView2 hosting uses x64 COM argument encodings.
Windows/386 and Windows/ARM64 remain compile-portable, but `Run` returns a clear
unsupported-architecture error before COM initialization or window creation. On
non-Windows targets `Run` returns `ErrUnsupportedPlatform`; no portable window
abstraction is attempted
([decision 0034](./decisions/0034-webview2-hosting-is-windows-amd64-only.md)).

> Last updated: 2026-07-26 | Editor: Claude (Opus 5) | Change: docs-vs-code accuracy pass — step 6 lists the sixth callback (new window requested, 0022), and the bridge section states the origin gate and the real reply behaviour (a malformed or restricted-source request gets a log line and no reply, not `ok: false`). Then the threading model gained the second apartment it had been missing: the system-browser launch runs on a bounded set of per-launch goroutines with their own STA (issue #74, 0029), which is also why Config.Logger states a concurrency contract — this file enumerated the cross-thread rules without that one.

> Last updated: 2026-07-30 | Editor: Claude (Opus 5) | Change: the render-watchdog counters now say what they count and what they do not. They are bucketed from the Content-Type mullion answered with, which comes from the name (decisions/0031), so images, fonts, JSON, wasm and anything application/octet-stream are counted nowhere and a successfully served asset in that class prints no log line at all. Healthy counts therefore do not mean the assets arrived. The suppression applies only under 400; a failed request is always logged, WARN for 4xx and ERROR for 5xx.

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: narrow WebView2 hosting support to Windows/amd64 and distinguish runtime support from compile portability (decision 0034).

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: make teardown ownership complete for every host and backdrop loop exit, clear HWND/browser references at WM_DESTROY, make startup-show release race-free, and define sequential same-Host reuse (issue #97).

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: document the 2,000-byte frontend logging and retained-diagnostic boundary, complete-first-URL rule, detached asset names and unchanged raw Config.Bridge payload (decision 0035).
