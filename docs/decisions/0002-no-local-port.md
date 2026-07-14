# 0002. Assets are served over an in-process virtual host, never a local port

**Status:** Accepted

## Context

The first asset pipeline was an HTTP server bound to the loopback address on an
OS-assigned random port, with the WebView navigating to it. It worked, and it is
what a great many embedded-browser samples do.

It also put a listening socket on the user's machine for a desktop application
that has no reason to have one: an endpoint reachable by any other local process,
a firewall prompt on first run, a port to collide with when two instances run,
and a question that has to be answered for every security review the product will
ever face. See [lessons-and-dead-ends.md](../lessons-and-dead-ends.md) section 8.

## Decision

Assets come from an `fs.FS` and are served in-process. The host registers
`AddWebResourceRequestedFilter(origin+"/*", COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)`
and answers every request in the `WebResourceRequested` callback, wrapping the
body in a COM `IStream`. The origin is synthetic (`https://` + `Config.VirtualHost`).
Nothing binds a socket, and the request never leaves the process.

## Alternatives rejected

**A loopback HTTP server.** Every web toolchain speaks HTTP, so relative URLs,
`fetch`, source maps and dev tooling work with no thought at all, and the code is
half the size. The cost is the listening socket above - pure attack surface for a
capability the application never needed - plus port collisions and a firewall
prompt. It is a good trade for a dev server and a bad one for a shipped desktop
app.

**`file://`.** No server and no port either. But a file origin is opaque: ES
modules, `fetch` and workers all run into CORS and loading restrictions that have
to be worked around one at a time, and the workarounds are worse than the
interception.

## Consequences

**We are the origin server, so we own the boundary.** The filter is a pattern, not
a trust boundary: WebView2 hands the callback whatever matches it. Scheme, host
and path traversal are therefore checked in the callback, and the traversal check
runs on the raw segments *before* any cleaning, so normalisation cannot launder a
rejected path. The full matrix - 400 unparsable, 403 wrong scheme / wrong host /
traversal, 404 missing, 500 read failure, `/` rewritten to `index.html` - is ours
to keep correct, permanently.

Responses carry `Cache-Control: no-store`, because the origin is identical across
builds and the WebView would otherwise replay an old build's asset into a new one.

What an HTTP server would have given for free is simply absent: no range
requests, no conditional GET, no streaming semantics. And the callback owns the
COM stream lifetime, which is a refcounting problem, not an I/O one.

## What would change our mind

A consumer needs real HTTP semantics that the virtual host cannot cheaply fake -
byte-range requests for seekable media, or a streamed response body - and
implementing them inside `WebResourceRequested` costs more than a loopback
server's security cost. That is a measurable trade, not a matter of taste: the
day a range request has to be honoured, this record is due for review.

## Evidence

- `assets_windows.go`: `resolveAssetRequest` (scheme, host, traversal, index
  rewrite) and the response construction with `Cache-Control: no-store`.
- `assets_windows_test.go`: the boundary matrix - root maps to `index.html`,
  wrong host 403, wrong scheme 403, `../` and `%2e%2e/` traversal 403, missing
  404, read failure 500, `no-store` asserted on every response.
- `docs/architecture.md`, "Asset serving without a port": the same table, plus
  the COM stream-lifetime rules that make it work.
- No `net.Listen` and no `http.Server` exists anywhere in the host package; the
  grep for them is part of the verification checklist and must stay empty.
