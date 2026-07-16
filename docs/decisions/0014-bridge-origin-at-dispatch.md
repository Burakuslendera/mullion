# 0014. The injected bridge acts only on messages from the trusted origin

**Status:** Accepted

## Context

`window.<ns>` — the window controls and `Config.Bridge`, the embedding
application's own Go methods — is injected into every document with
`AddScriptToExecuteOnDocumentCreated`, which is origin-independent: it runs in
whatever the WebView loads. The only origin containment mullion had was
`validateURL` (decisions/0012), and that runs once, against the initial
`Config.URL`.

Nothing constrained where the top-level document could go afterwards. A frontend
that navigates the top frame to a remote origin — an external link with no
`target`, an OAuth or open-redirect on a trusted link, `window.location = …`, or
a server redirect — would have the bridge re-injected into that origin, and a web
message posted from there reached `handleWebMessage` and `Config.Bridge` with no
origin check. A remote, network-controlled origin could then call into the
application's native Go. The `WebResourceRequested` filter (`origin+"/*"`) does
not help: it governs what the embedded `fs.FS` answers, not where the document may
navigate.

`ICoreWebView2WebMessageReceivedEventArgs.GetSource()` already reports the URI of
the document that posted a message. It was read and discarded.

## Decision

The host enforces the source origin at message dispatch. The `WebMessageReceived`
handler threads `args.GetSource()` into `MessageCallback`, and
`messageSourceAllowed` is an **allow-list**: a message passes only when its source
is the same http/https origin as the trusted one — the virtual host
(`https://<VirtualHost>`) in asset mode, or the `Config.URL` origin when set,
compared with the default port normalised and the host case-insensitive — or is the
`data:` error surface. Everything else is dropped silently and logged; no reply is
posted, so a foreign origin gets nothing to correlate.

An allow-list, not a deny-list, because the schemes that are *not* a concrete
http/https origin are not all harmless: `blob:` and `filesystem:` documents carry
the full web origin that created them (`blob:https://evil…`), and `about:blank`
inherits the previous document's origin, so a foreign origin the top frame was
steered to could otherwise launder its post through one of those. Admitting `data:`
is the one safe exception: only mullion itself can put a `data:` document in the top
frame, because browsers block a script-driven top navigation to a `data:` URL.

## Alternatives rejected

- **Cancel the navigation with a `NavigationStarting` handler** (pin the top
  document to the trusted origin). Stronger — the WebView never loads the foreign
  origin — and it remains the right defense-in-depth follow-up. It was not done
  first because it needs new COM bindings (`AddNavigationStarting` /
  `AddNewWindowRequested` and their event args), whose vtable offsets cannot be
  verified headless, and because a navigation the app legitimately intends (an
  OAuth flow) must then be routed to the system browser rather than just blocked.
  Enforcing at dispatch defeats the same attack with no new COM surface.
- **A per-origin gate on the injected script** (inject `window.<ns>` only on the
  trusted origin). `AddScriptToExecuteOnDocumentCreated` has no origin filter, so
  this would mean injecting a guard prologue that no-ops off-origin — the same
  check, in a place harder to test than a pure Go function.
- **Documentation only** ("do not navigate the top frame off-origin"). An
  invariant a library cannot enforce is one consumers break silently; that class
  is exactly what this library is built to catch.

## Consequences

The bridge is inert on any origin the frontend is navigated to that is not the
trusted one: a message from there is dropped. An application that deliberately
serves its UI from more than one origin over the same WebView must point
`Config.URL` at the origin it wants trusted, or keep to one origin — only a single
origin is trusted at a time. The check is a string comparison on every web
message, negligible next to the round trip it guards.

This does not stop the foreign origin from *loading* (that is the
`NavigationStarting` follow-up), nor does it gate `window.open` / new windows
(`NewWindowRequested`); it stops the foreign origin from *acting through the
bridge*, which is the reaches-into-Go half of the threat.

The `data:` exception is scoped. A `data:` source is admitted so the error page's
caption buttons work, but it is **not** trusted for `Config.Bridge`: only mullion
can put a `data:` document in the *top* frame, but a script can create a `data:`
*iframe*, and the bridge is injected into every frame — so a `data:` message may
be a hostile iframe rather than the error surface. `messageSourceTrusted` gates
the `Config.Bridge` hand-off, so a `data:` source reaches the reserved window
controls only, never the application's own Go methods.

## What would change our mind

- A WebView2 API that scopes an injected script to an origin natively would let
  the bridge simply not exist off-origin, making the dispatch check redundant.
- A requirement to trust more than one origin at once would replace the single
  `trustedOrigin` with an allow-list.

## Evidence

`host/loopback.go` (`messageSourceAllowed`, `trustedOrigin`), the dispatch check
in `host/webview_windows.go`, and the source threaded through
`internal/webview2/browser_windows.go`. `TestMessageSourceAllowed`
(`host/loopback_test.go`) locks the allow/reject matrix. Issue #6 carries the full
analysis and the reproduction reasoning.

> Last updated: 2026-07-16 | Editor: Claude (Opus 4.8) | Change: new record for the bridge-origin dispatch gate.
