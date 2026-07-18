# WebView2 and asset serving

How the host talks to WebView2 without a third-party binding, and how the
frontend's assets are served without opening a port. Both sections moved
verbatim out of [architecture.md](./architecture.md) — the end-to-end map —
when that file crossed the 400-line reference-doc limit.

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
  package-level map keyed by the interface pointer is what keeps the object reachable, and
  the entry is deleted when the COM refcount reaches zero — so the map is a root, not a leak.
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
derived from `Config.VirtualHost` (`https://mullion.local` by default). The host registers
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

### Serving from a caller URL instead (`Config.URL`)

By default the frontend is the embedded `fs.FS` above. `Config.URL` is an opt-in that
points the WebView at an origin the caller serves themselves — a local dev server, or
a runtime that already speaks HTTP — instead. It is empty by default, so the no-port
guarantee is unchanged.

**mullion still opens no socket.** The caller runs the server; mullion only navigates
to it. When `Config.URL` is set, the `WebResourceRequested` filter is not registered
and the boundary matrix above does not run — the caller's server owns those concerns.
The injected scripts still run on every navigation, so `window.<ns>` (the bridge and
window controls) works on the caller's origin too, and on the fallback page a failed
navigation shows in place of Edge's chromeless error screen (`host/errorpage.go`).

That last point is why `Config.URL` is pinned to **loopback** (`127.0.0.1`,
`localhost`, `::1`) over `http`/`https`, and any other URL is rejected by `Run`:
injecting `Config.Bridge` — the application's Go methods — into an arbitrary remote
origin would hand that origin a path into Go. Loopback keeps it on the local machine.
Every run logs the source in effect (`asset source=embedded-fs …` or
`asset source=external-url, url=…`, with the path dropped), so a report shows which
was used. See [decisions/0012](./decisions/0012-config-url-loopback.md).

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

The inbound half of the same event obeys the same rule: `args.GetRequest()`
returns a reference the handler owns, and it is released as soon as the callback
returns (`handleWebResourceRequested`). The event fires for every intercepted
resource, so an unreleased request is not a one-off — it is one leaked COM
object per document, stylesheet, script, image and fetch, growing without bound
for the life of the window.

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

> Last updated: 2026-07-18 | Editor: Claude (Fable 5) | Change: new file — the two sections moved verbatim out of architecture.md, which had crossed the 400-line reference-doc limit (AGENTS.md, File size discipline).
