# Bridge protocol and origin boundary

**Status:** active

This document is the canonical reference for frontend-to-host bridge messages and their origin boundary.

## Bridge protocol

The frontend calls into Go over the WebView's web-message channel with one JSON
envelope, wrapped by the injected shim as a promise API:

```js
// request                      // response
{ id, method, args }            { id, ok: true,  result }
                                { id, ok: false, error }

window.mullion.invoke(method, ...args) // -> Promise<result>
```

A monotonic sequence supplies `id`, and a pending map keyed by `id` settles the matching
promise when the response arrives. That map and the message listener live on a single
object on `window`, so multiple injected scripts and frontend modules share one channel
instead of each installing a listener of its own.

Go-side dispatch splits the method namespace. A reserved set is answered by the library
and never reaches the application:

```
WindowShow   WindowHide   WindowClose   WindowMinimise   WindowToggleMaximise
WindowIsMaximised   WindowFrameState   WindowStartDrag   WindowStartResize
WindowShellReady   WindowReady   WindowPhase   WindowDiagnostic
```

The first nine are the window controls. The last four are the signals the injected
scripts send back: the show gate (`shellReady`), the render watchdog (`ready`), and
the frontend diagnostics (`phase`, `diagnostic`).

Everything else is handed to `Config.Bridge` as the raw request JSON; it returns
the raw response JSON, or `""` to stay silent. `Bridge` may be nil — trusted-origin
window controls still work before the application implements a bridge method.

## Issue #116 current disposition

This section—not the [issue body](https://github.com/Burakuslendera/mullion/issues/116)
or historical decision annexes—is the current record. The issue body remains dated
audit input; this closes its findings without creating a new decision.

### How to use this record

Do not reopen or reimplement absent a finding's observable tripwire. Record
an observed divergence in a separately triaged issue before changing this contract.

### 1. Origin comparison uses the source plan

**Current resolution.** `sourcePlan.messageSourceAllowed` and
`sourcePlan.messageSourceTrusted` gate dispatch through `canonicalOrigin.matches`;
`lowerASCII` folds ASCII only and rejects Unicode case-fold confusables.
**Evidence owner.** `host/source_plan.go`; `TestEmbeddedSourcePlanCanonicalizesEveryConsumer`
and `TestSourcePlanConsumersRejectUnicodeCaseFoldConfusables` in
`host/source_plan_test.go`; [WebMessage source](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2webmessagereceivedeventargs?view=webview2-1.0.4129.50#get_source).
**Non-action / proof ceiling.** No Chromium canonicalisation observation is an
admission rule; no second origin-comparison path is added.
**Reopen only if.** A source is trusted or admitted for a source-plan consumer or
`Config.Bridge` contrary to `canonicalOrigin.matches`; this excludes the separately
generation-bound `Host.errorSurfaceMessageAllowed` fallback.

### 2. Restricted-source replies are explicit, not uncorrelatable

**Current resolution.** `Host.errorSurfaceMessageAllowed` is source admission before
`handleWebMessage`; `errorSurfaceMethodAllowed` is only the seven-method list,
separately from `Config.Bridge`. An admitted non-empty-id call gets its normal reply;
every rejected call is silent.
**Evidence owner.** `host/errorsurface_windows.go`
(`Host.errorSurfaceMessageAllowed`), `host/webview_windows.go` (`webMessageCallback`),
`host/bridge_windows.go`, and `host/bridge.js` (awaited
`WindowFrameState` response); `TestBridgeRestrictedSourcePreservesWatchdogEvidenceAndWindowControls`
and `TestProductionCallbacksClaimKnownEmptyFallbackBeforePinAndRestrictItsBridge`;
[WebMessage source](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2webmessagereceivedeventargs?view=webview2-1.0.4129.50#get_source).
**Non-action / proof ceiling.** Do not suppress admitted replies to recreate
non-correlatability: `WindowFrameState` is awaited by injected frame-state logic.
This is dispatch behavior, not proof of a live document or desktop effect.
**Reopen only if.** A rejected call produces a reply, or an admitted claimed-fallback
call with a non-empty id does not receive its normal protocol reply.

### 3. Frame ingress remains absent and defaults closed

**Current resolution.** `Browser.registerEvents` has six root event registrations,
but its only WebMessage ingress is `ICoreWebView2::add_WebMessageReceived`; it has
no `ICoreWebView2Frame2::add_WebMessageReceived` ingress. Any future frame ingress
must reject every frame before the empty-source fallback predicate unless identity is
independently tied to its claimed generation.
**Evidence owner.** `internal/webview2/browser_events_windows.go`;
`TestRegisterEventsPairsEveryNumericalConsumerWithItsSemanticHandler` in
`internal/webview2/browser_windows_test.go`;
[root receipt](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.4129.50#add_webmessagereceived) and
[frame receipt](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2frame2?view=webview2-1.0.4129.50#add_webmessagereceived).
**Non-action / proof ceiling.** No live frame receipt is claimed or observed.
**Reopen only if.** Adding frame-receipt code itself triggers this review, or a frame
message is observed at the root callback.

### 4. External delimiters and reply delivery have separate boundaries

**Current resolution.**
- **External delimiter.** `isExternalBrowserSafe` rejects raw `"`, U+0020, and
  C0, DEL, or C1 (`logsafe.IsControl`) before either `routeNewWindow` or
  `noteNavigationCancelledObserved` reaches
  [`shellExecuteOpen`](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew);
  encoded forms are unchanged, an admitted HTTP(S) URI is handed off unchanged,
  and rejected targets report `target not admitted`.
- **Reply delivery.** `webMessageCallback` admits the source at dispatch before
  calling `Config.Bridge` and posts only its non-empty result. After the host invokes
  [`PostWebMessageAsJson`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.4129.50#postwebmessageasjson)
  or [`PostWebMessageAsString`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.4129.50#postwebmessageasstring),
  a navigation before asynchronous page delivery discards it. The contract does not
  bind a reply to the document admitted at dispatch.
**Evidence owner.**
- **External delimiter.** `host/systembrowser_windows.go`
  (`isExternalBrowserSafe`, `routeNewWindow`, `noteNavigationCancelledObserved`,
  `shellExecuteOpen`); `TestIsExternalBrowserSafe`,
  `TestSafeTargetsAreHandedToTheSystemBrowser`, and
  `TestShouldCancelNavigation`.
- **Reply delivery.** `host/webview_windows.go` (`webMessageCallback`) and
  `internal/webview2.(*ICoreWebView2).PostWebMessageAsString`.
**Non-action / proof ceiling.** No current producer path for raw delimiters is
established and live production remains unverified. A navigation committed re-entrantly
during `Config.Bridge` before the host invokes Post, and which document receives that
later Post, remain unverified; the official contract does not bind the reply to its
dispatch document. There is no reply epoch: absent cross-document delivery evidence,
it would conservatively drop replies after any observed start, including a start later
canceled, inventing availability semantics.
**Reopen only if.** A prohibited raw delimiter reaches `ShellExecuteW`, this
delimiter boundary decodes, rewrites, or rejects `%22`, `%20`, or encoded controls,
or an admitted URI mutates before handoff. Separately, reopen if a controlled A→B
trace shows B receives A's response, or upstream adds or changes its binding/delivery
contract. Observing timing without cross-document delivery only removes timing
uncertainty; it does not justify an epoch.

### 5. Fallback controls deliberately need no gesture

**Current resolution.** The claimed fallback admits exactly `WindowStartDrag`,
`WindowStartResize`, `WindowMinimise`, `WindowToggleMaximise`,
`WindowIsMaximised`, `WindowFrameState`, and `WindowClose`; `WindowStartDrag` and
`WindowStartResize` use the native move-size path. Gesture is not authority.
**Evidence owner.** `host/bridge_windows.go` (`handleWebMessage`,
`errorSurfaceMethodAllowed`) and `host/control_windows.go`;
`TestBridgeRestrictedSourcePreservesWatchdogEvidenceAndWindowControls` and
`TestProductionCallbacksClaimKnownEmptyFallbackBeforePinAndRestrictItsBridge`;
[WebMessage receipt](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.4129.50#add_webmessagereceived).
**Non-action / proof ceiling.** Do not add a gesture gate absent a new public
threat-model requirement and a reliable runtime signal; frontend pointer checks are
UI behavior, not host authority. This is not proof of physical input or a live
native-window effect.
**Reopen only if.** An unlisted method receives fallback control authority, a listed
method loses its documented native route, or a rejected method is dispatched.

Frontend-controlled method names, phases, diagnostic kinds/details and asset
labels are a separate projection of that raw protocol. Before the host logs or
retains one, it selects bounded input and emits at most 2,000 bytes. Selection
rejects over-budget, malformed-authority, malformed-userinfo, and parser-invalid
path candidates — including control bytes or bad escapes beyond the retained
projection — then continues to the first http(s) candidate the production
reducer can emit with its authority whole. Valid userinfo is stripped to the
credential-free host without input-sized allocation, and a fully validated path
is streamed into fixed output storage without splitting escapes or encoded
runes. Retained file names and reduced strings are detached from the request's
backing storage. This diagnostic boundary does **not** rewrite the raw request
passed to `Config.Bridge`
([decision 0035](./decisions/0035-frontend-diagnostics-are-bounded.md)).

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: correct issue #116 findings 1–3 source, fallback, and frame-ingress boundaries.
