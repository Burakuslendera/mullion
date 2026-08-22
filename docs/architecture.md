# Architecture

## Contents

- [Overview](#overview)
- [Bootstrap contract (order matters)](#bootstrap-contract-order-matters)
- [Threading model](#threading-model)
- [Message routing](#message-routing)
- [Native-routing test boundary](#native-routing-test-boundary)
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

`New` first normalises configuration and builds one immutable frontend source
plan, then checks the process architecture. The plan canonicalises the origin
once and supplies navigation, embedded request filter, asset checks, bridge
admission, navigation gate, fallback Retry and source logging. A credentialed
external start URL is an exact navigation-only plan capability; it never becomes
reusable origin proof
([decision 0036](./decisions/0036-one-source-plan-defines-origin.md)). On
supported Windows, an invalid `VirtualHost` or `URL` prevents even the process
DPI call; `Run` returns that source error before runtime discovery, COM, class or
`HWND` work. On unsupported Windows, the architecture sentinel remains `Run`'s
first result even when the source is also invalid.

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

5. **`HWND` creation.** A single, capture-free window-procedure trampoline is
   allocated lazily only after the architecture gate and UTF-16 preflight pass.
   `CreateWindowEx` receives a unique pending scalar token; a failed create rolls
   that pending entry back, while `WM_NCCREATE` promotes it to the new `HWND` and
   stores it in `GWLP_USERDATA`. Every later dispatch validates both.
   `WM_NCDESTROY` clears the userdata and removes every association for that
   handle, preventing callback-table growth, retained `Host` graphs and
   recycled-handle misdispatch.
   The window procedure is bound at class registration, so the first
   messages the window ever receives — `WM_NCCALCSIZE` among them — already reach
   the library's routing. The frameless geometry is therefore correct on the first
   frame instead of being corrected afterwards. The creation rect is computed first:
   `Config.Width`/`Height` are scaled by the primary monitor's effective DPI and
   centered in its work area, falling back to the shell's default position only when
   the monitor cannot be resolved
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
   life of the window.
   The same `Host` supports sequential `Run` calls; each session resets and
   poisons its destruction, startup-gate, timing, diagnostic and navigation
   state while retaining stable Logger/diagnostic objects for older
   goroutine-safe calls. Every private command carries a process-global,
   non-zero active-Run token in `lParam` and is rejected unless both that token
   and the callback HWND still match; process scope matters because Windows can
   recycle one numeric handle across two different `Host` values. Deferred
   bounds posts preserve their original token and HWND. An older Run therefore
   cannot mutate the newer owner. Concurrent calls to `Run` on one `Host` are
   rejected immediately, including calls arriving while the prior Run is still
   draining already-admitted work; they never wait into a new sequential session.

## Threading model

Public methods on `Host` are callable from any goroutine. Mutating window
commands use session-tagged Win32 send/post messages so the window procedure
applies them on the UI thread. `IsMaximised` deliberately pins `HWND` ownership
across a direct, cross-thread-safe `IsZoomed` query. Diagnostic and readiness
methods synchronize their bookkeeping, and readiness posts window-affine
follow-up instead of performing it on the caller. The command routes are:

| Method | Message | Delivery |
| --- | --- | --- |
| `Show()` | `WM_APP+21` | send — the caller needs the visible/not-visible result |
| `Hide()` | `WM_APP+22` | post |
| `Quit()` | `WM_APP+23` | post |
| `Minimise()` | `WM_APP+24` | post |
| `ToggleMaximise()` | `WM_APP+25` | post |
| `StartDrag()` | `WM_APP+26` | post |
| `StartResize(edge)` | `WM_APP+27` | post; `wParam` is one of the eight resize hit-test codes, validated again by the receiver |
| deferred bounds resync; `MarkFrontendReady()` / `MarkFrontendShellReady()` bounds sync | `WM_APP+28` | post; the source label travels in `wParam` |
| `SetTitle(title)` | `WM_APP+29` | send; a call-lifetime UTF-16 pointer travels in `wParam` |

Every private message in this table carries the originating active-Run token in
`lParam`; existing `wParam` payloads remain unchanged. The window procedure
compares token and HWND before the first command-specific log, browser call or
Win32 mutation, then validates command-specific payloads before their operation
seam. An API call belongs to the Run active when the method enters.
Entry increments a short locked admission count, then releases the mutex before
native, WebView, caller or Logger code: re-entrant Logger/bridge calls therefore
cannot self-deadlock. Teardown closes library-callback admission and waits for
already-entered public methods to finish their timing, log, bounds and show
effects before poisoning the token and allowing the next Run. A new `Run`
arriving during that drain is rejected rather than queued behind it. `SetTitle`
routes through the tagged UI command rather than dereferencing a looked-up
`HWND` on the caller.

Two threads enter a COM apartment, not one. The UI thread's is the process's
(`initializeCOM`, step 3 above). The second belongs to each system-browser
launch: `ShellExecuteW` can block while resolving and starting the registered
handler, so running it in a WebView2 event handler would park the message loop.
Each launch gets a goroutine that pins its OS thread, enters its own STA, calls
the shell, and returns
([decisions/0029](./decisions/0029-system-browser-launch-off-the-ui-thread.md)).
At most eight such workers may be in flight; excess launches are dropped and
warned. This bounds concurrent goroutines, OS threads, and shell calls only. It
is not a lifetime, per-document, per-origin, or time-window rate limit.

The opt-in `PinNavigationToOrigin` route hands off only after WebView2 accepts
cancellation. The default-on `NewWindowRequested` attempt first requires
`PutHandled(true)` to succeed; only that success suppresses the runtime popup.
`GetUri` and `GetIsUserInitiated` must then both succeed before the host routes.
A getter failure produces no host launch after suppression; a `PutHandled`
failure produces no host launch and leaves runtime popup behavior unspecified.
Both routes pass the successfully observed and admitted HTTP(S) URI unchanged to
`ShellExecuteW` as a fresh OS URL activation, not request replay: method, body,
headers, referrer, opener, WebView profile, and session are not preserved.
Windows and the handler decide process, tab, profile/session, stored-credential,
userinfo, query, fragment, and network behavior. `IsUserInitiated` remains
diagnostic classification, never physical-input authority. The current external
admission and bridge receipt/reply contract is the
[Issue #116 disposition](./bridge.md#issue-116-current-disposition);
[decision 0043](./decisions/0043-external-routes-are-uri-only-os-activations.md)
remains its historical rationale.

That worker is why `Config.Logger` carries a concurrency contract: an
implementation must be safe to call from more than one goroutine. The launch
reports from its own thread, and the render watchdog and the startup show gate
write from `time.AfterFunc` timers. Each library-owned callback preserves its
originating Run: it either takes a counted admission and finishes there, or
finds teardown/a different token and performs no post or log. A late
system-browser worker may finish its OS launch, but its warnings are similarly
suppressed after its Run ends. `NopLogger` is trivially safe and `SlogLogger`
inherits whatever `*slog.Logger` guarantees; a Logger of the embedder's own that
holds a buffer, a file handle or a counter of its own needs its own lock.

Drag and resize are handed back to the window manager rather than emulated:
`StartDrag` releases capture and sends `WM_NCLBUTTONDOWN` with `HTCAPTION`;
`StartResize` accepts only the eight edge hit-test codes before it can release
capture and send that message. The receiver repeats that validation *after*
validating its active Run token and `HWND`; a malformed private resize message is
logged and dropped before it can release capture or reach `DefWindowProc`.

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
- **`WM_SIZE`, `WM_MOVE`** — own the post-apply controller-bounds resync. The
  controller does not follow its parent automatically; `WM_MOVING` and
  `WM_WINDOWPOSCHANGING` expose only proposed geometry and must not notify it.
- **`WM_ENTERSIZEMOVE`, `WM_EXITSIZEMOVE`** — bracket the authoritative native
  move/size state as well as resyncing bounds. The host publishes a monotonic
  `{ maximised, moveSizeActive, generation }` snapshot to the injected pointer
  scripts. Drag and resize remain disabled while the snapshot is pending, failed,
  superseded or active, so a drag-down restore cannot expose a second gesture before
  the original system loop exits (issue #124).
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

Known Windows bug: on Windows 11 build 26200.8875, interrupting a held maximised
drag-down with `Win+Shift+S` and then pressing Escape returns the window to its
pre-loop maximised placement. Stock Notepad reproduces the same rollback. Mullion
sends no second maximise command on that path: the shell messages retain
`DefWindowProc` cancellation semantics, and compensating for the rollback would
make a mullion window behave differently from a stock window.

## Native-routing test boundary

Windows-tagged headless tests exercise the scalar callback registry and private
command seam without allocating an `HWND`: pending-token rollback, `WM_NCCREATE`
promotion, `WM_NCDESTROY` eviction, recycled-handle rejection, and the
token/`HWND`-then-resize-payload validation order are deterministic contracts.
They do **not** prove that Windows delivers the creation/destruction messages or
that a real non-client gesture enters the shell move-size loop. Verify those
native routing effects, together with frame, Snap, and mixed-DPI appearance, in
a live Windows session; do not treat a passing headless seam test as evidence of
desktop behaviour.

## Talking to WebView2, and serving assets

The in-house COM binding — runtime discovery, loader-bypass traps and
event-handler constraints — moved verbatim to
[webview2-and-assets.md](./webview2-and-assets.md). The asset boundary, caller-URL
transfer and COM stream lifetime are in [assets.md](./assets.md).

## Bridge protocol

The bridge protocol and origin-boundary contract moved verbatim to
[bridge.md](./bridge.md).

## Startup gates and watchdog

The show gate, render watchdog, and diagnostic counter interpretation moved verbatim
to [startup-gates-and-watchdog.md](./startup-gates-and-watchdog.md).

## Known limitations

**WebView2 does not render while hidden.** Under `Config.StartHidden` the WebView is not
created until the first `Show`, and even once created a hidden window produces no frames.
`MarkFrontendReady` will not fire — and the render watchdog means nothing — until the
window is actually shown. An application that starts in a tray must treat the first
`Show`, not `Run`, as the moment its frontend begins to exist.

**Windows/amd64 only.** WebView2 hosting uses x64 COM argument encodings.
Windows/386 and Windows/ARM64 remain compile-portable, but `Run` returns public
`ErrUnsupportedArchitecture` (for `errors.Is`) before DPI, discovery, shared
callback allocation, COM, class or HWND work. Doctor rejects the same process
before reading a pinned runtime path or probing the machine. CI gives the
supported target its own explicit Windows/x64 runtime-and-suite job, executes
the rejection gates under Windows/386 WOW64, and keeps ARM64 compile-only.
Non-Windows `Run` returns `ErrUnsupportedPlatform`; no portable window
abstraction is attempted
([decision 0034](./decisions/0034-webview2-hosting-is-windows-amd64-only.md)).

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: point the external-route overview to the canonical issue #116 disposition and retain 0043 as rationale.