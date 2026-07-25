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

### The two-second gap before the first subresource (issue #85)

This section is open work. It is written so the next person starts where the
measurements stopped rather than where the guessing did.

**Measured.** On one machine, WebView2 runtime 150.0.4078.83, on 2026-07-25:
five startup navigations across five runs each waited about **2.03 seconds**
between mullion serving the main document out of the callback above and the
renderer requesting its first subresource. The five figures are 2.041, 2.026,
2.035, 2.027 and 2.039 seconds, a spread of roughly 15 ms on a two-second
interval. Startup navigations are the clean population to measure: they are
host-initiated, not inside a retry chain, and not racing anything else.
Everything downstream is fast, first subresource to frontend-ready being 20-40
ms, so the interval from serving the document to frontend-ready is almost
entirely this one gap.

**Measured negatives.** Each of the following was tried and moved nothing.
They are the valuable half of the section, because each one closes a road:

| Changed | Result |
| --- | --- |
| `Content-Length` set on every `200` response | no change |
| response and `IStream` held past `PutResponse` rather than released on the `defer` | no change (2.026 -> 2.031) |
| no `AddScriptToExecuteOnDocumentCreated` registrations at all | no change (2.035) |
| virtual host `mullion.test` in place of `mullion.local` | no change (2.027) |

The gap also sits entirely **before document creation**. In one run the
`document created` diagnostic and the request for `style.css` landed in the same
millisecond, 2.015 s after the document itself was served. That observation
carries the only inference this section draws, and it is a narrow one: the wait
falls inside the runtime, between our response being handed over and the
document being created. It does not say what the runtime is doing during it.

**Suspected, on someone else's authority, and not confirmed here.**
[WebView2Feedback #2381](https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381)
reports the same two-second shape for a virtual host whose name does not
resolve, and *attributes* it to a network timeout. That attribution is their
reading of their own repro, not a mechanism anything above establishes: they use
`SetVirtualHostNameToFolderMapping` where mullion answers `WebResourceRequested`
itself, and no measurement listed here tells a name-resolution wait apart from
any other two-second wait. The issue is tagged bug / priority-low / tracked, and
is not fixed. Weaker, and recorded only so it is not rediscovered as news:
[WebView2Feedback #3398](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3398)
reports a 1-2 second slowdown after 8-10 loads through `WebResourceRequested`,
tracked and unexplained. It resembles the retry chains in issue #77, but nothing
connects the two.

**A probe that was staged and then withdrawn, because it could not have
answered.** The obvious next step looked like passing
`--host-resolver-rules=MAP mullion.local ~NOTFOUND` through
`Config.BrowserArguments`, to make the resolver answer at once instead of
waiting. It was written and then removed unrun, for three reasons that are worth
keeping so nobody re-stages it:

- **The instrument is unverified.** There is no report anywhere - docs,
  WebView2Feedback, or otherwise - of `--host-resolver-rules` being passed to
  WebView2, working or ignored. And
  [`get_AdditionalBrowserArguments`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2environmentoptions)
  states that switches important to WebView functionality are ignored, that some
  features are blocked internally, and that a switch which fails to parse is
  ignored - all of it **silently**. A blocked switch and a switch that worked and
  changed nothing produce the same observation, so a null result would have meant
  nothing at all.
- **The form was wrong.** The only workaround anyone has confirmed for #2381's
  shape is a hosts-file entry, which is a *successful* resolution to a reachable
  address. `~NOTFOUND` is a *fast failure*. Those separate two different
  mechanisms, and the staged form could not reproduce the one thing known to
  work. `MAP mullion.local 127.0.0.1` would have been the first form to try.
- **The hypothesis it tested is already weak, on our own data.** Chromium forces
  `.local` lookups onto the system resolver (`ResemblesMulticastDNSName` in
  `net/dns/host_resolver_manager.cc`) while `.test` is eligible for the built-in
  async resolver. Those are two materially different code paths, and the
  measurements above say they cost the same to within ~10 ms. That is the
  strongest evidence against name resolution being the wait, and it is ours, not
  a third party's.

**What replaced it as the leading candidate: proxy auto-config.** Chromium's
`configured_proxy_resolution_service.cc` carries a literal
`kDelayAfterNetworkChangesMs = 2000` - a deliberate stall before running proxy
auto-config, surfacing in NetLog as `PAC_FILE_DECIDER_WAIT` - and
`pac_file_decider.cc` caps a failing `wpad` lookup at `kQuickCheckDelayMs = 1000`.
It is the only candidate found with a documented two-second constant, which is
what a 15 ms spread over five runs asks for, and it is host-name independent, so
the `.local` / `.test` equivalence falls out of it rather than needing a
coincidence. Third parties have tied it to WebView2 specifically with traces
([#3707](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3707), where a
Microsoft engineer answered by recommending `--use-system-proxy-resolver`, and
[#1432](https://github.com/MicrosoftEdge/WebView2Feedback/issues/1432), closed,
with two independent confirmations that disabling auto-detect fixed it). On the
machine these measurements were taken, WPAD auto-detect is **on**: the
`DefaultConnectionSettings` flags byte under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings\Connections`
reads 9, i.e. bit 3 set, with no manual proxy and no PAC URL.

Its unexplained part, stated so it is not glossed: the 2000 ms is a
*post-network-change* stall, so it should cost once per browser process rather
than once per navigation. Every measurement above is a startup navigation in a
fresh process, which is exactly the population that cannot tell those apart.

**A second candidate, which explains the position the first does not.** A
request made at commit time, before document creation, would sit exactly where
the gap sits. SmartScreen is the named suspect: on
[#3398](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3398) a
Microsoft engineer read a submitted ETW trace and answered *"the trace suggest
that it was SmartScreen that could not keep up with the repeated navigations"*,
recommending `IsReputationCheckingRequired=false` or
`--disable-features=msSmartScreenProtection`. No published figure of two seconds,
and reputation results should cache. The two candidates compose: a reputation
lookup is the kind of request that would queue behind a proxy decision.

**The experiment that should be run instead.** Change the instrument, not the
flag. `--log-net-log` and `--net-log-capture-mode` are both on the
[documented WebView2 flags list](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/webview-features-flags)
and can be passed through the `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`
environment variable, so no rebuild is needed. One startup navigation, then read
the timeline between `put_Response` and the `style.css` request in
[the NetLog viewer](https://netlog-viewer.appspot.com/): a
`PAC_FILE_DECIDER_WAIT` span of ~2000 ms names the first candidate, a
`HOST_RESOLVER_MANAGER_JOB` for the virtual host of ~2000 ms revives the third,
and nothing at all in the window kills both in one run and moves the search to
the second. That last outcome is why this beats flipping a flag: a behavioural
probe that changes nothing tells you only that one thing was not it, whereas the
NetLog either names the span or proves the wait is not in the network stack at
all.

The gap is issue #85. It also bounds issue #77, an in-origin navigation that
aborts and often never commits, in that every abort measured so far fired inside
this two-second window. Whether that makes them one bug or two is open.

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: new asset-serving subsection records the measured two-second gap between serving the main document and the first subresource request, the four negatives that rule out Content-Length, COM lifetime, injected scripts and the host name, and why the host-resolver probe was staged and then withdrawn unrun - the switch is unverified on WebView2 and silently ignorable, the form was wrong, and our own .local/.test equivalence already weakens the hypothesis it tested. Proxy auto-config (PAC_FILE_DECIDER_WAIT, a literal 2000 ms in Chromium) leads instead, with a commit-time request such as SmartScreen second, and the experiment that should be run is a NetLog capture rather than another flag (issue #85, and its overlap with issue #77).
