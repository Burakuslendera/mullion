# Architecture

## Contents

- [Overview](#overview)
- [Bootstrap contract (order matters)](#bootstrap-contract-order-matters)
- [Threading model](#threading-model)
- [Message routing](#message-routing)
- [Talking to WebView2 without a third-party binding](#talking-to-webview2-without-a-third-party-binding)
  - [Finding the runtime, and skipping the loader DLL](#finding-the-runtime-and-skipping-the-loader-dll)
  - [Event handlers are COM objects we implement](#event-handlers-are-com-objects-we-implement)
- [Asset serving without a port](#asset-serving-without-a-port)
  - [COM stream lifetime](#com-stream-lifetime)
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
   than continuing into a window that can never be correct. A caller with windows of
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
   same process can register it again.

5. **`HWND` creation.** The window procedure is bound at class registration, so the
   first messages the window ever receives — `WM_NCCALCSIZE` among them — already
   reach the library's routing. The frameless geometry is therefore correct on the
   first frame instead of being corrected afterwards.

6. **WebView2 embed.** The controller is created as a child of an `HWND` that already
   exists and is already DPI-aware. Every callback (web message, web resource
   requested, navigation completed, process failed) and every injected startup script
   is registered **before** the first `Navigate`; a callback registered after
   navigation begins can miss the requests and messages the first document produces —
   a race that reproduces only on fast machines, or only on slow ones, depending on
   where the gap lands.

7. **Show.** Parent window and WebView2 controller are both made visible explicitly.
   Showing the parent alone is not enough: the controller has an independent
   visibility flag, and a visible parent hosting an invisible controller renders as a
   blank window. Under `Config.StartHidden`, steps 6 and 7 defer to the first `Show`.

8. **Message loop.** `GetMessage` / `TranslateMessage` / `DispatchMessage`, owned by
   the library, pumping on the locked thread until `WM_QUIT`.

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
| deferred bounds resync | `WM_APP+28` | post (internal) |

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
- **`WM_DPICHANGED`** — applies the rect Windows suggests and resyncs the WebView
  bounds. Fires when the window crosses monitors with different scale factors.
- **`WM_SIZE`, `WM_MOVE`, `WM_MOVING`, `WM_WINDOWPOSCHANGED`, `WM_ENTERSIZEMOVE`,
  `WM_EXITSIZEMOVE`** — resync the controller's bounds to the parent client rect; the
  WebView2 controller does not follow its parent automatically.
- **`WM_ERASEBKGND`** — returns 1 without painting; the WebView covers the whole client
  area, so erasing the background only produces flicker.
- **`WM_INITMENU`** — syncs system-menu item state with real window state as the menu
  opens, since the library, not the default frame, decides what is currently possible.
- **`WM_CLOSE`** — offered to `Config.OnClose` first; returning true consumes the
  message, which is how a close-to-tray application keeps its process alive.
- **`WM_DESTROY`** — stops the render watchdog, shuts the WebView down, posts `WM_QUIT`.

Everything else falls through to `DefWindowProc`.

## Talking to WebView2 without a third-party binding

`internal/webview2` is the library's own COM binding for the WebView2 Win32 API. The
only module dependency is `golang.org/x/sys` — runtime discovery, environment creation,
every interface, and every event handler is implemented here, against Microsoft's
published interface definitions. Nothing is delegated to a browser binding that has to
be kept in step with the runtime.

### Finding the runtime, and skipping the loader DLL

The SDK's usual entry point is `CreateCoreWebView2EnvironmentWithOptions`, exported by
`WebView2Loader.dll`, which an application is expected to ship beside its executable.
The library ships no such DLL. Instead:

1. **Discover the runtime.** Read the Evergreen registration Edge Update publishes under
   `Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}` —
   `pv` (product version) and `location` — from `HKCU`, then `HKLM` in the 32-bit
   registry view (Edge Update is a 32-bit installer and writes under `WOW6432Node`),
   then the 64-bit view. `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` overrides all of it and is
   treated as a **pin**: if the folder holds no usable runtime the host fails rather
   than silently falling back to a different browser build. Every candidate is checked
   against the disk before it is accepted, because the registry outlives uninstalls.
2. **Load the runtime's own COM server**, `<runtime>\EBWebView\<arch>\EmbeddedBrowserWebView.dll`,
   with `LOAD_WITH_ALTERED_SEARCH_PATH` so its siblings resolve out of the install
   folder and not out of ours (the wrong folder, and possibly a writable one).
3. **Call its export directly:**

   ```c
   HRESULT CreateWebViewEnvironmentWithOptionsInternal(
       bool                                                          checkRunningInstance,
       int                                                           runtimeType,
       PCWSTR                                                        userDataFolder,
       IUnknown                                                     *environmentOptions,
       ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler   *handler)
   ```

   This is where `WebView2Loader.dll` does its real work; the loader is a convenience
   wrapper around it. `ICoreWebView2EnvironmentOptions` is a COM object the *caller*
   implements, so it is implemented in Go like any other handler.

Bypassing the loader is not free, and two traps only exist on this path. Both were found
live, against a real runtime, and neither is documented — the official loader hides them
by never letting you make the mistake.

**`TargetCompatibleBrowserVersion` must not be null.** The runtime validates the
property and rejects a null with `E_INVALIDARG`. `WebView2Loader.dll` always supplies a
value, so an application on the official path cannot discover this. Supplying an
invented version instead is worse, not better: a plausible-looking `"1.0.0.0"` is
rejected with `ERROR_FILE_NOT_FOUND`, because the runtime maps the version onto a real
browser build and finds none. The only answer that is both truthful and cannot fail is
to report **the version of the runtime that was just discovered**.

**The version floor lives in the loader, not in the runtime.** Declaring a target of
`"999.0.0.0"` against a 150 runtime *succeeds*. The compatibility gate that would have
refused it is implemented in `WebView2Loader.dll` — bypass the loader and the gate goes
with it. The consequence is a rule, and it is the important one on this page:

> **Detect features with `QueryInterface`, never with a version comparison.** A version
> number buys no protection here. `QueryInterface` asks the object that will actually
> serve the call whether it implements the interface, and its answer is the only one
> that is true by construction. Every optional interface — `ICoreWebView2Settings9`,
> `ICoreWebView2Controller3`, and the rest — is reached this way, and a missing one is a
> recoverable condition, not an error.

### Event handlers are COM objects we implement

`add_WebMessageReceived`, `add_WebResourceRequested`, `add_NavigationCompleted` and
`add_ProcessFailed` each take a COM object the runtime calls back into: a vtable, an
IUnknown implementation and a refcount, written in Go. Four constraints govern them, and
three of the four are fatal when violated.

- **Build vtables once, at package init.** `windows.NewCallback` allocates from a small,
  fixed table and never frees an entry. A callback allocated per handler *instance*
  exhausts the table; a vtable per interface wastes it. All four handler interfaces have
  the same COM shape (IUnknown + a single `Invoke` slot), so they share one vtable and
  one `NewCallback` for the whole process, and the per-instance IID lives in the object.
- **Keep a GC root.** Once a Go object's address has been handed to COM, the Go garbage
  collector cannot see the reference: the runtime holds an integer, not a Go pointer. A
  package-level map keyed by the interface pointer is what keeps the object reachable,
  and the entry is deleted when the COM refcount reaches zero — so the map is a root, not
  a leak.
- **Release *after* registering, never before.** `add_*` takes its own reference on the
  handler. Dropping ours before that call is a use-after-free; never dropping it is a
  leak. The correct order is: create with refcount 1, register, then release our one
  reference and let the runtime's own reference keep the object alive.
- **No panic may escape `Invoke`.** The caller is Chromium, and a Go panic unwinding into
  a C++ stack takes the process with it. Every `Invoke` recovers, reports through a hook,
  and **returns `S_OK` regardless**. A failing HRESULT out of an event handler is not a
  no-op: for `WebResourceRequested` the runtime reads it as "the handler produced no
  response", cancels the request and blanks the asset — so one buggy Go callback would
  turn into a dead window. `S_OK` means "the event was delivered", which is true whatever
  the callback did with it.

## Asset serving without a port

Assets come from an `fs.FS` — typically a `go:embed` FS — served on a synthetic origin
derived from `Config.VirtualHost` (`https://mullion.local` by default). The host
registers
`AddWebResourceRequestedFilter(origin+"/*", COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)`
and answers every request inside `WebResourceRequestedCallback`. Nothing binds a
socket; the request never leaves the process. Because that callback is the only
authority, it is also the only place the boundary can be enforced:

| Condition | Result |
| --- | --- |
| URI does not parse | `400` |
| scheme is not `https` | `403` |
| host is not the configured virtual host | `403` |
| path contains a `.` or `..` segment | `403` |
| path is `/` | rewritten to `index.html` |
| file exists | `200`, `Content-Type` from the extension |
| file missing | `404` |
| read fails otherwise | `500` |

The scheme and host checks matter because WebView2 hands the callback anything
matching the filter, and a filter is a pattern, not a trust boundary. The traversal
check runs on the raw path segments *before* any cleaning, so normalisation cannot
launder a rejected path into an accepted one. Responses carry `Cache-Control:
no-store`: the origin is identical across builds, so without it the WebView could
replay a cached asset from an older build into a new one. Bodies are wrapped in a COM
`IStream` built with `SHCreateMemStream`.

### COM stream lifetime

Serving one asset means handing WebView2 two COM objects — an `IStream` holding the
body, and an `ICoreWebView2WebResourceResponse` wrapping it — and the whole correctness
of the path is a reference-counting question. Three rules decide it, and each was
established by testing the runtime rather than by reading a signature:

| Call | What it does to the refcount |
| --- | --- |
| `CreateWebResourceResponse` | returns a response with a refcount of 1, owned by us |
| `response.PutContent(stream)` | the **response** takes its own reference on the stream |
| `args.PutResponse(response)` | the **runtime** takes its own reference on the response |

The body stream must be created first and attached with `PutContent`. Passing it to
`CreateWebResourceResponse` and releasing it on the way out — which reads like the
obvious thing to do, and is what a convenience helper is likely to do for you — frees
the body before anything reads it, because that call takes no reference of its own. The
failure is silent: every call returns success, navigation completes, and the document
loads with zero stylesheets and zero scripts and paints nothing. No exception, no
console message.

Once both `PutContent` and `PutResponse` have run, the runtime holds every reference it
needs and **ours are redundant, so both are released immediately** — the response and
the stream, at the end of the same callback that created them. Nothing accumulates for
the life of the process. This is only expressible because the library owns the COM
lifetime end to end: the earlier design, which could not see the runtime's own
references, had to retain both objects until `Run` returned and grew memory
monotonically with the number of requests served.

The general lesson: **when a COM object crosses an API boundary, establish who takes a
reference and who merely borrows one.** A Go wrapper around a COM interface cannot
express ownership in its type signature. Release too early and you get use-after-free
behaviour that presents as a rendering bug rather than a memory bug; release too late,
or never, and you get a leak that no test will fail on.

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
WindowStartDrag   WindowStartResize   WindowMinimise
WindowToggleMaximise   WindowIsMaximised   (diagnostics)
```

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
`Config.RenderTimeout` elapses first, the host logs an error carrying everything it
knows:

```
phase=<last frontend phase>   asset=<last asset served>
asset_category=…   asset_status=…
document=<n>  stylesheet=<n>  script=<n>  shim=<n>
last_bridge=<method:status>   shim_observed=<bool>
```

The counts are what make the payload diagnostic rather than decorative.
`document=1, stylesheet=0, script=0` is an asset-serving failure — the stream lifetime
bug above produces exactly this shape. `document=0` is a navigation or filter failure.
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
