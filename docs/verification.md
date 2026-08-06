# Verification and Acceptance

## Contents

- [1. Automated gates](#1-automated-gates)
- [2. Why "it compiles" is not acceptance](#2-why-it-compiles-is-not-acceptance)
- [3. Manual acceptance checklist](#3-manual-acceptance-checklist)
- [4. Traps when scripting GUI checks](#4-traps-when-scripting-gui-checks)
- [5. Diagnostic build tags and env switches](#5-diagnostic-build-tags-and-env-switches)
- [6. What a good bug report contains](#6-what-a-good-bug-report-contains)

How a change to `mullion` is proved correct. The automated gates are cheap and
catch a narrow class of mistakes; the manual gates are the only thing that
catches the class of mistakes this library actually produces. Both are
mandatory before a frame, hit-test, DPI or WebView2 change is considered done.

## 1. Automated gates

Run in this order. Each one exists because it catches something the previous
one cannot.

```
gofmt -l .                                       # must print nothing
go build ./...                                   # windows build
go vet ./...                                     # syscall/unsafe/printf misuse
go test -count=1 ./...                           # unit + table tests, never cached
go test -count=1 -race ./...                     # message pump vs. callback races
node scripts/test-bridge.mjs                     # real bridge bytes in a Node VM
go run ./cmd/mullion doctor                      # execute runtime/export discovery
go build -tags mullion_dwm_caption_diag ./...; go test -count=1 -tags mullion_dwm_caption_diag ./...
go build -tags mullion_caption_passthrough_diag ./...; go test -count=1 -tags mullion_caption_passthrough_diag ./...
$env:GOOS = 'linux'; go build ./...; Remove-Item Env:GOOS # non-Windows stub gate (PowerShell)
pwsh scripts/leak-scan.ps1                       # nothing private is published
Push-Location examples/basic; go run .; Pop-Location # it actually starts
```

| Gate | What it catches |
| --- | --- |
| `gofmt -l .` | Formatting drift. Non-empty output is a failure, not a suggestion. |
| `go build ./...` | Compile errors on the default Windows path only. |
| `go vet ./...` | Misuse of `unsafe.Pointer` around Win32 calls, wrong printf verbs in log lines, suspicious struct tags. Vet is the closest thing to a static check on syscall boundaries. |
| `go test -count=1 ./...` | Uncached pure-logic invariants: hit-test region maths, non-client rect adjustment, DPI scaling, style-bit composition, asset name-to-MIME mapping, diagnostic log parsing — **and every COM vtable offset and IID in `internal/webview2`** (see below). CI also executes the architecture-tagged production gates as a real Windows/386 process under WOW64; Windows/ARM64 remains compile-only. |
| `TestNoNetworkListener` | The promise on the README's first screen: **no local port is ever opened.** It greps the source for `net.Listen`, `http.ListenAndServe`, `httptest` and loopback literals — with one exemption, the default virtual host name, and only where that name stands alone: `preview.mullion.localhost`, `mullion.localhost:443` and the trailing-dot FQDN form all still fail ([decisions/0030](./decisions/0030-guard-exempts-the-virtual-host-name.md)). Until this existed the claim was documentation and nothing else — the kind of invariant that decays quietly, when somebody reaches for a test server "just for a fixture" and the build stays green. See [decisions/0002](./decisions/0002-no-local-port.md). |
| `TestNoUpstreamBrandLeak`, `TestNoNonASCIIInSource` | The repository stays in one language, and carries nothing from the private code base it was extracted from. |
| `TestRunTokensAreProcessGlobalAcrossHosts` | Process-global non-zero Run identities keep a stale private command from one Host from being admitted by another Host after the OS recycles the same HWND. |
| `TestLoggerMayReenterHostMethodWhileTeardownWaits` | A Logger callback may re-enter `Quit` while teardown waits for the outer `Hide`; both admitted commands post and both calls complete instead of deadlocking on Run admission. |
| `go test -count=1 -race ./...` | Uncached data-race coverage between the UI thread and goroutines that touch shared state (asset serving, watchdogs, bound callbacks). |
| `go build -tags <diag>` | Diagnostic builds rot silently. Each diagnostic tag therefore gets its own build and uncached test in CI. |
| `$env:GOOS = 'linux'; go build ./...; Remove-Item Env:GOOS` | PowerShell-runnable proof that non-Windows stubs still satisfy the public API; this is compile portability, not window execution support. |
| `pwsh scripts/leak-scan.ps1` | Anything that must never be published: upstream product names, absolute local paths, artefact hashes, real-looking pseudo-versions, commit-trailer text inside a file — across tracked files **and commit messages**. CI runs it in the Windows job. |
| `node scripts/test-bridge.mjs`; `go run ./cmd/mullion doctor`; the example | Dependency-free VM coverage of the exact bridge bytes; direct, uncached execution of runtime discovery/export resolution; then a visible loaded window. These execution gates catch false greens that compilation or cached/removed tests cannot. |

### The COM ABI is pinned by tests, because the compiler cannot see it

`internal/webview2` is a hand-written COM binding, and a COM call is a jump through
slot *n* of a vtable. Go's type checker cannot tell a correct vtable struct from a
wrong one: both compile. A field inserted, dropped or transposed silently retargets
every method after it, and the result is not `E_NOINTERFACE` — it is a call through
the wrong function pointer, i.e. a crash or memory corruption inside the browser
process, at the point of first use. **A green build proves nothing about a vtable.**

The gate is therefore a test, and it must stay one:

- Every vtable's slot offsets are pinned with `unsafe.Offsetof`, every interface ID is
  pinned byte for byte, and the settings chain's total slot count (39) is asserted
  against the sum of its links. Change a struct in any of the `interfaces_*` files and the test
  tells you immediately; ship it untested and the user finds out.
- These tests need **no WebView2 runtime and no window** — they are assertions about
  struct layout — so they run in the same headless suite as everything else. Keep them
  there.
- The reference for any change is Microsoft's MIDL-generated `WebView2.h` / `WebView2.idl`
  from the SDK package. **The MS Learn reference pages list members alphabetically and
  must never be used to derive an ABI.**

### The test suite is headless — keep it that way

No test in this package creates an `HWND`, calls `Run()`, spins a message pump,
or requires a WebView2 Runtime to be installed. That is a deliberate design
constraint, not an accident: it means the full suite (including `-race` and
every diagnostic tag) runs on a headless CI worker with no desktop session, no
GPU and no WebView2 install.

Rules for new tests:

- Test the **pure function**, not the window. Hit-test decisions, rect maths,
  style-bit composition, DPI conversion and log-line parsing are all expressed
  as functions over plain structs precisely so they can be tested this way. If
  a new behaviour is hard to test without a window, that is a signal the logic
  should be extracted out of the window procedure.
- Never call a Win32 entry point from a test; tests exercise the callers of the
  Win32 wrappers, not the wrappers. **Including indirectly.** A test that drives
  a policy function has to be read down to the call it eventually reaches: the
  navigation-cancel gate hands an off-origin `http(s)` target to `ShellExecute`,
  so a gate test written with an `https://` URL opened a browser tab on every
  run of the suite, in CI as well (issue #76). Picking an input that stops short
  of the effect is not enough on its own — it has to be got right in every test
  that ever touches the policy, and it was not. Put the effect behind a seam and
  stub it in the test host, so the protection is the default: `Host.openExternal`
  is that seam, `newTestHost` stubs it, and a test that wants to assert the
  routing swaps in a recorder. That also turns a live-only behaviour into a
  headless lock, which is the second reason to prefer it.
- If a test needs a window to be meaningful, it belongs in the manual checklist
  below — and the code it would have tested belongs in a function that *can* be
  tested headlessly. A test that requires a desktop turns a green CI into a
  machine-dependent coin flip. Reject it in review.

## 2. Why "it compiles" is not acceptance

In this architecture the compiler is nearly blind to the failure modes that
matter. All of the following build cleanly, pass every unit test, and are
broken:

- The window opens and paints **white forever** — the frontend never loaded:
  the asset handler rejected the request, the synthetic origin was mismatched,
  or the WebView2 controller was created but never made visible.
- The window is **visible but dead to drag** — the title bar returns the wrong
  hit-test code, or the WebView2 child swallows the pointer before the parent
  sees it.
- **Resize borders show the right cursor but do not resize** — the cursor comes
  from one code path and the sizing from another; they can disagree.
- The **system menu opens with the wrong item states** — `Maximize` enabled
  while already maximized, `Restore` greyed out while maximized.
- Everything works until the window crosses onto a **monitor with a different
  scale factor**, where the non-client geometry is still computed at the old DPI.

None of these are compile errors, and none are unit-testable without a window.
Acceptance therefore means **live interaction with a running window**, by a human
or by a script that drives the native frame. Green build plus green tests is the
entry ticket to acceptance, not acceptance itself.

## 3. Manual acceptance checklist

Run `examples/basic` (or the host application) and walk the list. Every item is
a pass/fail with an observable result — "looks fine" is not a result.

- [ ] **Title bar drag.** Press in the title bar and move: the window follows
      the cursor immediately, with no dead zone and no snap-back.
- [ ] **Double-click title bar.** Toggles maximize, then toggles back.
- [ ] **Drag down from maximized.** Press in the title bar of a maximized
      window and drag downward: the window restores *under the cursor* and
      continues following it in the same gesture — it must not restore to a
      corner or drop the drag.
- [ ] **Resize: 4 edges and 4 corners.** For each of the eight zones, check
      **both** that the cursor changes to the correct shape *and* that dragging
      actually resizes in that direction. The cursor and the sizing come from
      different code paths; test them as two separate assertions.
- [ ] **Minimize** from the custom caption control, and restore from the
      taskbar.
- [ ] **Close** from the custom caption control; the process exits and no
      child process is left behind.
- [ ] **`window.open` / `target=_blank` → the system browser.** From the
      frontend, open an external `https://` link (a `target=_blank` anchor, or a
      scripted `window.open`): it opens in the default browser and **no second
      window appears** — a detached, chrome-less WebView2 popup is the failure.
      A non-http(s) scheme (`window.open('mailto:…')`) does nothing, and the log
      says `new window dropped, unsupported scheme` (decisions/0022).
- [ ] **The window still answers while the browser starts** (issue #74,
      decisions/0029). The user-visible half of that change; the timing half was
      measured with a probe and is in 0029's Evidence (230 ms on a launch that
      starts the browser, and the handler itself below the clock's resolution).
      Kill every process of the default browser so the next launch is a cold
      start, then click an external link and — without waiting for the browser —
      drag the title bar and press a caption button. Both must respond at once.
      Note that 230 ms is a narrow window to aim at by hand: catching it takes
      deliberate effort, and failing to catch it proves nothing either way, which
      is why the probe exists. The browser still opens, and `external open
      dropped` must not appear — that line means all eight in-flight slots were
      taken, which one click cannot do.
- [ ] **`Config.PinNavigationToOrigin` cancels off-origin top navigation**
      (opt-in — only when the field is set). With the gate on, a top-frame
      navigation to a foreign origin — an external `https://` link with no
      `target`, or a redirect off the trusted origin — is cancelled (the frontend
      stays put) and an http/https target opens in the system browser; the
      trusted origin, in-origin routing and the `data:` error surface are never
      cancelled. Verify a redirect specifically, and that the app's own startup
      navigation is not cancelled (decisions/0023).
- [ ] **The cancel was performed, not merely asked for**, in the same run as the
      item above. Each cancelled navigation logs `navigation cancelled off
      origin, routed to system browser` and then `cancelled navigation completed,
      status=14, id=<n>` at debug — the completion consumed, so the fallback
      error surface must not appear and the frontend must not be replaced. Four
      WARN lines say it went otherwise, and a good run has none of them:
      `cancelled navigation committed anyway` (the runtime accepted `put_Cancel`
      and navigated regardless, which is 0023's `unverified` premise failing),
      `cancelled navigation forgotten` (four cancels outstanding at once, so one
      of them reverts to the pre-issue-73 behaviour and its completion may arm
      the surface), `navigation cancelled off origin, target unreadable` (the URI
      getter failed and the gate cancelled a navigation it could not read), and
      `webview2 runtime warning, reason=cancel navigation <n>` (the runtime
      refused the cancel, so the foreign document loads — check that it is then
      **not** also opened in the system browser). Judge by the presence or
      absence of those lines, for the reason the next item gives about
      `SessionWarnCount` (decisions/0027).
- [ ] **An in-origin full navigation does not fall into the error surface.**
      Serving the embedded assets (no `Config.URL`), click a link that navigates
      the top frame to another in-origin document (`<a href="index.html?x=1">`)
      several times: every attempt must land on the frontend. A `navigation
      failed, status=9` followed by `showing fallback error surface` is the
      issue #72 loop; with the rule in place the completion is reported once, at
      debug, as `navigation aborted, not arming the error surface, status=9,
      id=<n>`, the frontend stays, and **no `navigation failed` line and no WARN**
      appears for it (decisions/0024, 0026). Judge that by the absence of WARN
      lines, not by `SessionWarnCount`, which is a snapshot taken at
      frontend-ready. The abort is a race that needs the click to land while the
      previous navigation is in flight: click fast, or every attempt commits.
      The clicked navigation is told apart from the app's own startup navigation
      by the trailing `?` on its `navigation starting` line
      (`uri=https://mullion.localhost/index.html?` against
      `uri=https://mullion.localhost/index.html`); the query *value* is dropped from
      the log (decisions/0025). Match it literally — `?` is a regex
      metacharacter, so an unescaped `index.html?` matches both lines. Use
      `Select-String -SimpleMatch` or `index\.html\?$`. If a check ever needs
      more than two navigations told apart, give them distinct paths rather than
      distinct queries.
- [ ] **A frontend error keeps the URL it names.** Make the frontend throw with
      a URL in the message — `throw new Error("could not load
      https://mullion.localhost/app/main.js")` from `app.js` is enough, or point a
      `<script src>` at a missing in-origin file. The ERROR line must read
      `frontend diagnostic error, message=…https://mullion.localhost/app/main.js`,
      with the host intact; `httpmain.js` is issue #80, the shape three releases
      of live verification were read past without anyone noticing
      (decisions/0028). A query in that URL must still be reduced to a bare `?`.
      Headless tests pin the reducer and its 2,000-byte bound, including a first
      URL after long context; only delivery of a real `window.onerror` string to
      this line is observable live (decisions/0035).
- [ ] **Window lifecycle reuse after failure and success.** First run a Host
      whose embed fails before loop entry, clear the cause, and call `Run` again
      on that same Host; close its painted window normally and call `Run` a
      third time on the same Host. Separately, reject a NUL-bearing title, then
      create a fresh Host with the corrected title and the same class name.
      Every supported retry must paint, with no stale `WM_QUIT`, "Class already
      exists", stale browser, or already-destroyed refusal (issues #48, #97).
- [ ] **Sequential-Run stale-control and early-readiness adversary.** Reuse one
      `Host` for two sessions and record both Run tokens. Queue every private
      window command from session N (`Show`, `Hide`, `Quit`, `Minimise`,
      maximise toggle, drag, resize, bounds sync and `SetTitle`), end N, and
      arrange for Windows to reuse the same numeric HWND in N+1. None may apply
      or add a warning/log line in N+1; fresh commands must still apply. During
      an embed pump, signal `shellReady()` before the show gate starts: no show
      may be posted before start, and exactly one tagged show must be posted
      after start. Reject that show's first embed/application attempt and confirm
      the fallback re-arms and retries rather than leaving the non-hidden session
      invisible.
- [ ] **Teardown ordering under admitted calls and firing callbacks.** Block an
      already-entered `MarkFrontendShellReady()` between its bounds and show
      effects while teardown attempts to finish. Teardown must wait; both effects
      retain N's token. Separately fire N's startup/render timers, deferred bounds
      callback and worker-warning path after N+1 starts with the recycled HWND:
      N+1 must receive no post, timeout/fallback line or warning. Block
      `IsMaximised()` inside its query and confirm `WM_DESTROY` cannot clear/reuse
      the HWND until the query returns.
      `TestRunTokensAreProcessGlobalAcrossHosts`,
      `TestPrivateCommandsRejectOldRunTokenAfterIdenticalHWNDReuse`,
      `TestExportedCommandsCarryEntryRunTokenAndPreserveWParamPayloads`,
      `TestLoggerMayReenterHostMethodWhileTeardownWaits`,
      `TestStartupShowGateLatchesEarlyReadinessUntilStart`,
      `TestStartupShowApplicationFailureRestoresFallbackAndRetries`,
      `TestOldRunTimersDeferredPostsAndWorkerWarningsStayOutOfNextRun`,
      `TestReadinessAdmittedBeforeTeardownCompletesInsideOriginatingRun` and
      `TestIsMaximisedPinsHWNDOwnershipUntilQueryReturns` exercise these through
      headless production dispatch/callback seams without creating a window.
      Remaining uncertainty is live Windows scheduling and actual numeric HWND
      recycling; repeat this checklist live because a headless seam cannot force
      the kernel's allocation timing.
- [ ] **`StartHidden` → first `Show`.** With `Config.StartHidden` set, no
      window may appear until `Show()` is called; the first `Show` embeds the
      WebView and the frontend paints. Quitting without ever showing must
      still exit cleanly with no browser process left behind. (A `Show()` or
      `Quit()` landing mid-embed is timing-dependent and stays a live-only
      scenario; the refusal and cancel logic is pinned headless — see
      decisions/0016.)
- [ ] **First frame position and size.** The window opens centered in the
      primary monitor's work area, identical across launches, and its physical
      size is `Config.Width x Height` times the monitor's scale (e.g. 980x640
      at 125% → 1225x800; the Debug-level startup log line
      `mullion: initial placement` states the applied numbers). On a scaled monitor an unscaled window is
      the issue #59 regression (decisions/0018).
- [ ] **Time the startup navigation gap** (issues #85, #77 - fixed, and this is
      the check that it stays fixed). No interaction is needed - the app's own
      startup navigation shows it. With the Logger at debug level
      (`examples/basic` already is), read two lines:
      `asset response served, status=200, ... asset=index.html` (T0) and
      `asset response served, status=200, ... asset=style.css` (T2). **T2 minus
      T0 is the gap** - document to first subresource. Do not end the window at
      `frontend diagnostic phase, phase=document created`: that line is stamped
      when the host receives a bridge message from the injected script, so it
      lags by a few milliseconds and now lands *after* T2. Time the **startup**
      navigation: on the old default a click landed inside a retry chain the
      runtime drove itself (one click started 45 navigations, most aborted) and
      was not comparable, which no longer happens - 31 clicked navigations
      measured 10-18 ms in the same session, all committing. The old gap ran
      2.026 - 2.041 s, named by a NetLog capture as a `HOST_RESOLVER_MANAGER_JOB`
      for the virtual host (webview2-and-assets.md). The default is now under the
      TLD RFC 6761 reserves for loopback (decisions/0030), so the gap must read in
      the tens of milliseconds and in-origin navigations must commit. Run it twice
      from the same launcher and read the second: this package points WebView2 at
      a profile named after the executable, so an IDE-built binary starts on a
      fresh one and its first navigation measured 1633 ms against 15 ms warm.
      Five runs on 2026-07-28 (runtime 150.0.4078.99) measured 47-141 ms, the 141
      on the session's first run; two earlier readings, 11-79 and 11-22 ms, did
      not reproduce and are superseded. Upstream, unfixed:
      https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381
- [ ] **Right-click the title bar → system menu appears**, and its item states
      are correct **in both window states**:
      restored → `Restore` disabled, `Maximize` enabled, `Move`/`Size` enabled;
      maximized → `Restore` enabled, `Maximize` disabled, `Move`/`Size`
      disabled. This is the single most fragile item on the list: it breaks
      whenever style bits or the non-client path change, and it breaks silently
      because the menu still *opens*. Check it every time.
- [ ] **`Win`+`←` / `Win`+`→` snap.** The window snaps to the half-screen work
      area (not the full monitor rect — the taskbar must still be visible), and
      snapping back out restores the previous size.
- [ ] **Mixed-DPI monitor transition.** With two monitors at different scale
      factors, drag the window across the boundary. The title bar height,
      caption controls, resize borders and frontend text must all rescale, and
      the client area must not be a stretched bitmap. Then maximize on each
      monitor and confirm the client area fills the work area exactly, with no
      title bar clipped off the top.
- [ ] **Auto-hide taskbar reveal while maximized.** Set the taskbar to auto-hide,
      maximize the window, then push the mouse into the auto-hide edge. The taskbar
      must still pop up (a 1px sliver is reserved for it — docs/decisions/0015).
      Verify on the primary monitor and, with the taskbar's monitor changed, on a
      secondary one. A window that covers the whole monitor and suppresses the
      reveal is a failure.
- [ ] **Hit-test trace.** Relaunch with `MULLION_HITTEST_DIAG=1` and repeat the
      drag / resize / caption-button passes. The emitted hit-test lines must
      show the expected code for each region (caption over the drag strip,
      the eight sizing codes over the borders and corners, client over the
      frontend). A visually correct window with wrong hit-test codes is a
      latent bug, and this is how you see it.
- [ ] **Tooltip trace** (when touching caption-control tooltips): relaunch with
      `MULLION_TOOLTIP_TRACE=1` and confirm show/hide events pair up and no
      tooltip is orphaned after the pointer leaves the window.

If a change touches the frame, DPI or hit-test code, the whole list is re-run.
There is no "small frame change".

## 4. Traps when scripting GUI checks

The GUI scripting failure modes and rules moved verbatim to
[gui-verification-traps.md](./gui-verification-traps.md) when this file reached
its 400-line limit.

## 5. Diagnostic build tags and env switches

Diagnostics exist because the frame bugs in this library are *invisible* — the
window looks right and behaves wrong. Each switch trades a little runtime cost
or a little behaviour for a lot of visibility.

| Switch | Kind | What it does | When to turn it on |
| --- | --- | --- | --- |
| `mullion_dwm_caption_diag` | build tag | Builds an alternative caption/DWM extension path and logs the frame decisions it makes, so the default path can be compared side by side against it. | Double title bar, missing or extra shadow, wrong rounded corners, native caption leaking during startup, maximize glyph flicker. |
| `mullion_caption_passthrough_diag` | build tag | Builds a variant of the caption hit-test/passthrough behaviour and traces which component claims each caption-area point. | Drag works but caption buttons do not (or the reverse), snap layouts flyout does not appear on hover, hover state stuck after the pointer leaves. |
| `MULLION_HITTEST_DIAG=1` | env | Emits one line per hit-test decision: point, region, returned code. | Any drag/resize/cursor complaint; mandatory when changing hit-test geometry. |
| `MULLION_TOOLTIP_TRACE=1` | env | Traces caption-control tooltip show/hide/lifetime. | Tooltips that stick, never appear, or appear on the wrong control. |

Rules:

- A diagnostic tag is a **diagnostic**, never a release configuration. Ship the
  default path; use tags to find out why the default path is wrong.
- **Diagnostic builds must be compiled in CI.** `go build ./...` does not touch
  a single file behind a build tag, so a diagnostic variant can be broken by an
  unrelated rename and stay broken until the day you need it — which is
  precisely the day you cannot afford to fix it first. Every tag gets a
  `go build -tags <tag> ./...` line in the gate list above. The same holds for
  tests: a tag that has its own test files needs
  `go test -tags <tag> ./...` too.
- Env switches must default to **off** and must not change behaviour when on —
  only logging. If enabling a diagnostic makes the bug disappear, the
  diagnostic is not read-only and is itself a bug.

## 6. What a good bug report contains

The environment a frame bug report needs and the reporting contract live in
[bug-reports.md](./bug-reports.md); they moved when this file reached its limit.

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: define issue #97's live lifecycle retries after bad-title, pre-loop-failure and normal-destruction paths; and update the frontend-error item for issue #88's 2,000-byte diagnostic bound, whose first-complete-URL behavior is headless-tested while real window.onerror delivery remains live-only (decision 0035).

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: add uncached runtime checks, direct doctor/export and bridge VM smoke, and real Windows/386 WOW64 architecture-gate execution while keeping ARM64 compile-only.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: add issue #97 adversarial sequential-Run verification for tagged private commands, early shell-ready retry, firing timers/deferred workers, already-admitted readiness ordering and HWND-pinned synchronous queries; state the live HWND-recycling uncertainty.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: name the two headless lifecycle locks for process-global Run tokens and Logger re-entry while teardown waits, without changing their mechanism claims.

## 7. 2026-08-06 post-merge blocker closure audit

- **Automated:** formatting, build, vet, `-unsafeptr`, uncached full tests, both diagnostic-tag build/test pairs, bridge VM, leak scan, Windows/386 production gates, and Linux/amd64 plus Windows/386/ARM64 builds passed. GitHub Actions run `31060250331` passed all four Go 1.24/stable Windows/portable jobs; both Windows jobs passed `go test -count=1 -race ./...`, including the render-watchdog generation repair exposed by the preceding CI run.
- **A/B:** restoring the teardown wait, pre-gate DPI call, eager backdrop callback, post-reduction URL displacement, disconnected production message callback, or the render watchdog's callback-before-identity assignment made its named regression or race check fail; each repaired version passed.
- **Live:** `mullion doctor` found WebView2 151.0.4129.59; `examples/basic` reached shell-ready, window-visible, application `Ping`, navigation-completed and frontend-ready with zero session warnings/errors on the two-monitor 125%/100% setup.
- **Not covered:** the final smoke was process-stopped after readiness rather than closed through the UI; the manual snap/resize/DPI checklist was not repeated. Local `go test -race ./...` could not build because `gcc` is absent; the two Windows CI race lanes passed instead.
- **`unverified`:** Windows/ARM64 remains compile-only by decision 0034, and physical HWND-value recycling was not forced live; the headless token/HWND adversaries cover both identity halves.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: record the green four-job Go 1.24/stable CI run, both passing Windows race lanes, the render-watchdog race found by CI and repaired with pre-callback generation identity, and the remaining live-check gaps.