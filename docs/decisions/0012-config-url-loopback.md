# 0012. Config.URL lets a caller serve the frontend itself; mullion still opens no socket

**Status:** Accepted

## Context

[0002](./0002-no-local-port.md) makes the library open no listening socket: assets
come from an `fs.FS` served in-process over a synthetic virtual host. That is the
security property on the README's first screen, and it is guarded by
`TestNoNetworkListener`.

A recurring request is the opposite: let a consumer use a real local HTTP origin —
a dev server with hot reload and source maps, or a runtime that already speaks
HTTP. 0002's *What would change our mind* anticipated the pressure but framed it as
mullion growing HTTP semantics of its own (range requests, streaming). This request
is different and cheaper to satisfy: the consumer has full Go and can run their own
server in a few lines. The only thing missing was that mullion always navigated to
its own virtual host and offered no way to point it elsewhere.

The catch is the bridge. mullion injects `window.<ns>` — the window controls **and**
`Config.Bridge`, the application's own Go methods — into whatever it navigates to.
Point it at an arbitrary remote origin and that origin can drive the window and call
into Go across the network. So "navigate anywhere" is not safe; "navigate to a
server the user runs on their own machine" is.

## Decision

`Config.URL`, an opt-in that is empty by default. Empty keeps 0002 exactly as it
was: `fs.FS` over the virtual host, no socket. A non-empty `Config.URL` must use
`http`/`https` and name a **loopback** host — `127.0.0.1`, `localhost` or `::1`;
anything else is rejected by `Run`. mullion then navigates there and does **not**
serve `Assets` (which becomes optional). The injected scripts still run — they are
per-navigation and origin-independent — so the bridge and window controls work on
the caller's origin too.

**mullion still opens no socket.** The *caller* runs the server; mullion only points
the WebView at it. So 0002 is **not superseded** — it remains true — and this record
complements it. `TestNoNetworkListener` keeps forbidding the listener markers
(`net.Listen`, `http.ListenAndServe`, `http.Serve(`, `httptest`) in every file, and
loosens only enough to let `loopback.go` name the loopback hosts it exists to
*reject* non-loopback URLs against.

Every run logs which source is in effect, at INFO next to the version line, so a bug
report shows it without anyone asking: `asset source=embedded-fs …` or
`asset source=external-url, url=<scheme://host:port>` (path and query dropped).

## Alternatives rejected

**Open the port inside mullion (a `Config.Port`).** The literal reading of the
request, and the least work for the consumer. Rejected: it reintroduces everything
0002 removed — an attack surface reachable by any local process, a firewall prompt,
port collisions — and it destroys the guarantee for *everyone*, because the socket
code would then live in every binary and `TestNoNetworkListener` would have to go.
"mullion opens no socket, provably" is worth more than saving the consumer five
lines of `net/http`.

**Allow any URL, not just loopback.** Simpler validation, and it would enable remote
dev servers. Rejected: mullion injects `Config.Bridge` into the target, so a remote
origin becomes a path from the network into the application's Go. Loopback keeps that
surface on the machine the user already trusts. A genuine remote-URL need is a
further, louder opt-in, not the default of this one.

**Build-tag-gated port.** Keeps the default binary provably socket-free while letting
an opt-in build serve over a port. A real option, but heavier than needed: the
consumer-owned-server approach reaches the same place — a real local HTTP origin —
with mullion opening nothing at all, so there is no socket code to gate.

## Consequences

**When `Config.URL` is set, the asset boundary is the caller's, not ours.** 0002's
matrix — scheme/host/path-traversal rejection, `index.html` rewrite, `no-store` —
runs in `WebResourceRequested`, which only fires for the virtual host. A caller
serving their own origin owns those concerns themselves. This is stated in the field
doc and in `architecture.md`, because it is a real transfer of responsibility.

**The bridge reaches the caller's origin.** That is the point (their frontend calls
`window.<ns>`), and the loopback pin is what bounds it. If a caller ever serves
untrusted content from their own loopback origin, the bridge is exposed to it — their
call to make, and documented as such.

**A new triage axis.** A frame or asset bug now depends on whether the frontend came
from the embedded FS or a caller URL. `agents/issues.md` and `docs/verification.md`
add it to the environment a report must carry; the startup log line makes it
answerable from a paste.

**`TestNoNetworkListener` is one file looser.** The listener markers stay banned
everywhere. Only `loopback.go`/`loopback_test.go` may name the loopback hosts, and
only to reject non-loopback URLs. A future contributor who adds `net.Listen`
anywhere still fails the build.

## What would change our mind

- **A way to withhold the bridge from an external origin** — a per-origin gate on the
  injected scripts — would make a non-loopback `Config.URL` safe, and the loopback
  pin could relax to an explicit allow-list.
- **A caller with a legitimate non-loopback need** (a shared dev server on the LAN)
  is not served today; if that recurs, it is a separate, louder opt-in with its own
  record, not a quiet widening of this one.
- **mullion itself needing to serve HTTP** (range requests for seekable media) is
  still 0002's trigger, not this one, and would be answered inside
  `WebResourceRequested` rather than by opening a port.

## Evidence

- **Issue #3**: the request, the requirements (default off, loopback-only, recorded
  default, explanatory doc, triage rule, console output), and the maintainer's
  direction.
- `host/loopback.go`: `validateURL` (empty ok; http/https; loopback-only),
  `urlOrigin` (path/query dropped for logs), `assetSourceSummary` (the startup line).
- `host/config.go`: the `URL` field and `startURL` preferring it; `host_windows.go`:
  validation + the always-on log line + skipping the asset provider when URL is set;
  `webview_windows.go`: the filter and callback registered only when serving the
  embedded FS.
- `host/loopback_test.go`: loopback-only accepted, remote/LAN/wrong-scheme rejected,
  `startURL` selection, and that the log line drops the path.
- `host/leak_test.go`: `TestNoNetworkListener` re-scoped — listeners banned in every
  file, loopback hosts allowed only in `loopback.go`/`loopback_test.go`.
- 0002 stays `Accepted`: mullion opens no socket on either path.
