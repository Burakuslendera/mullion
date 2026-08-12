# Startup gates and watchdog

This topic moved verbatim from the end-to-end architecture reference in
[architecture.md](./architecture.md).

## Startup gates and watchdog

A WebView2 control can embed successfully, navigate successfully, report
navigation-completed successfully — and still paint nothing. That white window is the
single most common field failure of this architecture, and neither the OS nor the
runtime reports it. Two independent timers exist because of it.

**Show gate.** After the embed the window is not shown immediately: the host waits for
the frontend to call `shellReady()`, which maps to `Host.MarkFrontendShellReady()` and
posts the tagged show message, keeping the user from seeing an empty window while the
first document is still parsing. The wait is bounded by `Config.ShowTimeout`; when it
expires the host shows the window anyway and logs the reason. Shell readiness may
arrive while the embed pump is still running, before `Run` reaches the gate start.
That readiness is latched without attempting a pre-window show post; gate start then
arms the fallback and performs exactly one release attempt. Queue failure leaves the
fallback armed, and a queued show whose embed/visibility application fails reopens
and re-arms the gate. Fired and stopped timers detach immediately and retain their
originating Run token, including across sequential runs.

**Render watchdog.** Armed before `Navigate`, cancelled by `Host.MarkFrontendReady()` —
the frontend's `ready()` call, made only after it has actually rendered. Timer
identity is a lock-protected generation chosen before `time.AfterFunc`; even a
zero-duration callback cannot race its own identity assignment or impersonate a
later session. If `Config.RenderTimeout` elapses first, the host logs an error
carrying everything it knows:

```
phase=<last frontend phase>   asset=<last asset served>
asset_category=…   asset_status=…
document=<n>  stylesheet=<n>  script=<n>
last_bridge=<method:status>
```

The counts are what make the payload diagnostic rather than decorative.
`document=1, stylesheet=0, script=0` is an asset-serving failure — the stream lifetime
bug in [assets.md](./assets.md) produces exactly this shape. `document=0` is a navigation or filter failure.
Healthy counts with `phase` stuck early is a frontend fault. `last_bridge=unknown` means
no bridge call ever arrived — the injected shim never ran. One line separates four root
causes that all present as the same white rectangle. Both timeouts are configurable; a negative value disables the mechanism.

**Read the counts knowing what they count.** They are bucketed from the
`Content-Type` mullion answered with, not from the file name and not from
WebView2's resource context: `text/html` increments `document`, `text/css`
`stylesheet`, anything containing `javascript` `script`, and **everything else is
counted nowhere**. The content type in turn comes from the name (decisions/0031),
so the chain is name → type → bucket, and a name mullion cannot classify breaks
it at the first link.

Two consequences a reader chasing a blank window needs, and neither is obvious
from the line itself:

- **Healthy counts do not mean the assets arrived.** Images, fonts, `.json`,
  `.wasm` and anything served `application/octet-stream` fall in the unbucketed
  class. A frontend whose real payload is a WebAssembly module reports
  `script=0` while working perfectly, and reports `script=0` when the module
  404s too.
- **A successfully served asset in that class produces no log line at all.**
  `logAssetResponseDebug` skips it deliberately — one page load can pull dozens
  of images and fonts, and logging each buries the three lines that say whether
  the document, its stylesheets and its scripts arrived. The suppression applies
  only to responses under `400`. A *failed* request is always logged, at `WARN`
  for `4xx` and `ERROR` for `5xx`, whatever its type, so a missing font is
  visible even though a present one is not.

> Last updated: 2026-08-12 | Editor: OpenAI (GPT-5.6) | Change: move the startup-gate and render-watchdog reference out of the end-to-end architecture document.
