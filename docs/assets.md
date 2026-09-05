# Asset serving without a port

## Contents

- [Serving from a caller URL instead (`Config.URL`)](#serving-from-a-caller-url-instead-configurl)
- [COM stream lifetime](#com-stream-lifetime)
- [The two-second gap before the first subresource (issues #85, #77)](#the-two-second-gap-before-the-first-subresource-issues-85-77)

Assets come from an `fs.FS` — typically a `go:embed` FS — served on a synthetic origin
derived from `Config.VirtualHost` (`https://mullion.localhost` by default). The host registers
`AddWebResourceRequestedFilter(origin+"/*", COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)`
and answers every request inside `WebResourceRequestedCallback`. Nothing binds a
socket; the request never leaves the process. Because that callback is the only
authority, it is also the only place the boundary can be enforced:

The filter and callback do not reconstruct that origin. `New` creates one
immutable canonical source plan, and the filter pattern, `assetProvider` origin
check, initial navigation, bridge gate, Retry target and source log all consume
it ([decision 0036](./decisions/0036-one-source-plan-defines-origin.md)).
`sourcePlan.summary` is created from the plan's canonical, credential-free
origin; logging never re-reads the raw `Config.URL` or `Config.VirtualHost`.
Credentials, path, query and fragment may remain in an external initial
navigation capability, but none can enter the retained startup summary.
`TestExternalSourcePlanCanonicalizesOnceAndRedactsSummary` locks that separation,
while `TestEmbeddedSourcePlanCanonicalizesEveryConsumer` locks the corresponding
embedded consumer agreement.

An invalid embedded `Config.VirtualHost` fails source-plan construction. On
supported Windows, `Run` returns that error before process DPI setup, runtime
discovery, COM, window-class registration or `HWND` creation.
`TestInvalidSourcePreflightPrecedesSupportedNativeStartup` counts the first two
native dependencies to lock this early boundary; it does not execute native
setup.
Registering the embedded filter is mandatory after the Browser commit: failure
immediately uncommits and shuts down that Browser before settings, scripts,
watchdog or navigation, then returns for the terminal host policy to report
once. No request
path runs without its filter.

| Condition | Result |
| --- | --- |
| URI does not parse | `400` |
| scheme is not `https` | `403` |
| host or effective port differs from the planned embedded origin | `403` |
| path has a `.` or `..` segment, **or any segment ending in a dot or a space** (`notes.txt.`, `sub./x`) — Windows' DOS-to-NT conversion strips those, so the name would not be the file | `403` |
| path contains a backslash, a colon or a control or rendering-control character (incl. `%5c`, `%00`) | `403` |
| path is not a valid `fs.FS` name (`fs.ValidPath` — raw invalid UTF-8 among others) | `403` |
| path is `favicon.ico`, no file exists | `204`, shortcut answered after lookup |
| path is `favicon.ico`, file exists | served normally as `200` (the shortcut does not shadow it) |
| path is `/` | rewritten to `index.html` |
| path names an existing directory | `404`, `missing` — a directory is not an asset |
| file exists, name carries a type mullion knows | `200`, `Content-Type` from the **name**, never from the bytes (0031) |
| file exists, name carries none | `200`, `application/octet-stream` — with `nosniff`, a download rather than a document |
| file missing | `404` |
| read fails otherwise | `500` |

The scheme and host checks matter because WebView2 hands the callback anything
matching the filter, and a filter is a pattern, not a trust boundary. The traversal
check runs on the raw path segments *before* any cleaning, so normalisation cannot
launder a rejected path into an accepted one. The same pre-clean pass rejects a
backslash, a colon, a control character or a rendering control such as a
zero-width or bidi character: `url.Parse` decodes `%5c` to a literal `\`, which
the `/`-only segment split and `path.Clean` would both carry through as an
ordinary byte, so on Windows it would act as a second path separator — and a `:`
selects a drive letter or an NTFS alternate data stream. Rendering controls can
make an admitted name display as a different name. The final gate asserts
`fs.ValidPath`, the canonical rule for a name an `fs.FS` will accept; its UTF-8
requirement is load-bearing, rejecting a raw invalid byte the rune-level checks
decode to U+FFFD and would otherwise pass. The boundary rejects all of this
itself rather than leaning on the caller's `fs.FS` or the OS (issue #66).

One further class is rejected for the same reason (issue #100). Any segment
ending in a dot or a space is refused, not only a segment made entirely of them:
Windows strips those, so `notes.txt.` is an alias for `notes.txt`, and the name
mullion classified would not be the file the OS opens.

**Windows device names are not filtered here** — `/nul`, `/con`, `/com1` reach
the caller's `fs.FS` like any other name. That is a decision (`decisions/0031`),
and it rests on measurement rather than on oversight: `os.DirFS` refuses the bare
names itself on go1.22 already (`dirFS.join` → `safefilepath.FromFS` →
`IsReservedName`, renamed to `filepathlite.Localize` in 1.23 without a behaviour
change; identical on go1.22.12, go1.23.12 and go1.26.5), and an `embed.FS` never
reaches the OS at all. The only caller a filter would help is one who wrote an
`fs.FS` that hands names to `os.Open`, and that is their own code to guard.

The price is recorded rather than hidden: through such a passthrough `fs.FS`,
`ReadFile("nul")` returns zero bytes and a nil error, so `/nul` answers `200` with
an empty body, and the request path is chosen by the page rather than by the
application. `TestAssetBoundaryDoesNotFilterDeviceNames` locks the decision, so a
later "hardening" that adds the filter back goes red and points at the record.

**Reparse points are not a name problem, and the boundary cannot see them.** A
directory junction inside the asset root points outside it, and
`junction/secret.txt` is an ordinary name that passes every check above — the
redirection lives in the filesystem, not in the string (issue #103). So this one
is answered by the `fs.FS` the caller supplies rather than by the boundary, and
the two standard ones differ. Measured on go1.24.6 and go1.26.5 against the same
`mklink /J` fixture: `os.DirFS(dir)` read the file from outside the root, while
`os.OpenRoot(dir).FS()` answered `path escapes from parent` and served the
legitimate assets normally.

That is why the module's Go floor is 1.24 and why `Config.Assets` recommends
`os.OpenRoot(dir).FS()` for a directory
([decisions/0042](./decisions/0042-go-1-24-remains-the-released-consumer-floor.md)).
It is a recommendation and not an enforcement: `Config.Assets` is an `fs.FS`,
mullion cannot tell which one it was handed, and a caller who passes `os.DirFS`
keeps the old behaviour.

Two limits, both measured, because "`os.Root` keeps you inside the directory" is
the sentence a reader would carry away and it is wrong in both directions.
`os.Root` refuses reparse points whose tag is a **name surrogate** — junctions and
symlinks — so a **hard link** out of the root is served exactly as `os.DirFS`
serves it, and `mklink /H` needs no elevation. And it refuses those tags wherever
they point, so a junction whose target is **inside** the root (`dir/alias ->
dir/real`, a build-step convenience) answers `500` where `os.DirFS` answers `200`.
An asset directory that other code can write into is not contained by any of
this; the remedy there is an `embed.FS` or a directory nothing else writes to.
`embed.FS` is unaffected either way — nothing in it reaches the OS.
`TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot` pins both halves, so the
recommendation fails loudly if either file system changes;
`TestAssetBoundaryOSDirFSDoesNotEscapeViaDotOrSpaceForms` covers the lexical
forms next to it.

Back to the table. The
`favicon.ico` row is a convenience, not a boundary: the browser probes for it on every
navigation, and answering `204` keeps that probe from surfacing as a resource-load
failure in the diagnostics of every run. Mullion stats the entry first, so a real
`favicon.ico` is served as an ordinary asset; only an absent file takes the `204`
shortcut. Responses carry `Cache-Control: no-store`: the origin is identical across
builds, so without it the WebView could replay a cached asset from an older build into
a new one. They also carry `X-Content-Type-Options: nosniff` — it stops bytes an app
serves as plain text from being content-sniffed into executable HTML on the bridge
origin (issue #13).

That header only protects what mullion labelled correctly, and until issue #100
mullion did the sniffing itself: `contentTypeForAsset` fell back to
`http.DetectContentType` for any name whose extension it did not recognise, and
`DetectContentType` answers `text/html` for anything opening with a tag. Measured
over `os.DirFS` on byte-identical content, `note.txt` was typed `text/plain`
while `abc123` and `x.foobar` were typed `text/html` — so an application serving
an upload directory or a content-addressed blob store got HTML execution in its
own origin, and `nosniff` then made the wrong label irreversible. The fallback is
now `application/octet-stream`: the function takes no content and never inspects
the bytes. `mime.TypeByExtension` remains in the middle, unpinned — it is still a
decision about the name, which is the model, not about the bytes.

Bodies are wrapped in a COM `IStream` built with `SHCreateMemStream`.
`SHCreateMemStream` takes a 32-bit `UINT` byte count. A body larger than
4,294,967,295 bytes is rejected before that call; otherwise Windows would
silently use only the low 32 bits and expose a truncated or empty stream (issue
#120). The request fails and the asset-response error is logged rather than
serving partial content.

### Serving from a caller URL instead (`Config.URL`)

By default the frontend is the embedded `fs.FS` above. `Config.URL` is an opt-in that
points the WebView at an origin the caller serves themselves — a local dev server, or
a runtime that already speaks HTTP — instead. It is empty by default, so the no-port
guarantee is unchanged.

**mullion still opens no socket.** The caller runs the server; mullion only
navigates to it. `Config.URL` is parsed once into the source plan described
above. Its canonical HTTP(S) loopback origin drives bridge admission, later
navigation policy, Retry and the source summary, while the exact configured
start URL alone retains the caller's path, query, fragment or userinfo.
That navigation-only capability never proves origin identity for another
navigation or bridge message. When `Config.URL` is set, the
`WebResourceRequested` filter is not registered and the boundary matrix above
does not run — the caller's server owns those concerns.
The injected scripts still run on every navigation, so `window.<ns>` works on
the caller's origin; a failed load shows Mullion's controllable fallback instead
of Edge's chromeless error screen (`host/errorpage.go`).

That difference also controls how an abort is classified. With `Config.URL` set,
a socket load can fail and must arm the fallback. For embedded assets, an exactly
attributed `ConnectionAborted` aimed at the trusted origin is treated as the
runtime abandoning a navigation it restarts, based on issue #72; this is a
classification, not proof that every byte was delivered. Off-origin, anonymous
and stale-identity aborts still arm. In particular, a later navigation start
overwrites the single recorded target, so an older abort takes the fail-closed
path and may replace a live frontend.
[Decision 0024](./decisions/0024-benign-abort-in-process.md) owns that cost; its
bounded reachability evidence is in the
[verification records](./verification/records/2026-08.md#2026-08-records).

That last point is why `Config.URL` is pinned to **loopback** (`127.0.0.1`,
`localhost`, `::1`) over `http`/`https`, and any other URL is rejected by `Run`:
injecting `Config.Bridge` — the application's Go methods — into an arbitrary remote
origin would hand that origin a path into Go. Loopback keeps it on the local machine.
Every run logs the plan summary (`asset source=embedded-fs …` or
`asset source=external-url, url=…`), so a report identifies the source without
repeating caller URL details. The loopback rationale remains
[decision 0012](./decisions/0012-config-url-loopback.md).

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

**Root cause found and measured: the navigation waits on the resolution of the
virtual host name.** Every navigation waited about 2.03 seconds between mullion
serving the main document out of the callback above and the renderer requesting
its first subresource. A NetLog capture named the span: a
`HOST_RESOLVER_MANAGER_JOB` for `mullion.local:443`, running **2.007 s**. The fit
is read across two logs, because the capture holds neither end of the window -
WebView2 answers both documents from the virtual-host callback, so they never
reach the URLRequest path. The job spans 23:28:47.238 to 49.245; mullion's own
log for that run served `index.html` at 47.245 and `style.css` at 49.257.
Moving the virtual host to a name reserved for loopback collapsed it, and that
name is the default as of [decisions/0030](./decisions/0030-guard-exempts-the-virtual-host-name.md).

What the capture records there is a duration, not an outcome. The job ends with
its last request detaching and a `CANCELLED` event, carrying no `net_error` and
no finished attempt - so "the lookup times out" is the upstream issue's wording,
marked as theirs below, and is not something measured here.

Nor is non-existence on its own the explanation. In the same capture the three
`wpad:80` resolver jobs - another name that does not exist on this machine,
though a single-label one reached down a different path - finished in **3, 2 and
1 ms** with `ERR_NAME_NOT_RESOLVED`, while `mullion.local:443` ran 2.007 s and
was then abandoned. What separates the two is not recorded here, and
the obvious candidate does not survive this section's own table: Chromium routes
`.local` to the system resolver and lets `.test` use its built-in one
(`ResemblesMulticastDNSName`, `net/dns/host_resolver_manager.cc`), but
`mullion.test` measured 2.027 s, so both paths cost the same and the routing
cannot be what makes the difference. The wait is measured and its span is named;
the mechanism inside it is still open.

| virtual host | document to first subresource | in-origin navigations |
| --- | --- | --- |
| `mullion.local` | 2.012 - 2.041 s, seven runs | 45 consecutive aborts, none committed |
| `mullion.localhost` | **47 - 141 ms**, five runs | **31 of 31 clicked, all committed** |

`LaunchToWindowVisibleMs` went from 2419-2543 to **448 - 630 ms** on the same
machine and frontend. **Not on the same runtime, though:** the old numbers are
from 150.0.4078.83 and the new ones from 150.0.4078.99, and `mullion.local` was
never re-measured on .99. The comparison is therefore not single-variable, and
nothing here rules out the update having contributed. Issue #77 - an in-origin
navigation that aborts and often never commits - disappeared with it: the
two-second window was where that race lived, and there is nothing left to lose
it in.

**The post-fix row settles an older disagreement by replacing it.** Two readings
had been recorded from an earlier run whose raw logs were not kept - 11-79 ms in
#85 and 11-22 ms in #77, a minute apart. Neither reproduces. Five runs on
2026-07-28, runtime 150.0.4078.99, measured 47, 48, 57, 64 and 141 ms, the 141 on
the session's first run and the rest inside 47-64, and those logs are kept. What
the older readings supported is untouched: two orders of magnitude below the wait.

**Measure the second run from a given launcher.** This package points WebView2 at
`%LOCALAPPDATA%\<executable name>\WebView2` (`internal/webview2/browser_windows.go`;
the runtime's own fallback is a folder beside the executable, which is why that
default exists), so a different launcher is a different profile. Run from an IDE,
which builds under its own long name, the same commit created a profile at the
second the run started and its first navigation took **1633 ms**, against **15 ms**
on the second run of that configuration. Only the first paid it. That the 1.6 s is
profile creation is the obvious reading rather than a measured one, and that
console log was not kept; what is kept is the folder, stamped at the run's own
second. `go run .` keeps one profile, which is what makes the five runs comparable.

**End the window at the first subresource, not at document-created.** The
`frontend diagnostic phase, phase=document created` line is timestamped when the
host *receives* the message, and `host/diagnostics.js` sends it over the bridge
from the injected script, so it lags the event. Against a two-second wait that was
invisible; at 50 ms it is not, and in all five runs `style.css` was served 3-12 ms
**before** it. The subresource request is the renderer's own.

**Why that name and not another.** The rule is not "pick a name that will not
resolve" - that is the version that fails. `.example`, `.test` and `.invalid` are
reserved by RFC 2606 so that nobody registers them, which says nothing about what
a resolver does when it is asked for one. `.localhost` is different in kind: RFC
6761 reserves it as *always loopback* and requires resolvers to answer it without
querying the network. That requirement is the RFC's. No capture was taken on the
new name, so what is measured here is the 47-141 ms that replaced the wait, not
the absence of a lookup. Renaming `mullion.local` to `mullion.test` was measured
first and changed nothing (2.027 s), which is the same result three other people
have reported for `.example` on the upstream issue.

**What was ruled out on the way, each by its own measurement.** Recorded so the
next person does not re-run them:

| Changed | Result |
| --- | --- |
| `Content-Length` set on every `200` response | no change |
| response and `IStream` held past `PutResponse` rather than released on the `defer` | no change (2.026 to 2.031) |
| no `AddScriptToExecuteOnDocumentCreated` registrations at all | no change (2.035) |
| virtual host `mullion.test` | no change (2.027) |
| `--no-proxy-server` | no change (2.017) |
| `--host-resolver-rules=MAP mullion.local 127.0.0.1` | no change - **and the rule never reached the browser**, see below |

The response-lifetime negative is worth keeping on its own: it confirms
behaviourally what `asset_responses_windows.go`'s comment previously only
assumed, that the runtime takes its own references at `PutResponse`.

**Two things the NetLog settled that no behavioural probe could.** First, proxy
auto-config was the leading suspect before the capture - Chromium carries a
literal `kDelayAfterNetworkChangesMs = 2000` and WPAD auto-detect is on for this
machine - and the capture killed it outright: the `wpad:80` lookups failed in 1-3
ms each, the whole `PAC_FILE_DECIDER` span was 10 ms, and the proxy resolved to
`DIRECT` well before the document was served. Second, the `--host-resolver-rules`
rule **never reached the browser**, and the capture says why: `127.0.0.1` appears
nowhere in it, and the browser command line it records reads
`--host-resolver-rules=MAP` with the mapping gone. The value was cut at its first
space before the browser wrote down its own command line, so what arrived was a
switch with no rule. Where it was cut is not in the capture, and it is not
obviously this library: `internal/webview2/loader_options_windows_test.go`
round-trips a space-containing two-switch string through
`Get`/`PutAdditionalBrowserArguments` and asserts byte equality.

The lesson that cost two runs survives the correction, and is the reason to state
it precisely: a behavioural probe built on a browser flag cannot distinguish "not
the cause" from "never applied". There is more than one way to land there. The
argument can be mangled before the browser sees it, as it was here, and
[`get_AdditionalBrowserArguments`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2environmentoptions)
documents that WebView2 also ignores switches it blocks or cannot parse without
saying which. Either way the null result is unreadable. A NetLog has neither
failure mode: it names the span or it proves the wait is not in the network
stack.

Upstream:
[WebView2Feedback #2381](https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381)
reports the same shape for `SetVirtualHostNameToFolderMapping` and attributes it
to the runtime waiting out a name-lookup timeout. The name lookup is what the
capture here confirms for the `WebResourceRequested` path as well; the timeout is
theirs, for the reason given above. It is tagged bug / priority-low / tracked
and has been open since 2022. The workaround offered there is a hosts-file entry,
which an application cannot ask of its users; `.localhost` needs nothing from the
machine. Note that the Microsoft engineer's advice on that issue - use
`*.example` - does not work, and has now been contradicted four times including
by the `.test` measurement above.

**The fix is applied, and what it cost is worth stating.** `defaultVirtualHost` is
now `mullion.localhost`. The swap failed six tests: five pinned the old default and
were ordinary updates, and the sixth was `TestNoNetworkListener`, the no-port
promise's guard (decisions/0002), which greps the tree for loopback literals and
read the "localhost" inside the new default as one. Teaching it the difference is
[decisions/0030](./decisions/0030-guard-exempts-the-virtual-host-name.md): the scan
drops the exact token `mullion.localhost` before matching, and only where the name
stands alone, so a bare `localhost` still fails and so do `mullion.localhost:443`,
`preview.mullion.localhost` and the trailing-dot FQDN form. The first version of
that rule keyed on a following port alone, and an adversarial pass refuted it
before it shipped: the request filter is registered for this origin exactly, so a
subdomain of the virtual host is never intercepted and goes to the network stack.
Twelve mutants were run against the shipped rule. The guard is now strict enough
that a comment naming the reserved TLD on its own fails the scan, which is why the
prose here and in `config.go` names it rather than spells it.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: point the August evidence link to the canonical verification records path.
