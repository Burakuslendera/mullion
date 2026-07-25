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
   against the disk before it is accepted, because the registry outlives uninstalls,
   and a relative `location` value is dropped rather than resolved against the process
   working directory, so a malformed or planted registry entry cannot steer the load
   to a CWD-relative path (issue #69).
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

`add_WebMessageReceived`, `add_WebResourceRequested`, `add_NavigationStarting`,
`add_NavigationCompleted`, `add_ProcessFailed` and `add_NewWindowRequested` each take a
COM object the runtime calls back into: a vtable, an IUnknown implementation and a
refcount, written in Go. Four constraints govern them, and three of the four are fatal
when violated. `NewWindowRequested` is where a single-window host takes `window.open` /
`target=_blank` over from the runtime: it suppresses the runtime's own detached new
window and routes an http/https target to the system browser (decisions/0022).

- **Build vtables once, at package init.** `windows.NewCallback` allocates from a small,
  fixed table and never frees an entry. A callback allocated per handler *instance*
  exhausts the table; a vtable per interface wastes it. All six handler interfaces have
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

One handler carries a fifth constraint of its own, and it is an ordering.
`NavigationStarting` is the other half of the containment above: it asks the host
whether to cancel, calls `put_Cancel` itself, and only when *that* call succeeds
invokes a second host callback — `NavigationCancelledCallback` — which is where
everything following from a cancel happens. A `put_Cancel` that fails warns, naming
the navigation, and tells the host nothing: the navigation is going ahead, so it has
to reach the host as an ordinary one rather than as a cancel that never took
(decisions/0027). Both getters on this event report their own failure for the same
reason — an unreadable URI arrives at a host gate as the empty string, which is no
origin's, and a failed id read arrives as `0`, which no real navigation uses.

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
| path contains a `.` or `..` segment — or one made only of dots and spaces (`.. `, `...`), which Windows' DOS-to-NT conversion can strip to one | `403` |
| path contains a backslash, a colon or a control byte (incl. `%5c`, `%00`) | `403` |
| path is not a valid `fs.FS` name (`fs.ValidPath` — raw invalid UTF-8 among others) | `403` |
| path is `favicon.ico` | `204`, answered before any file lookup |
| path is `/` | rewritten to `index.html` |
| file exists | `200`, `Content-Type` from the extension |
| file missing | `404` |
| read fails otherwise | `500` |

The scheme and host checks matter because WebView2 hands the callback anything
matching the filter, and a filter is a pattern, not a trust boundary. The traversal
check runs on the raw path segments *before* any cleaning, so normalisation cannot
launder a rejected path into an accepted one. The same pre-clean pass rejects a
backslash, a colon or a control byte: `url.Parse` decodes `%5c` to a literal `\`,
which the `/`-only segment split and `path.Clean` would both carry through as an
ordinary byte, so on Windows it would act as a second path separator — and a `:`
selects a drive letter or an NTFS alternate data stream. The final gate asserts
`fs.ValidPath`, the canonical rule for a name an `fs.FS` will accept; its UTF-8
requirement is load-bearing, rejecting a raw invalid byte the rune-level checks
decode to U+FFFD and would otherwise pass. The boundary rejects all of this itself
rather than leaning on the caller's `fs.FS` or the OS to (issue #66). The
`favicon.ico` row is a convenience, not a boundary: the browser probes for it on every
navigation, and answering `204` keeps that probe from surfacing as a resource-load
failure in the diagnostics of every run. Responses carry `Cache-Control:
no-store`: the origin is identical across builds, so without it the WebView could
replay a cached asset from an older build into a new one. They also carry
`X-Content-Type-Options: nosniff` — every response names an explicit
`Content-Type`, so the header is inert except for the sniffable `text/plain` case,
where it stops bytes an app serves as plain text from being content-sniffed into
executable HTML on the bridge origin (issue #13). Bodies are wrapped in a COM
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

That difference also decides what a *failed* navigation means. With `Config.URL`
set there is a socket in the path, so an aborted load can be a dead endpoint — the
case the fallback page exists for. Serving the embedded assets in process there is
none, so an abort of a navigation that was headed for the trusted origin can only
be one the runtime abandoned and restarted, and showing the fallback there would
replace a live frontend over nothing. Only that combination skips the page; an
aborted *off-origin* navigation is a real socket load and still shows it, because
`Config.URL` being empty does not keep the top frame on the origin — see
[decisions/0024](./decisions/0024-benign-abort-in-process.md).

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
needs and **ours are redundant, so both are released on a `defer`** — the response and
the stream, at the end of the same callback that created them. The `defer` is
load-bearing, not stylistic: the event dispatch recovers a panicking handler and keeps
the process alive, so an inline release could be skipped by a panic between creating the
response and returning, stranding both refs for the life of the process (issue #45).
Nothing accumulates for the life of the process. This is only expressible because the library owns the COM
lifetime end to end: the earlier design, which could not see the runtime's own
references, had to retain both objects until `Run` returned and grew memory
monotonically with the number of requests served.

The general lesson: **when a COM object crosses an API boundary, establish who takes a
reference and who merely borrows one.** A Go wrapper around a COM interface cannot
express ownership in its type signature. Release too early and you get use-after-free
behaviour that presents as a rendering bug rather than a memory bug; release too late,
or never, and you get a leak that no test will fail on.

### The two-second gap before the first subresource (issues #85, #77)

**Root cause found and measured: the virtual host name is resolved, and the
lookup times out.** Every navigation waited about 2.03 seconds between mullion
serving the main document out of the callback above and the renderer requesting
its first subresource. A NetLog capture named the span: a
`HOST_RESOLVER_MANAGER_JOB` for `mullion.local:443`, running **2.007 s** and
covering exactly that window. Changing the virtual host to a name Chromium never
sends to the network collapses it.

| virtual host | document to first subresource | in-origin navigations |
| --- | --- | --- |
| `mullion.local` | 2.012 - 2.041 s, seven runs | 45 consecutive aborts, none committed |
| `mullion.localhost` | **11 - 79 ms** | **16 of 16 committed, none aborted** |

`LaunchToWindowVisibleMs` went from 2419-2543 to **495-508** on the same machine
and frontend. Issue #77 - an in-origin navigation that aborts and often never
commits - disappeared with it: the two-second window was where that race lived,
and at 15 ms there is nothing left to lose it in.

**Why that name and not another.** The rule is not "pick a name that will not
resolve" - that is the version that fails. `.example`, `.test` and `.invalid` are
reserved by RFC 2606 so that nobody registers them, but a resolver still asks the
network and still waits out the answer. `.localhost` is reserved by RFC 6761 as
*always loopback*, and Chromium answers it without a lookup at all. Renaming
`mullion.local` to `mullion.test` was measured first and changed nothing (2.027
s), which is the same result three other people have reported for `.example` on
the upstream issue.

**What was ruled out on the way, each by its own measurement.** Recorded so the
next person does not re-run them:

| Changed | Result |
| --- | --- |
| `Content-Length` set on every `200` response | no change |
| response and `IStream` held past `PutResponse` rather than released on the `defer` | no change (2.026 to 2.031) |
| no `AddScriptToExecuteOnDocumentCreated` registrations at all | no change (2.035) |
| virtual host `mullion.test` | no change (2.027) |
| `--no-proxy-server` | no change (2.017) |
| `--host-resolver-rules=MAP mullion.local 127.0.0.1` | no change - **and the switch was never applied**, see below |

The response-lifetime negative is worth keeping on its own: it confirms
behaviourally what `asset_responses_windows.go`'s comment previously only
assumed, that the runtime takes its own references at `PutResponse`.

**Two things the NetLog settled that no behavioural probe could.** First, proxy
auto-config was the leading suspect before the capture - Chromium carries a
literal `kDelayAfterNetworkChangesMs = 2000` and WPAD auto-detect is on for this
machine - and the capture killed it outright: the `wpad:80` lookup failed in
about 10 ms and the proxy resolved to `DIRECT` well before the document was
served. Second, `--host-resolver-rules` was passed and **silently ignored**:
`127.0.0.1` appears nowhere in the capture. That matters beyond this bug.
[`get_AdditionalBrowserArguments`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2environmentoptions)
documents that WebView2 ignores switches it blocks or cannot parse, without
saying which - so a behavioural probe built on a Chromium flag cannot distinguish
"not the cause" from "never applied", and two runs were spent learning only that.

Upstream:
[WebView2Feedback #2381](https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381)
reports the same shape for `SetVirtualHostNameToFolderMapping` and attributes it
to a name lookup - which the capture here confirms for the
`WebResourceRequested` path as well. It is tagged bug / priority-low / tracked
and has been open since 2022. The workaround offered there is a hosts-file entry,
which an application cannot ask of its users; `.localhost` needs nothing from the
machine. Note that the Microsoft engineer's advice on that issue - use
`*.example` - does not work, and has now been contradicted four times including
by the `.test` measurement above.

**The fix is not applied yet, and the obstacle is worth stating.** Changing
`defaultVirtualHost` to `mullion.localhost` fails `TestNoNetworkListener`: that
test is the no-port promise's guard (decisions/0002), it greps the tree for
loopback literals, and it reads the "localhost" inside the new default as one.
Six tests fail in total; the other five pin the current default and are ordinary
updates. So the change is not a one-line default swap - it needs the guard taught
to tell a virtual host name from a loopback URL, without weakening what it
catches. That is a decision record's worth of work, not a rename.

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: the two-second gap is resolved - a NetLog capture named it as a HOST_RESOLVER_MANAGER_JOB for the virtual host running 2.007 s, and moving the host to a .localhost name (RFC 6761, never sent to the network) collapses it to 11-79 ms and takes issue #77's aborts with it (16 of 16 in-origin navigations commit where 45 consecutive ones had aborted). Records the six negatives, the fact that --host-resolver-rules is silently ignored by WebView2, and why the default cannot simply be renamed: TestNoNetworkListener reads the "localhost" in it as a loopback literal.
