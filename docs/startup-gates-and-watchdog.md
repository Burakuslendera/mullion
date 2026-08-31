# Startup gates and watchdog

This topic moved verbatim from the end-to-end architecture reference in
[architecture.md](./architecture.md).

## Startup gates and watchdog

A WebView2 control can embed successfully, navigate successfully, report
navigation-completed successfully — and still paint nothing. That white window is the
single most common field failure of this architecture, and neither the OS nor the
runtime reports it. Two independent timers exist because of it.

**Show gate.** On a normal (non-`StartHidden`) startup, the gate starts only
after `ensureWebView` returns successfully: the required
[document-created-script barrier](./webview2-and-assets.md#document-created-script-registration-barrier),
optional tab-strip attempt, watchdog arm, and first `Navigate` request have all
completed. A failed or cancelled required registration therefore starts neither
the gate nor visibility. The host then waits for `shellReady()` /
`Host.MarkFrontendShellReady()` to post the tagged show message, keeping the user
from seeing an empty window while the first document parses. The wait is bounded
by `Config.ShowTimeout`; expiry shows the window and logs the reason. Shell
readiness may arrive during the embed pump before `Run` starts the gate; it is
latched without a pre-window show post, then starts the fallback and exactly one
release attempt. Queue failure, failed show application, and stopped/fired timer
handling preserve the originating Run token across sequential runs. `StartHidden`
continues to defer startup until an explicit `Show`.

**Render watchdog.** Armed before `Navigate`, cancelled by `Host.MarkFrontendReady()` —
the frontend's `ready()` call, made only after it has actually rendered. Timer
identity is a lock-protected generation chosen before `time.AfterFunc`; even a
zero-duration callback cannot race its own identity assignment or impersonate a
later session. If `Config.RenderTimeout` elapses first, the host logs an error
carrying the observations it has retained:

```
phase=<last frontend phase>   asset=<last embedded asset response>
asset_category=…   asset_status=…
document=<n>  stylesheet=<n>  script=<n>
last_bridge=<method:status>
```

`document`, `stylesheet` and `script` are embedded-asset response buckets. They
are populated only by mullion's `WebResourceRequested` path; when `Config.URL`
selects a caller-served origin, that filter is not installed and the counters
provide no evidence about the caller's server. `last_bridge` is likewise narrow:
it is the last application method handed to `Config.Bridge`, with its latest
`received` or `completed` status. Reserved host methods such as window controls
and readiness do not update it.

Treat the payload as observed shapes, not as proof of a cause. In embedded mode,
`document=1, stylesheet=0, script=0` points toward the asset path and was the
shape observed for the stream-lifetime bug in [assets.md](./assets.md);
`document=0` points toward navigation or the asset filter before a document
response was recorded. Bucketed responses with `phase` stuck early point toward
frontend execution or rendering. `last_bridge=unknown` says only that no
application `Config.Bridge` call was recorded; it does not show that the
injected shim never ran. Confirm a cause with navigation status, frontend phase
updates, and the `asset response served` / `asset response error` logs. For
`Config.URL`, use navigation status, phase and application-bridge observations
together with the caller server's request and response logs. Both timeout values
are configurable.

**Read the embedded-mode counts knowing what they count.** They are bucketed
from the `Content-Type` mullion answered with, not from the file name and not
from WebView2's resource context: `text/html` increments `document`, `text/css`
`stylesheet`, anything containing `javascript` `script`, and **everything else is
counted nowhere**. The content type in turn comes from the name (decisions/0031),
so the chain is name → type → bucket, and a name mullion cannot classify breaks
it at the first link.

Two consequences a reader chasing a blank window needs, and neither is obvious
from the line itself:

- **Healthy bucket counts do not prove that every required asset arrived.**
  Images, fonts, `.json`, `.wasm` and anything served
  `application/octet-stream` fall in the unbucketed class. A frontend whose real
  payload is a WebAssembly module reports `script=0` while working perfectly,
  and reports `script=0` when the module 404s too.
- **A below-`400` response in that class produces no `asset response served`
  log line.** `logAssetResponseDebug` skips it deliberately — one page load can
  pull dozens of images and fonts, and logging each buries the three lines that
  show mullion's document, stylesheet and script responses. A *failed* request
  is always logged, at `WARN` for `4xx` and `ERROR` for `5xx`, whatever its type,
  so a missing font is visible even though a present one is not.

> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: state the exact normal-start show-gate boundary and its required-registration failure behavior.
