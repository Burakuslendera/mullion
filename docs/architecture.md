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

4. **Window class registration.** The name comes from `Config.ClassName` and must be
   unique per process. It is unregistered when `Run` returns, so a later host in the
   same process can register it again — a pre-loop failure destroys the window first
   (and drains the quit that teardown posts), so the unregister succeeds on that
   path too.

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
   navigation completed, process failed) and every injected startup script is registered
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

8. **Message loop.** `GetMessage` / `TranslateMessage` / `DispatchMessage`, owned by
   the library, pumping on the locked thread until `WM_QUIT`. An abnormal
   `GetMessage` failure tears the window down the same way the pre-loop failure
   path does - destroy, drain, handle cleared - so even that exit leaves the
   process reusable and the WebView shut down.

`Run` blocks for the life of the window and must be called from the goroutine that
owns the process main thread.

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
implemented a single bridge method. Unknown methods, malformed requests and missing
arguments yield an `ok: false` response and a sanitised log line, never a panic.

## Startup gates and watchdog

A WebView2 control can embed successfully, navigate successfully, report
navigation-completed successfully — and still paint nothing. That white window is the
single most common field failure of this architecture, and neither the OS nor the
runtime reports it. Two independent timers exist because of it.

**Show gate.** After the embed the window is not shown immediately: the host waits for
the frontend to call `shellReady()`, which maps to `Host.MarkFrontendShellReady()` and
posts the show message, keeping the user from seeing an empty window while the first
document is still parsing. The wait is bounded by `Config.ShowTimeout`; when it expires
the host shows the window anyway and logs the reason. A gate that can hang forever is
worse than the flash it prevents, so the fallback is not optional.

**Render watchdog.** Armed before `Navigate`, cancelled by `Host.MarkFrontendReady()` —
the frontend's `ready()` call, made only after it has actually rendered. If
`Config.RenderTimeout` elapses first, the host logs an error carrying everything it knows:

```
phase=<last frontend phase>   asset=<last asset served>
asset_category=…   asset_status=…
document=<n>  stylesheet=<n>  script=<n>  shim=<n>
last_bridge=<method:status>   shim_observed=<bool>
```

The counts are what make the payload diagnostic rather than decorative.
`document=1, stylesheet=0, script=0` is an asset-serving failure — the stream lifetime
bug in [webview2-and-assets.md](./webview2-and-assets.md) produces exactly this shape. `document=0` is a navigation or filter failure.
Healthy counts with `phase` stuck early is a frontend fault. `shim_observed=false` means
the bridge never loaded. One line separates four root causes that all present as the same
white rectangle. Both timeouts are configurable; a negative value disables the mechanism.

## Known limitations

**WebView2 does not render while hidden.** Under `Config.StartHidden` the WebView is not
created until the first `Show`, and even once created a hidden window produces no frames.
`MarkFrontendReady` will not fire — and the render watchdog means nothing — until the
window is actually shown. An application that starts in a tray must treat the first
`Show`, not `Run`, as the moment its frontend begins to exist.

**Windows only.** The package is behind `//go:build windows`; `Run` returns
`ErrUnsupportedPlatform` elsewhere. WebView2, Win32 window management and the frameless
hit-test model have no portable equivalent, and no abstraction layer is attempted.

> Last updated: 2026-07-19 | Editor: Claude (Fable 5) | Change: step 5 records the computed creation rect — DPI-scaled `Config.Width`/`Height` centered in the primary work area (issue #59, decision 0018).
