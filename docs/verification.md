# Verification and Acceptance

## Contents

- [1. Automated gates](#1-automated-gates)
- [2. Why "it compiles" is not acceptance](#2-why-it-compiles-is-not-acceptance)
- [3. Manual acceptance checklist](#3-manual-acceptance-checklist)
- [4. Traps when scripting GUI checks](#4-traps-when-scripting-gui-checks)
- [5. Diagnostic build tags and env switches](#5-diagnostic-build-tags-and-env-switches)
- [6. What a good bug report contains](#6-what-a-good-bug-report-contains)
- [7. 2026-08 verification records](#7-2026-08-verification-records)

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
pwsh scripts/leak-scan.ps1                       # configured publication shapes in the reported Git scope
Push-Location examples/basic; go run .; Pop-Location # it actually starts
```

| Gate | What it catches |
| --- | --- |
| `gofmt -l .` | Formatting drift. Non-empty output is a failure, not a suggestion. |
| `go build ./...` | Compile errors on the default Windows path only. |
| `go vet ./...` | Misuse of `unsafe.Pointer` around Win32 calls, wrong printf verbs in log lines, suspicious struct tags. Vet is the closest thing to a static check on syscall boundaries. |
| `go test -count=1 ./...` | Uncached pure-logic invariants, including issue #112's exact `int64` ceiling scaling over signed inputs/maximum DPI and zero-DPI fallback; preservation of positive `MaxInt32` Config metrics; invalid, outside-endpoint and full-signed-span half-open rects; clipped wrap bands and caption-button thirds; independent midpoint resize saturation/non-overlap; all corner/edge, controls, caption, client and maximized results; active-profile and in-process/no-shell maximized behavior; and bounded/invalid diagnostics from the production geometry constructor. It also covers non-client rect adjustment, style-bit composition, asset name-to-MIME mapping, diagnostic log parsing, and **every COM vtable offset and IID in `internal/webview2`** (see below). CI executes architecture-tagged production gates as a real Windows/386 process under WOW64; Windows/ARM64 remains compile-only. |
| `TestNoNetworkListener` | A fail-closed syntactic guard for the README's no-port promise. It parses every Go file regardless of build tag; resolves prohibited APIs, all supported Winsock loaders, named/unnamed conversions, local/cross-file package type aliases and parenthesized string conversions; and scans Go strings plus shipped raw text for bounded standalone/scheme/scheme-relative authority endpoints with IPv6 path/userinfo controls. The real named guard runs temporary modules. Exact fixtures and the case-folded intercepted host have rule-specific exceptions ([decisions/0030](./decisions/0030-guard-exempts-the-virtual-host-name.md)). Comments, run-time assembly, reflection, raw numeric syscalls and dependencies remain outside this source proof. |
| `TestNoUpstreamBrandLeak`, `TestNoNonASCIIInSource` | The repository stays in one language, and carries nothing from the private code base it was extracted from. |
| `TestRunTokensAreProcessGlobalAcrossHosts` | Process-global non-zero Run identities keep a stale private command from one Host from being admitted by another Host after the OS recycles the same HWND. |
| `TestLoggerMayReenterHostMethodWhileTeardownWaits` | A Logger callback may re-enter `Quit` while teardown waits for the outer `Hide`; both admitted commands post and both calls complete instead of deadlocking on Run admission. |
| `go test -count=1 -race ./...` | Uncached data-race coverage between the UI thread and goroutines that touch shared state (asset serving, watchdogs, bound callbacks). |
| `go build -tags <diag>` | Diagnostic builds rot silently. Each diagnostic tag therefore gets its own build and uncached test in CI. |
| `$env:GOOS = 'linux'; go build ./...; Remove-Item Env:GOOS` | PowerShell-runnable proof that non-Windows stubs still satisfy the public API; this is compile portability, not window execution support. |
| `pwsh scripts/leak-scan.ps1` | A fail-closed configured-shape scan of every strictly decoded Git-tracked text file and real-object commit message reachable from a validated, non-shallow `HEAD`. It rejects redirected source/index, replacement refs and grafts; reports file/commit/binary/allowance/error counts; and is wired after exactly one pinned current-source checkout. Untracked/binary/unreachable/obfuscated inputs and unnamed secret classes stay outside the verdict. See [guard-verification.md](./guard-verification.md). |
| `node scripts/test-bridge.mjs`; `go run ./cmd/mullion doctor`; the example | Dependency-free VM coverage of the exact bridge bytes; direct, uncached execution of runtime discovery/export resolution; then a visible loaded window. These execution gates catch false greens that compilation or cached/removed tests cannot. |

### The COM ABI is pinned by tests, because the compiler cannot see it

`internal/webview2` is a hand-written COM binding, and a COM call is a jump through
slot *n* of a vtable. Go's type checker cannot tell a correct vtable struct from a
wrong one: both compile. A field inserted, dropped or transposed silently retargets
every method after it, and the result is not `E_NOINTERFACE` — it is a call through
the wrong function pointer, i.e. a crash or memory corruption inside the browser
process, at the point of first use. **A green build proves nothing about a vtable.**

The default gate stays headless and deterministic:
- Local ABI manifests pin runtime-owned vtable slots/sizes and IID literals, plus
  Go-owned object representation, vtable layout, identity and numeric dispatch.
- `TestCOMABIInventoryCompleteness` alarms on unclassified production vtables,
  COM object representations and GUID literals; classification is not proof.
- Separate consumer-mapping tests pin the semantic method and concrete interface
  selected by production call sites. Reference-handoff tests pin ownership when
  a COM pointer crosses a callback or result boundary.
- The authority is Microsoft.Web.WebView2 NuGet `1.0.4129.50`:
  `build/native/include/WebView2.h` for C vtables and root `WebView2.idl` for
  UUIDs/declaration order. Alphabetised Learn pages are not ABI evidence.
- These headless contracts need neither runtime nor window and do **not** replace §3's live cursor, drag, maximize or mixed-monitor checks.

### The test suite is headless — keep it that way

No test creates an `HWND`, enters native COM, calls a Win32 entry point, spins a
message pump, or requires a display or WebView2 Runtime by default. Public
`Run()` is permitted only when a deterministic seam or build path proves return
before runtime discovery and every native boundary, with assertions that
forbidden seams were not reached. An early-return input alone is not proof (0039).

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

Issue #96's `SHCreateMemStream` pointer-lifetime repair cannot be locked by a
runtime test because it would have to cross a Win32 boundary, which is forbidden
above. `TestNewMemoryStreamPinsContentAtSyscallBoundary` instead compiles the
package with escape diagnostics and locks the compiler-recognised
`uintptrescapes` contract: the slice backing array must remain pinned through the
synchronous `LazyProc.Call`.
`TestSHCreateMemStreamSizeRejectsValuesOutsideUINT` locks the native 32-bit
length boundary with scalar values, so its over-limit case allocates no
multi-gigabyte slice and reaches no Win32 entry point (issue #120).

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

- [ ] **Temporary issue #112 large-metric fixture, before and in addition to the normal checklist.** Use an odd physical restored window rect `[l,r) x [t,b)`;
      record `w`, `h`, `mx=l+floor(w/2)` and `my=t+floor(h/2)`. Run three temporary Config passes, keeping the other metrics ordinary:
      (A) `HitTestTitlebarHeight=1_500_000_000`; (B) `HitTestCaptionControlsWidth=1_500_000_000`; (C) `ResizeBorder=1_500_000_000`.
      At both 96 and 192 DPI, A must drag and trace `HTCAPTION` at an interior point outside controls/resize; B must trace `HTCLIENT` throughout the non-resize title width and HTML caption controls must remain clickable.
      In restored C, require `(l,t)=HTTOPLEFT`, `(mx,t)=HTTOP`, `(r-1,t)=HTTOPRIGHT`, `(l,my)=HTLEFT`, `(r-1,my)=HTRIGHT`,
      `(l,b-1)=HTBOTTOMLEFT`, `(mx,b-1)=HTBOTTOM`, `(r-1,b-1)=HTBOTTOMRIGHT`; each cursor must have the matching shape and each drag must resize in that direction.
      Maximize C: none of those eight points may return a resize code or resize the window. Move each pass between the 96/192-DPI monitors: ordinary companion metrics must ceiling-scale, huge metrics must remain clipped/saturated to the current rect, no region may wrap or disappear, and valid traces must report matching effective `side_border`, `top_border`, `titlebar_height` and `controls_width`.
      Remove the fixture and run **every** normal checklist item below; it is not a substitute for the normal configuration or live checks.
- [ ] **Normal title bar.** Drag follows immediately; double-click toggles maximize and restore.
- [ ] **Normal drag down from maximized.** The window restores under the cursor and continues following in the same gesture, without jumping to a corner or dropping the drag.
- [ ] **Normal resize: 4 edges and 4 corners.** In all eight zones, separately verify the correct cursor and an actual drag resize in that direction; cursor and sizing use different paths.
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
      `cancelled navigation forgotten` (one of two independent four-entry ledgers
      overflowed: exact non-zero ids or anonymous known-zero/unavailable credits,
      so an evicted completion may arm the surface), `navigation cancelled off origin, target unreadable` (the URI
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
- [ ] **Mixed-DPI monitor transition.** Across different scale factors, the
      title bar, controls, borders and frontend text rescale without a stretched
      client bitmap; maximized on each monitor, the client fills the work area
      exactly and the title bar is not clipped.
- [ ] **Auto-hide taskbar reveal while maximized.** Set the taskbar to auto-hide,
      maximize the window, then push the mouse into the auto-hide edge. The taskbar
      must still pop up (a 1px sliver is reserved for it — docs/decisions/0015).
      Verify on the primary monitor and, with the taskbar's monitor changed, on a
      secondary one. A window that covers the whole monitor and suppresses the
      reveal is a failure.
- [ ] **Hit-test trace.** With `MULLION_HITTEST_DIAG=1`, repeat drag, resize and
      caption-button checks; require caption, all eight sizing codes and client
      in their expected regions. Correct visuals with wrong codes still fail.
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

## 7. 2026-08 verification records
- **Earlier P0 automated/A/B:** formatting, build, vet, `-unsafeptr`, uncached tests, diagnostic tags, bridge VM, leak scan, Windows/386 gates and portable builds passed; GitHub Actions `31060250331` passed Go 1.24/stable and both race lanes. Restoring the teardown wait, pre-gate DPI call, eager backdrop callback, post-reduction URL displacement, disconnected message callback or pre-fix watchdog ordering made its named check fail.
- **Earlier P0 live:** WebView2 151.0.4129.59 reached shell-ready, visible, `Ping`, navigation-completed and frontend-ready with zero warnings/errors on 125%/100% monitors.
- **Earlier P0 not covered / `unverified`:** the process was stopped rather than UI-closed; snap/resize/DPI was not repeated; local race lacked `gcc` but CI covered it; Windows/ARM64 stayed compile-only and physical `HWND` recycling was not forced.
- **Issues #96/#120 automated/A/B:** Go 1.24/current focused and full gates, diagnostic tags, bridge VM, portable builds and leak scan passed; Actions `31101143512` passed all jobs/race lanes. Reintroducing the precomputed `uintptr` or removing the `UINT` bound made its named regression fail.
- **Issues #96/#120 live:** WebView2 151.0.4129.59 served three assets and reached `Ping`, navigation and readiness with zero warnings/errors; a capture showed the rendered restored window.
- **Issues #96/#120 not covered / `unverified`:** local race lacked `gcc`; the demo was process-stopped; frame checks were unchanged; a real 4-GiB asset was not allocated, so only the scalar Win32 boundary was exercised.
- **Issues #86/#104/#110 and audit follow-up automated:** current Go and Go 1.24 formatting, build, vet and uncached suites passed; the focused navigation-diagnostic and COM consumer-boundary tests, both diagnostic-tag build/test pairs, bridge VM, WebView2 doctor, Linux/amd64 and Windows/amd64/386/ARM64 gates, and the 269-file leak scan passed.
- **Issues #86/#104/#110 and audit follow-up A/B:** the two navigation diagnostics failed before their fixes; an event `Add*` swap, omitted `addEvent` package-reference release, swapped environment/controller completion constructors, swapped options/handler arguments, changed parent `HWND`, and omitted loader options/controller-handler releases each made its consumer-boundary test fail. Earlier source-plan, fallback-generation and `//go:uintptrescapes` mutations retained their recorded failures.
- **Issues #86/#104/#110 and audit follow-up live:** `mullion doctor` found WebView2 151.0.4129.72 and its direct export; `examples/basic` served all three assets, completed `Ping`, navigation and readiness with zero warnings/errors, rendered visibly on the current 1920x1080 100% single-monitor setup, and exited its message loop cleanly after capture.
- **Issues #86/#104/#110 and audit follow-up not covered:** local `-race` could not build because `gcc` is absent; failed event getters, failed setters, ledger saturation and filter-registration failure remain deterministic fault-seam tests; the frame/snap/DPI checklist was not repeated because this change does not alter window behaviour.
- **Issues #86/#104/#110 and audit follow-up `unverified`:** the runtime's empty-source representation for a fallback `data:` document, its NavigationStarting URI for a credentialed `Config.URL`, whether the inspected COM getters pump nested callbacks, and whether callbacks can arrive after controller close were not observed live; Windows/ARM64 remains compile-only by decision 0034.
- **Issue #112 automated:** the final pre-push ladder passed `gofmt -l`, Windows/amd64 build/vet/uncached full suite, WebView2 runtime/export opt-in, doctor, both diagnostic-tag build/test pairs, Linux/amd64 and Windows/amd64 builds, Windows/386 rejection tests, Windows/ARM64 build, the 272-file leak scan, and the bridge/real-resize-template VM. The VM exercises ordinary and 1.5-billion-pixel borders on an odd viewport, all eight routes, center separation, bounded zone styles, resize/maximize synchronization and both no-drag declarations; restoring target-first routing made it fail. Local `-race` was attempted and remains unavailable because the CGo-free local toolchain has no `gcc`.
- **Issue #112 live:** earlier scripted 96-DPI `WM_NCHITTEST` and normal input passed, and reporter runs moved normal Config plus large-title/large-controls fixtures across real 96/120 DPI with exact `1.00`/`1.25` rasterization, bounded rects, working large-title drag and clickable HTML controls. The reporter then exposed a large-resize failure: raw overlapping frontend zones and ancestor `app-region: drag` left top directions unavailable. After correction, a supervised 96-DPI odd `981x641` client dispatched `top-left/top/top-right/left/right/bottom-left/bottom/bottom-right` as hits `13/12/14/10/11/16/15/17`; real pointer deltas changed the matching rect sides and maximized probes returned no resize code. The reporter subsequently reran corrected C on a real 200% / 192-DPI display and accepted all eight directional resize drags, their visible cursors, maximized no-resize, ordinary titlebar drag and caption controls, including a transition between differently scaled monitors. After fixture removal, normal 8px runs at 120 DPI were accepted for edge-only resize, non-resizing interior, titlebar drag and caption controls; the final run reached frontend-ready with zero warnings/errors and closed through the UI with exit 0.
- **Issue #112 not covered:** the large-title and large-controls temporary passes were not repeated at 192 DPI, effective `MULLION_HITTEST_DIAG` transition rows remain absent, and normal §3 items beyond the recorded cursor/drag/caption/DPI smoke were not repeated. The reporter declined the restart required to re-establish a 192-DPI display and explicitly directed closure and direct `main` delivery using the prior 192-DPI C acceptance, 96/120-DPI A/B acceptance and exact headless production-path coverage. These omitted checks remain `unverified` and must not be cited as performed. The duplicated `old_dpi` field is a separate deferred P2 diagnostic defect, not transition evidence.
- **Issue #112 closure disposition:** the released-Config overflow, bounded native geometry, frontend overlap/no-drag correction and native move-loop ordering are locked by production-path regressions and the live observations above. All temporary fixtures were removed; issue #112 may close with the explicit coverage boundary retained here and on the tracker.
- **Issues #94/#107/#99 adversarial follow-up — automated:** named/generic string aliases, surplus slash/backslash authorities, mapped wildcard IPv6, backtick UNC and array inventory fixtures passed focused and real-child tests; six compiling disconnect mutations produced their named red signals; current-Go format/build/vet/uncached full suite, Go 1.24.2 full suite, both diagnostic-tag build/test pairs, bridge VM, 277-file/224-commit leak scan, Linux/amd64 and Windows/amd64 builds, Windows/386 rejection tests and Windows/ARM64 build passed. **Live:** `mullion doctor` found WebView2 151.0.4129.72 and its export and printed a paste-ready report on the current 1920x1080 100% single-monitor setup. **Not covered:** no window was created and frame/snap/DPI checks were not repeated; local `-race` remains unavailable because `gcc` is absent. **`unverified`:** surplus-backslash navigation was not driven through a live WebView2 instance, and the live machine supplied no backtick-wrapped UNC value; those boundaries are locked headlessly.
> Last updated: 2026-08-10 | Editor: OpenAI (GPT-5.6) | Change: record the #94/#107/#99 adversarial follow-up through generic string aliases, special-scheme separators, mapped wildcard IPv6, backtick UNC, array inventory, compiling mutants and the exact automatic/live/uncovered boundary.