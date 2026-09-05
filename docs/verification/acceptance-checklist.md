# Acceptance checklist

Status: active

## Contents

- [Manual acceptance checklist](#manual-acceptance-checklist)
- [Frame and input](#frame-and-input)
- [Lifecycle and navigation](#lifecycle-and-navigation)
- [Placement, timing, shell and DPI](#placement-timing-shell-and-dpi)
- [Diagnostics and rerun scope](#diagnostics-and-rerun-scope)

## Manual acceptance checklist

Launch `examples/basic` using the status-preserving PowerShell demo gate in
[automated-gates.md](./automated-gates.md#automated-gates) (or run the host
application), then walk the relevant list. Launch alone is not proof; only the
recorded observations below count. Report each monitor's resolution/scale and
the observed result. Mark unavailable scale factors `not covered` or
`unverified`; reporters are not expected to change OS DPI settings solely for a
report, and missing coverage does not make the session a failure. This does not
waive release acceptance for a frame, DPI or hit-test behavior change: the
maintainer or a dedicated verifier must supply the required mixed-DPI/different-
scale evidence on a suitable setup.

## Frame and input

- [ ] **FRAME-LARGE-METRICS-112 — Temporary issue #112 large-metric fixture,
      before and in addition to the normal checklist.** Use an odd physical
      restored window rect `[l,r) x [t,b)`; record `w`, `h`, `mx=l+floor(w/2)`
      and `my=t+floor(h/2)`. Run three temporary Config passes, keeping the
      other metrics ordinary: (A) `HitTestTitlebarHeight=1_500_000_000`; (B)
      `HitTestCaptionControlsWidth=1_500_000_000`; (C)
      `ResizeBorder=1_500_000_000`.
      At both 96 and 192 DPI, A must drag and trace `HTCAPTION` at an interior
      point outside controls/resize; B must trace `HTCLIENT` throughout the
      non-resize title width and HTML caption controls must remain clickable.
      In restored C, require `(l,t)=HTTOPLEFT`, `(mx,t)=HTTOP`,
      `(r-1,t)=HTTOPRIGHT`, `(l,my)=HTLEFT`, `(r-1,my)=HTRIGHT`,
      `(l,b-1)=HTBOTTOMLEFT`, `(mx,b-1)=HTBOTTOM`,
      `(r-1,b-1)=HTBOTTOMRIGHT`; each cursor must have the matching shape and
      each drag must resize in that direction. Maximize C: none of those eight
      points may return a resize code or resize the window. Move each pass
      between the 96/192-DPI monitors: ordinary companion metrics must
      ceiling-scale, huge metrics must remain clipped/saturated to the current
      rect, no region may wrap or disappear, and valid traces must report
      matching effective `side_border`, `top_border`, `titlebar_height` and
      `controls_width`. Remove the fixture and run **every** normal checklist
      item below; it is not a substitute for the normal configuration or live
      checks.
- [ ] **FRAME-TITLEBAR — Normal title bar.** Drag follows immediately;
      double-click toggles maximize and restore.
- [ ] **FRAME-DRAG-DOWN — Normal drag down from maximized.** The window restores
      under the cursor and continues following in the same gesture, without
      jumping to a corner or dropping the drag.
- [ ] **FRAME-RESIZE-EIGHT — Normal resize: 4 edges and 4 corners.** In all
      eight zones, separately verify the correct cursor and an actual drag
      resize in that direction; cursor and sizing use different paths.
- [ ] **FRAME-MOVE-LOOP-124 — Issue #124 live move-loop gate.** Before
      `WM_EXITSIZEMOVE`, a second frontend drag or resize gesture must remain
      disabled. After exit and a fresh authoritative frame-state snapshot, both
      gestures must recover. `frame_state_windows_test.go` and
      `scripts/test-bridge.mjs` cover deterministic seams, not live
      WebView2/Win32 scheduling.

## Lifecycle and navigation

- [ ] **LIFECYCLE-REGISTRATION-135 — Issue #135 exact serialized first-document
      registration.** With a supported WebView2 Runtime, first run an
      **untagged** artifact and verify bridge, diagnostics, drag and
      resize/frame behavior on its first navigation. Then build the same source
      tree with `mullion_script_completion_delay_diag` and run only
      `internal/cmd/mullion-issue135-diag`: its real-callback markers must show
      the first hold → one rejected `Show` → release → second hold → one
      `Quit` → release sequence, terminal barrier error, no
      registration/asset/watchdog/Navigate/frontend-ready/host-ready success,
      zero marker drop/timeout, and clean process exit. Record HEAD, dirty-tree
      tracked/untracked SHA-256 manifest, both binary hashes and Runtime/OS.
      The tagged artifact proves the adversarial integration only; it cannot
      prove untagged release behavior, so both artifacts are required. The
      [2026-08-31 paired record](./records/issues/issue-135-paired-live.md)
      completes this item on Win11 while retaining its untagged graceful-close,
      visual-Pong and Win10 nonclaims.
- [ ] **WINDOW-MINIMIZE — Minimize** from the custom caption control, and
      restore from the taskbar.
- [ ] **WINDOW-CLOSE — Close** from the custom caption control; the process
      exits and no child process is left behind.
- [ ] **NAV-NEW-WINDOW-SYSTEM-BROWSER — `window.open` / `target=_blank` → the
      system browser.** From the frontend, open an external `https://` link (a
      `target=_blank` anchor, or a scripted `window.open`): it opens in the
      default browser and **no second window appears** — a detached, chrome-less
      WebView2 popup is the failure. A non-http(s) scheme
      (`window.open('mailto:…')`) does nothing, and the log says
      `new window dropped, target not admitted` (decisions/0022).
- [ ] **NAV-BROWSER-LAUNCH-RESPONSIVE — The window still answers while the
      browser starts** (issue #74, decisions/0029). This is the user-visible
      half of that change; the timing half was measured with a probe and is in
      0029's Evidence (230 ms on a launch that starts the browser, and the
      handler itself below the clock's resolution). Kill every process of the
      default browser so the next launch is a cold start, then click an external
      link and — without waiting for the browser — drag the title bar and press
      a caption button. Both must respond at once. The 230 ms interval is a
      narrow window to aim at by hand: catching it takes deliberate effort, and
      failing to catch it proves nothing either way, which is why the probe
      exists. The browser still opens, and `external open dropped` must not
      appear — that line means all eight in-flight slots were taken, which one
      click cannot do.
- [ ] **NAV-PIN-ORIGIN — `Config.PinNavigationToOrigin` cancels off-origin top
      navigation** (opt-in — only when the field is set). With the gate on, a
      top-frame navigation to a foreign origin — an external `https://` link
      with no `target`, or a redirect off the trusted origin — is cancelled
      (the frontend stays put) and an http/https target opens in the system
      browser; the trusted origin, in-origin routing and the `data:` error
      surface are never cancelled. Verify a redirect specifically, and that
      the app's own startup navigation is not cancelled (decisions/0023).
- [ ] **NAV-CANCEL-COMPLETION — The cancel was performed, not merely asked for,**
      in the same run as the item above. Each cancelled navigation logs
      `navigation cancelled off origin, routed to system browser` and then
      `cancelled navigation completed, status=14, id=<n>` at debug — the
      completion was consumed, so the fallback error surface must not appear
      and the frontend must not be replaced. Four WARN lines say it went
      otherwise, and a good run has none of them:
      `cancelled navigation committed anyway` (the runtime accepted
      `put_Cancel` and navigated regardless, which is 0023's `unverified`
      premise failing), `cancelled navigation forgotten` (one of two
      independent four-entry ledgers overflowed: exact non-zero ids or
      anonymous known-zero/unavailable credits, so an evicted completion may
      arm the surface), `navigation cancelled off origin, target unreadable`
      (the URI getter failed and the gate cancelled a navigation it could not
      read), and `webview2 runtime warning, reason=cancel navigation <n>` (the
      runtime refused the cancel, so the foreign document loads — check that it
      is then **not** also opened in the system browser). Judge by the presence
      or absence of those lines, for the reason the next item gives about
      `SessionWarnCount` (decisions/0027).
- [ ] **NAV-IN-ORIGIN-ABORT — An in-origin full navigation does not fall into
      the error surface.** Serving the embedded assets (no `Config.URL`), click
      a link that navigates the top frame to another in-origin document
      (`<a href="index.html?x=1">`) several times: every attempt must land on
      the frontend. A `navigation failed, status=9` followed by `showing
      fallback error surface` is the issue #72 loop; with the rule in place the
      completion is reported once, at debug, as
      `navigation aborted, not arming the error surface, status=9, id=<n>`, the
      frontend stays, and **no `navigation failed` line and no WARN** appears
      for it (decisions/0024, 0026). Judge that by the absence of WARN lines,
      not by `SessionWarnCount`, which is a snapshot taken at frontend-ready.
      The abort is a race that needs the click to land while the previous
      navigation is in flight: click fast, or every attempt commits. The
      clicked navigation is told apart from the app's own startup navigation
      by the trailing `?` on its `navigation starting` line
      (`uri=https://mullion.localhost/index.html?` against
      `uri=https://mullion.localhost/index.html`); the query value is dropped
      from the log (decisions/0025). Match it literally — `?` is a regex
      metacharacter, so an unescaped `index.html?` matches both lines. Use
      `Select-String -SimpleMatch` or `index\.html\?$`. If a check ever needs
      more than two navigations told apart, give them distinct paths rather
      than distinct queries.
- [ ] **DIAGNOSTIC-FRONTEND-URL — A frontend error keeps the URL it names.**
      Make the frontend throw with a URL in the message —
      `throw new Error("could not load
      https://mullion.localhost/app/main.js")` from `app.js` is enough, or point
      a `<script src>` at a missing in-origin file. The ERROR line must read
      `frontend diagnostic error, message=…https://mullion.localhost/app/main.js`,
      with the host intact; `httpmain.js` is issue #80, the shape three releases
      of live verification were read past without anyone noticing
      (decisions/0028). A query in that URL must still be reduced to a bare
      `?`. Headless tests pin the reducer and its 2,000-byte bound, including a
      first URL after long context; only delivery of a real `window.onerror`
      string to this line is observable live (decisions/0035).
- [ ] **LIFECYCLE-REUSE — Window lifecycle reuse after failure and success.**
      First run a Host whose embed fails before loop entry, clear the cause,
      and call `Run` again on that same Host; close its painted window normally
      and call `Run` a third time on the same Host. Separately, reject a
      NUL-bearing title, then create a fresh Host with the corrected title and
      the same class name. Every supported retry must paint, with no stale
      `WM_QUIT`, "Class already exists", stale browser, or already-destroyed
      refusal (issues #48, #97).
- [ ] **LIFECYCLE-STALE-CONTROL — Sequential-Run stale-control and
      early-readiness adversary.** Reuse one `Host` for two sessions and record
      both Run tokens. Queue every private window command from session N
      (`Show`, `Hide`, `Quit`, `Minimise`, maximise toggle, drag, resize,
      bounds sync and `SetTitle`), then end N. Try to observe Windows reusing
      the same numeric HWND in N+1, but treat allocation as best-effort rather
      than a reproducible pass/fail step. If it happens, no stale command may
      apply or add a warning/log line in N+1; fresh commands must still apply.
      During an embed pump, signal `shellReady()` before the show gate starts:
      no show may be posted before start, and exactly one tagged show must be
      posted after start. Reject that show's first embed/application attempt and
      confirm the fallback re-arms and retries rather than leaving the non-hidden
      session invisible.
- [ ] **LIFECYCLE-TEARDOWN-ORDER — Teardown ordering under admitted calls and
      firing callbacks.** Block an already-entered
      `MarkFrontendShellReady()` between its bounds and show effects while
      teardown attempts to finish. Teardown must wait; both effects retain N's
      token. Separately fire N's startup/render timers, deferred bounds
      callback and worker-warning path after N+1 starts, using the same numeric
      HWND if the allocator supplies it. N+1 must receive no post,
      timeout/fallback line or warning. Block `IsMaximised()` inside its query
      and confirm `WM_DESTROY` cannot clear/reuse the HWND until the query
      returns. `TestRunTokensAreProcessGlobalAcrossHosts`,
      `TestPrivateCommandsRejectOldRunTokenAfterIdenticalHWNDReuse`,
      `TestExportedCommandsCarryEntryRunTokenAndPreserveWParamPayloads`,
      `TestLoggerMayReenterHostMethodWhileTeardownWaits`,
      `TestStartupShowGateLatchesEarlyReadinessUntilStart`,
      `TestStartupShowApplicationFailureRestoresFallbackAndRetries`,
      `TestOldRunTimersDeferredPostsAndWorkerWarningsStayOutOfNextRun`,
      `TestReadinessAdmittedBeforeTeardownCompletesInsideOriginatingRun` and
      `TestIsMaximisedPinsHWNDOwnershipUntilQueryReturns` exercise these through
      headless production dispatch/callback seams without creating a window.
      The headless seams deterministically force identical handles and pin token
      rejection. Actual numeric HWND recycling remains a best-effort live
      observation; failure to obtain reuse from the allocator is not a failure.
- [ ] **LIFECYCLE-START-HIDDEN — `StartHidden` → first `Show`.** With
      `Config.StartHidden` set, no window may appear until `Show()` is called;
      the first `Show` embeds the WebView and the frontend paints. Quitting
      without ever showing must still exit cleanly with no browser process left
      behind. (A `Show()` or `Quit()` landing mid-embed is an external
      Runtime/queue proof ceiling and may be observed live, but is supplemental,
      not a no-test exception; refusal and cancel logic is pinned headlessly —
      see decisions/0016.)

## Placement, timing, shell and DPI

- [ ] **FRAME-FIRST-PLACEMENT — First frame position and size.** The window
      opens centered in the primary monitor's work area, identical across
      launches, and its physical size is `Config.Width x Height` times the
      monitor's scale (e.g. 980x640 at 125% → 1225x800; the Debug-level
      startup log line `mullion: initial placement` states the applied
      numbers). On a scaled monitor an unscaled window is the issue #59
      regression (decisions/0018).
- [ ] **TIMING-STARTUP-GAP — Time the startup navigation gap** (issues #85,
      #77 - fixed, and this is the check that it stays fixed). No interaction is
      needed; the app's own startup navigation shows it. With the Logger at
      debug level, record numeric `T0` from `asset response served, status=200,
      ... asset=index.html` and numeric `T2` from `asset response served,
      status=200, ... asset=style.css`, then record numeric `T2−T0` (the
      document-to-first-subresource gap). Record the WebView2 Runtime version,
      cold/warm state for each run, run order including launcher/profile
      context, and preserve raw timing log or probe evidence. Name the approved
      baseline and numeric threshold used for comparison. Do not use
      `frontend diagnostic phase, phase=document created` as T2: it is stamped
      after the host receives a bridge message and can lag the stylesheet.
      “Tens of milliseconds” and historical ranges (including 47–141 ms and
      1,633 ms cold versus 15 ms warm) are descriptive, not pass thresholds.
      Without an approved numeric threshold, mark acceptability `unverified`,
      not passed. Upstream, unfixed:
      https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381
- [ ] **SHELL-SYSTEM-MENU — Right-click the title bar → system menu appears,**
      and its item states are correct **in both window states**: restored →
      `Restore` disabled, `Maximize` enabled, `Move`/`Size` enabled; maximized →
      `Restore` enabled, `Maximize` disabled, `Move`/`Size` disabled. This is
      the single most fragile item on the list: it breaks whenever style bits or
      the non-client path change, and it breaks silently because the menu still
      opens. Check it every time.
- [ ] **SNAP-EDGE-DRAG — Shell edge-drag placement.** Physically drag the
      window to a screen edge. It must snap to the half-screen work area (not
      the full monitor rect — the taskbar must still be visible), and dragging
      or snapping back out restores the previous size. Name this action and
      record placement and restore separately. This is shell-placement evidence,
      not maximize-button hover evidence.
- [ ] **SNAP-WIN-ARROW — Shell `Win`+`←` / `Win`+`→` placement.** The window
      snaps to the half-screen work area (not the full monitor rect — the taskbar
      must still be visible), and snapping back out restores the previous size.
      Name the direction and record placement and restore separately. This is
      shell-placement evidence, not maximize-button hover evidence.
- [ ] **SNAP-WIN-Z — Windows 11 shell Snap Layout UI.** On Windows 11, press
      `Win+Z` and observe the shell-owned Snap Layout UI, then place the window
      and record placement and restore. Name this action. `Win+Z`, `Win`+Arrow,
      and edge-drag are separate shell-placement observations; none proves the
      maximize-button hover flyout.
- [ ] **SNAP-MAXIMIZE-HOVER — Conditional native maximize-button hover.** This
      scenario applies only when an intentionally selected profile exposes a real
      native maximize path. For the current `caption_sysmenu_nccalc` profile,
      record `not applicable (outside contract)`, not a failure: its
      client-extended caption has no DWM or synthetic `HTMAXBUTTON` path. If a
      supporting native profile is deliberately in scope, use a real mouse hover
      over its native maximize button and require the flyout and cursor to be
      visible in the frame or recording. A keypress, hit-test trace, or another
      profile's diagnostic result is not a substitute.
- [ ] **DPI-MIXED-MONITOR — Mixed-DPI monitor transition.** Across different
      scale factors, the title bar, controls, borders and frontend text rescale
      without a stretched client bitmap; maximized on each monitor, the client
      fills the work area exactly and the title bar is not clipped.
- [ ] **SHELL-TASKBAR-AUTOHIDE — Auto-hide taskbar reveal while maximized.** Set
      the taskbar to auto-hide, maximize the window, then push the mouse into the
      auto-hide edge. The taskbar must still pop up (a 1px sliver is reserved
      for it — docs/decisions/0015). Verify on the primary monitor and, with the
      taskbar's monitor changed, on a secondary one. A window that covers the
      whole monitor and suppresses the reveal is a failure.

## Diagnostics and rerun scope

- [ ] **DIAG-HITTEST — Hit-test trace.** With `MULLION_HITTEST_DIAG=1`, repeat
      drag, resize and caption-button checks; require caption, all eight sizing
      codes and client in their expected regions. Correct visuals with wrong
      codes still fail.
- [ ] **DIAG-TOOLTIP — Tooltip trace** (when touching caption-control
      tooltips): relaunch with `MULLION_TOOLTIP_TRACE=1` and confirm show/hide
      events pair up and no tooltip is orphaned after the pointer leaves the
      window.

If a change touches the frame, DPI or hit-test code, the whole applicable list
is re-run. There is no "small frame change". Record tested contracts, live
observations, exact not-covered boundaries and uncertainty using
[the evidence boundary](./evidence.md); historical observations stay in
[records.md](./records.md).

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: extract the complete live checklist into stable frame, lifecycle, navigation, shell, DPI, and diagnostic scenarios.
