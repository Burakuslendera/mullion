# Verification records

## Contents

- [2026-08 records](#2026-08-records)
- [Issue #113](#issue-113)
- [Boundary](#boundary)

The dated records moved verbatim from
[`verification.md`](./verification.md) when that acceptance document reached its
400-line limit. Keep new command results and live observations here; keep the
acceptance rules and checklist in the parent document.

## 2026-08 records

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
- **CI run `31472023223` regression and correction — P0:** all four jobs failed before their later gates. Windows Go 1.24/stable stopped at leak-scan because three files newly tracked by `2694deb` contained four intentional synthetic drive/UNC fixtures that the pre-commit scan had not seen while those files were untracked; four exact path/rule/component/count allowances raise the measured production total from 70 to 74, and the first corrected pre-push scanner passed over 287 tracked text files and 225 commits. Portable Go 1.24/stable reached the real UTF-32 rejection test, received the intended nonzero `unsupported UTF-32 byte order mark` exception, then misreported failure because PowerShell on the Ubuntu runner wrapped the final word to its terminal width; the assertion now binds the nonzero/no-clean verdict plus the stable classifier prefix rather than host formatting. Focused tests, current-Go format/build/vet/full suite and bridge VM, Go 1.24.2 build/vet/full suite, and current/floor Linux-amd64, Windows-amd64/386/ARM64 and Darwin-arm64 builds passed locally. **Remote:** correction run `31473577189` completed successfully; Windows and Ubuntu portable jobs both passed on Go 1.24 and stable, including leak-scan, both full suites, Windows race/tag/runtime gates, bridge VM and every portable cross-build. **Issue #125:** the workflow adds one unconditional stable-Go `windows x64` singleton to the two existing two-value matrices, declares `windows/amd64`, requires WebView2 and runs an uncached full suite. Its structural and action-count regressions, current-Go full ladder, Go 1.24.2 full suite, 287-file/227-commit scanner with 76 exact allowances, and the exact local x64 build/full-suite command with runtime opt-in passed. Actions run `31479374909` then scheduled exactly five jobs—`windows x64`, Windows 1.24/stable and portable 1.24/stable—and all five completed successfully with no failed step; the requested fifth-lane result is verified.
- **Current callback/frame/backdrop preventative pass — automated:** passed `go test -count=1 ./host -run '^(Test(WindowProcRegistry.*|CreateWindowProcToken.*|SharedWindowProc.*|PrivateCommandsRejectOldRunTokenAfterIdenticalHWNDReuse|ResizeCommandRejectsInvalidPayloadBeforeOperationSeam|NCCalc.*|WindowProcBoundsSyncsOnlyPostApplyMessages|NativeRunTokenRegistrySkipsLiveTokenAtUintptrWrap|EndRunReleasesActiveNativeRunToken|MaximizeGeometryUsesAutoHideInset))$'`; `go test -count=1 ./internal/backdrop -run '^(TestCleanupBackdropWindowOwnsEveryLoopExitWithoutDoubleDestroy|TestArmBackdropWatchClearsStaleTargetAndCommitsOnlyAfterTimerSuccess|TestClearBackdropWatchStateRejectsStaleAndLiveOwnership))$'`; `go test -count=1 ./...`; and `go vet ./...`. These headless tests cover callback/token lifecycle, NCCALC pointer handling, degradation and auto-hide geometry, post-apply bounds synchronization and resize-payload validation, and backdrop timer ownership. **Unobserved live-only boundary:** real Win32 message delivery, resize, title drag, system-menu and Snap behavior, mixed-DPI and auto-hide-taskbar rendering, and live backdrop operation remain unobserved; headless results cannot close those paths.

- **2026-08-21 — Issues #102/#111/#82 final local automated-CI matrix:** the repo-local pre-push commands passed, with no remote-CI or live-GUI claim. Go 1.26.5 on native Windows/amd64 passed formatting, build, vet, the uncached full suite, required WebView2 tests, direct doctor/export execution and the bridge VM; Go 1.24.0 on Windows/amd64 passed build, vet and the uncached full suite. Also under Go 1.26.5, the `mullion_dwm_caption_diag` and `mullion_caption_passthrough_diag` build/test pairs passed; the leak scan passed over 298 files and 240 commits; Windows/386 under WOW64 passed the focused architecture tests—including doctor exit, public-sentinel and no-native-callback assertions—and the full build; and Windows/amd64, Windows/ARM64, Linux/amd64 and Darwin/ARM64 builds passed. Initial full-suite attempts failed only because repository-root cache blobs were scanned as source; after the caches were removed or moved outside the scanned tree, the same commands passed without a source edit.
- **2026-08-21 — Issues #102/#111/#82 final local automated-CI boundary / `unverified`:** a Go 1.26.5 race run was attempted with `CGO_ENABLED=1` but was unavailable because `gcc` is absent. Ubuntu native-runner tests and remote GitHub Actions were not run; the portable builds are compile evidence only and do not prove WebView2 hosting, rendering or other live GUI behavior.
- **2026-08-21 — Issues #102/#111/#82 live, observed:** Windows 11 25H2 build 26200.9168, amd64, Go 1.26.5 and WebView2 151.0.4129.93 on two 1920x1080 96-DPI monitors. In the second run from the same GoLand launcher, the embedded-source request at `00:40:50.309` reached its filter at `00:40:50.714` (~405 ms); shell-ready was 660 ms, visible 739 ms, frontend-ready 758 ms and visible-to-ready 18 ms. `index.html`, `style.css` and `app.js` returned 200, `Ping` was received/completed, and `Warn=0 Error=0`. The initial/cold run had reached visible at 13.101 s and ready at 13.124 s before forced `Ctrl+C`; the faster repeat is an observed difference, not evidence for any particular cause.
- **2026-08-21 — issue-specific live boundaries:** for #102, the repeat routed `top-right` as hit `14`, logged move-size `true→false`, and three times changed matching client/controller bounds equally; combined with the prior live hits `10/11/12/13/15/16/17`, valid native resize-edge routing is 8/8. The user visually observed a normal cursor glyph in every resize region they tried on the two 96-DPI monitors. For #111, the representative relative pin `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER=.\runtime` found no usable runtime, emitted the explicit-pin diagnostic, did not fall back to Evergreen and exited 1. For #82, live logging showed only the benign embedded-source summary `https://mullion.localhost`.
- **2026-08-21 — graceful close and assessment:** the repeat logged close requested/allowed, destroy requested, WebView2 shutdown requested, teardown reason `message_loop_exit`, message-loop exit and process exit 0; the Chromium unregister line seen after the earlier forced `Ctrl+C` was absent. Within these two runs, the ~13-second first startup was not persistent; a persistent startup regression is therefore unlikely on this exact setup, but its cause—including profile creation—remains unverified.
- **2026-08-21 — not covered / `unverified`:** cursor-glyph and other visual behavior at DPI scales other than the observed 96 DPI, pixel alignment, and mixed-DPI behavior were not exercised; both monitors used the same DPI. Invalid inherited edge names for #102 remain covered only in the headless JS VM, and malicious credential/control summary redaction for #82 remains covered only by headless tests. Race/CGo coverage was not part of this live session.

- **2026-08-21 — Issue #87 bounded live probes:** Windows build 26200/amd64,
  Go 1.26.5 and WebView2 151.0.4129.93. Probe 1 comprised two successful A-only
  controls and six B-present trials at 0/1/10/50/250/500 ms. In every B-present
  trial, A reported status 9 and was benignly suppressed before B started; B
  then reported status 14. No trial produced the required
  A-start/B-start/A-status-9 ordering or fallback arming. Probe 2 comprised four
  timing-equivalent probe/control pairs at 0/10/50/250 ms. Each probe established
  genuine A-start/B-start/A-complete overlap: A succeeded 1.23–1.48 s after B
  started, and only B failed, with status 0; every paired no-B control also
  succeeded. Neither bounded matrix reproduced #87.
  These negatives do not disprove the accepted risk: WebView2 permits cross-ID
  overlap but does not guarantee status 9 or B-cancel causality. [Decision
  0024](./decisions/0024-benign-abort-in-process.md) remains accepted. [Issue
  #87](https://github.com/Burakuslendera/mullion/issues/87) is
  CLOSED/NOT_PLANNED; it reopens only if the exact
  A-start/B-start/A-`ConnectionAborted` condition reaches fallback arming, which
  remains unproduced. Raw temporary logs and tables are local evidence, not
  repository fixtures. Separate residuals do not satisfy or reopen that gate:
  the successfully read empty pending-fallback claim previously catalogued with
  #75 is carried by [decision
  0037](./decisions/0037-event-values-preserve-getter-provenance.md) as its
  conditional P2 tripwire; getter provenance was fixed under #86; and a late
  old-browser callback after a new `Run` was already unverified. None reopens
  closed #87.

- **2026-08-21 — Issue #87 live basic regression smoke:** On the previously doctor-recorded Windows 11 25H2 build 26200.9168/amd64 workstation with Go 1.26.5, WebView2 151.0.4129.93 and 96 DPI, `go run examples/basic` logged 200 responses for `index.html`, `style.css` and `app.js`, shell-ready, `Ping` received/completed, visible, navigation completed and frontend-ready; `LaunchToVisible=3104ms`, `ShellReady=3054ms`, `Ready=3117ms`, visible-to-ready 13 ms, `Warn=0 Error=0`. Terminal interaction evidence covered all eight resize routes—left `10`, right `11`, top `12`, top-left `13`, top-right `14`, bottom `15`, bottom-left `16`, bottom-right `17`—with paired active `true`/`false` move-size generations and matching client/controller bounds. Repeated `960x1033` half-work-area and `1920x1032` full-work-area bounds were logged, but the log does not identify the gesture that caused each. Alt+F4 logged close requested/allowed, destroy, WebView2 shutdown, `message_loop_exit` and process exit 0. This basic smoke did not exercise the #87 A/B/status-9 interleave. Terminal evidence does not establish cursor glyphs, pixel alignment, smoothness or mixed-DPI behavior.

- **2026-08-21 — Issue #87 successful remote CI:** commit [`e2b7ef5`](https://github.com/Burakuslendera/mullion/commit/e2b7ef5) passed [GitHub Actions run `32518587177`](https://github.com/Burakuslendera/mullion/actions/runs/32518587177), 5/5 jobs: portable `stable`/Go 1.24, Windows x64, and Windows `stable`/Go 1.24. The Windows jobs passed `gofmt`, leak scan, build, vet, the full uncached suite, required runtime/export tests, doctor, bridge VM, Windows/386 gates, the race suite, and both diagnostic-tag build/test pairs; the portable jobs passed tests/bridge plus Windows amd64/386/ARM64 and Darwin cross-builds. Node.js 20 deprecation annotations are owned by open v0.0.3 [issue #122](https://github.com/Burakuslendera/mullion/issues/122) and are not #87 failures. CI created no live window; GUI evidence remains the preceding live smoke.

- **2026-08-21 — Issue #127 remote Go 1.27 compatibility evidence:** GitHub Actions run [`32425771310`](https://github.com/Burakuslendera/mullion/actions/runs/32425771310) resolved the Windows `stable` lane to Go 1.27.0 and failed there only because that toolchain's `gofmt` changed an ABI-manifest test fixture; the Go 1.24 Windows lane and both portable lanes succeeded. Formatting-only commit [`f69f129`](https://github.com/Burakuslendera/mullion/commit/f69f129) made the manifest stable across both printers; its recorded pre-push commands ran whole-tree `gofmt -l .`, `go test -count=1 ./internal/webview2`, and a dual-toolchain formatting diff under both Go 1.24 and Go 1.27, all clean.
- **2026-08-21 — Issue #127 successful remote matrix:** follow-up run [`32427012163`](https://github.com/Burakuslendera/mullion/actions/runs/32427012163) on `f69f129` completed successfully with 5/5 jobs: Windows Go 1.24 and `stable` (Go 1.27), the dedicated Windows x64 `stable` job, and portable Go 1.24 and `stable`. This is current-release compatibility evidence, not a floor change: the supported minimum remains Go 1.24.
- **2026-08-21 — Issue #127 active Go-floor audit:** [`go.mod`](../go.mod) remains `go 1.24` with no `toolchain` directive; README and CONTRIBUTING state Go 1.24 or newer; and the Windows and portable CI matrices remain `["1.24", "stable"]`. The active asset-root comments and regression fixture are consistent with Go 1.24. No floor, dependency, source, test or workflow change was required. [Decision 0042](./decisions/0042-go-1-24-remains-the-released-consumer-floor.md) owns the corrected policy and release-history evidence chain; [Issue #127](https://github.com/Burakuslendera/mullion/issues/127) owns its acceptance.
- **2026-08-21 — Issue #127 focused local automation:** `GOTOOLCHAIN=go1.24.0 go test -count=1 ./cmd/mullion ./internal/doctor` and `GOTOOLCHAIN=go1.27.0 go test -count=1 ./cmd/mullion ./internal/doctor` both exited 0 with both packages passing. On Windows 11/amd64, `GOTOOLCHAIN=go1.27.0 go run ./cmd/mullion doctor` exited 0 and reported Go 1.27.0, WebView2 Evergreen 151.0.4129.93, and `CreateWebViewEnvironmentWithOptionsInternal: yes`. The first `pwsh -NoProfile -File scripts/leak-scan.ps1` attempt rejected a raw full commit hash in the draft; after replacing it with a short hash, the same command passed over 298 tracked text files and 245 commits with 76 allowances and 0 inspection errors. `git diff --check` produced no output; read-only validators found all 105 local link instances and 6 external links valid, the owned documents within line caps with one current footer each, and no raw full hash. `git diff --numstat -- docs/decisions/0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md && git diff -- docs/decisions/0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md` returned `2 2` and showed only its superseded status and footer changing, so decision 0033's historical body remained immutable.

- **2026-08-21 — Issue #127 focused local boundary / not covered:** the package tests and doctor run are headless command-line evidence; they created no GUI or window and do not prove WebView2 control hosting or rendering. They verify the decision documents and focused doctor paths only, not the adjacent remote matrix or compatibility with untested future Go releases.

- **2026-08-21 — Issue #127 remote boundary / not covered:** the portable jobs cross-compiled Windows/ARM64 but did not execute it; Windows/ARM64 therefore remains compile-only. The Windows lanes checked build/test behavior plus runtime discovery and the required WebView2 export, but created no visible window and do not prove WebView2 control hosting or rendering. The formatting-only `f69f129` pre-push checks likewise make no runtime-behavior claim.

- **2026-08-21 — Issue #75 first live attempt, inconclusive:** Windows build
  26200.9168/amd64, Go 1.26.5, and WebView2 151.0.4129.93. Command transport
  removed backslashes from the intended absolute
  `UserDataFolder`, yielding malformed drive-relative
  `C:UsersPublicmullion-issue75-probe-20260821-01profile`. WebView2 resolved it
  beneath the temporary consumer and never completed controller creation; the
  25-second failsafe ended with HRESULT `0x80004004` (`Operation aborted`) before
  visibility. There were zero `window.open` calls, route events, external tabs,
  or server requests. This is setup-failure evidence only: it says nothing about
  popup suppression, `NewWindowRequested`, `PutHandled`, or routing. Exact
  command-line-matched owned WebView2 processes and temporary source/profile
  files were removed; repository status remained identically ` M host/config.go`
  and no tracker action occurred.

- **2026-08-21 — Issue #75 corrected live URI-route probe:** Windows build
  26200.9168/amd64, Go 1.26.5, and WebView2 151.0.4129.93. A single 750 ms timer
  recorded `direct_user_gesture=false` while
  `navigator.userActivation.isActive=true` and `hasBeenActive=true`; exactly one
  `NewWindowRequested` route logged `user_initiated=true` and the redacted
  `uri=http://127.0.0.1:49358/issue75?#`. That host callback can run only after
  `PutHandled(true)`, `GetUri`, and `GetIsUserInitiated` succeed, although the
  live log does not expose separate results for those three operations. The
  caller-owned loopback server then received the primary
  `GET /issue75?token=synthetic` with no `Authorization`, `Cookie`, or `Referer`;
  the fragment was absent at the server. Fifteen feed-shaped GETs and one favicon
  GET followed; the probe did not schedule them and assigns no cause. The host,
  server, matched WebView2 process, temporary consumer, fresh profile, and spill
  were cleaned; the only retained raw evidence is local, the target was never
  remote, repository status remained identically ` M host/config.go`, and no
  tracker action occurred. This one route proves the observed URI handoff/server
  reach on this setup only. It makes no POST, repeated-rate, default-browser
  profile/session, credential-forwarding, or ancillary-request-attribution claim;
  [decision 0043](./decisions/0043-external-routes-are-uri-only-os-activations.md)
  owns those boundaries.

- **2026-08-22 — Issue #75 malformed-userinfo P2 correction:** [decision
  0044](./decisions/0044-malformed-http-userinfo-is-never-emitted-by-diagnostics.md)
  owns the output guarantee. The literal-HTTP(S) logging reducer scans the raw
  authority after normal URL reduction fails. That authority ends at the first
  `/`, `\`, `?`, or `#`; a literal `@` before the boundary rejects the entire
  untrusted authority and retains only `unknown` plus bare query/fragment
  markers. Reverse solidus deliberately follows the WebView/WHATWG special-URL
  path boundary, so a later `@` is path data under the established fallback.
  The exact direct-value reproducer
  `https://alice:bad%zz@evil.example?token=s3cr3t#private-fragment` is locked to
  `unknown?#`; the backslash control locks the ordinary fallback boundary.
  Embedded `Message`/`Diagnostic` runs share the guarantee. While their
  authority is open, only literal space terminates the run; TAB, LF, CR, VT, and
  FF stay inside so later userinfo remains visible and any control-bearing raw
  authority fails closed to `unknown`. After `/`, `\`, `?`, or `#`, ASCII
  whitespace again terminates the run and preserves multiline path evidence.
  `Diagnostic("error 'https://alice:bad%zz@evil.example'")` is locked exactly to
  `error 'unknown`; the five-control valid/malformed userinfo matrix forbids all
  credential and authority pieces, and its completed-path controls preserve the
  path and following prose. A trusted production `WindowDiagnostic` callback
  carries the multiline reproducer through `MarkFrontendDiagnostic`,
  `logsafe.Diagnostic`, and the configured `Logger`; the emitted line contains
  only the sanitized `error 'unknown` projection. Existing foreign-source and
  fallback diagnostic denial regressions remain separate. Production new-window
  and accepted cancel-route regressions still lock rejection before the opener
  and the same credential-free direct-value diagnostic. The allocation
  regression still compares 1 KiB and 1 MiB malformed userinfo so output cannot
  regain input-sized retained state.

- **2026-08-22 — Issue #75 malformed-userinfo evidence boundary / not live:**
  this is source, deterministic headless regression, and independent security
  review coverage. No live WebView producer emitted the malformed target, no
  `ShellExecuteW` call was attempted, and no receiving-browser behavior is
  claimed. The valid-target live route recorded on 2026-08-21 remains separate
  evidence and does not prove this rejected-target diagnostic branch.

- **2026-08-25 — Windows 10 x64 VirtualBox live smoke / observed:** A VirtualBox
  guest reported Windows 10 Pro 22H2 (build 19045.3803), `amd64`, one
  1024x768 monitor at 100% (96 DPI), and Microsoft Basic Display Adapter (driver
  `10.0.19041.3636`). `mullion doctor` observed WebView2 Evergreen
  `151.0.4129.107` through the 32-bit HKLM EdgeUpdate view and reported the
  required `CreateWebViewEnvironmentWithOptionsInternal` export. The Mullion
  identity was `devel`; no usable source-commit stamp was captured. In that guest,
  `go run -buildvcs=true ./examples/basic` visibly rendered the first document and
  custom frame. The operator then observed the Ask Go for the time action, title-bar
  drag, maximize/restore, resize, title-bar system menu, and `Win`+Left Snap all
  working.

- **2026-08-25 — Windows 10 x64 VirtualBox smoke boundary / `unverified`:** This
  was a guest-built `go run` result, not an unchanged executable built on Windows
  11 and then run on Windows 10; it does not prove same-artifact Win11-to-Win10
  compatibility or release provenance. The Basic Display Adapter is not a real GPU
  driver result. There was no mixed-DPI transition, 3D-acceleration equivalence,
  actual multi-monitor coverage, Fixed Version Runtime coverage, or clean
  no-developer-artifact baseline. The observed single-monitor controls do not prove
  pixel parity, general Snap parity, or the full live checklist.

- **2026-08-27 — Windows 11-built x64 basic executable on the recorded Win10 VM /
  observed:** On a Windows 11 x64 host (build 26200), the basic example was built
  from clean detached commit `2a20cffb0dfdd4dc6b3af028eed5f63e4955b1af` with
  `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64`, and `GOAMD64=v1`. The transferred
  source artifact was
  `mullion-basic-win11-2a20cff-go1.26.5-windows-amd64v1.exe`, with SHA-256
  `5A9B807B7B809F666B2B3AD11D8518B896B079EC3B5515317046B0796A424F00`. A screenshot
  of the recorded Windows 10 Pro 22H2 (19045.3803) VirtualBox guest's
  `Get-FileHash` output observed the same SHA-256 for that transferred executable.
  The operator reported that directly running it in the guest succeeded.

- **2026-08-27 — Windows 11-built executable evidence boundary / `unverified`:**
  The matching guest hash proves that the observed transferred file was the recorded
  Windows 11-built artifact; together with the operator-reported direct launch, it
  is same-artifact Win11-to-Win10 startup evidence for this exact host/guest/runtime
  combination. The screenshot does not independently record the running window or
  its interactions, and no video was inspected. The earlier guest-built smoke's
  Ask-Go-for-the-time, drag, maximize/restore, resize, system-menu, and `Win`+Left
  Snap observations remain separate: they do not prove those controls were rerun
  with the transferred executable. This result does not establish mixed-DPI,
  real-GPU or 3D-acceleration behavior, multi-monitor coverage, Fixed Version
  Runtime coverage, pixel parity, a clean no-developer-artifact baseline, or full
  release parity.

- **2026-08-27 — Win10-built x64 basic executable transfer / observed:** The
  Windows 10 guest built the basic example as a CGo-free `windows/amd64` artifact
  with `GOAMD64=v1`. On the Windows 11 host, the received executable had SHA-256
  `A6B15AD5DAE3D2BFDD0B5FC0D2952A02234636AC71FA552CBAE379BD39B51860` and metadata
  reporting Go `go1.26.7`, `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64`, and
  `GOAMD64=v1`; the metadata recorded clean VCS revision `8807ede`. The operator
  reported the same SHA-256 on the Windows 10 guest and that directly launching
  this unchanged executable on the Windows 11 host succeeded.

- **2026-08-27 — Win10-built executable evidence boundary / `unverified`:** The
  matching hashes establish the identity of the guest-built file across transfer;
  together with the operator-reported direct host launch, this is same-artifact
  Win10-to-Win11 startup evidence for this exact guest/host/runtime combination.
  The host execution was not independently captured in a screenshot or video, so
  no rendered-window, bridge-result, or individual frame-interaction result is
  claimed for this executable. It does not establish mixed-DPI, real-GPU or
  3D-acceleration behavior, multi-monitor coverage, Fixed Version Runtime
  coverage, pixel parity, a clean no-developer-artifact baseline, or full parity.

- **2026-08-28 — Windows CI publication-evidence failure / observed:** GitHub
  Actions run [`33167361118`](https://github.com/Burakuslendera/mullion/actions/runs/33167361118)
  on commit `ca7b67d` failed in both `windows` matrix jobs at `leak-scan` before
  build, vet, tests, runtime checks, doctor, race, or diagnostic-tag steps began.
  The scanner reported eight findings in the Windows compatibility evidence added
  on the preceding day: three artifact-identity values and five artifact-name
  values. The separate `windows x64` job and both portable jobs completed
  successfully; this record does not infer any Windows GUI result from them.

- **2026-08-28 — recurrence control / observed policy:** The scan's fail-closed
  result was correct: the evidence matched detector families without exact
  path/rule/value/count allowances. The correction must retain the evidence and
  add only those narrow allowances with a real-script exact-value and near-miss
  regression. [Publication evidence](./publication-evidence.md) records the
  procedure; a clean scan after that correction proves configured publication
  scope only, not artifact execution or GUI behaviour.

### 2026-08-30 — [Issue #135](https://github.com/Burakuslendera/mullion/issues/135) required first-document registration

- **Implementation / contract:** [Decision 0045](./decisions/0045-required-document-created-script-registration-barrier.md)
  requires successful documented completions for bridge, diagnostics, drag, and
  resize before first `Navigate` or re-entrant Browser availability; optional
  tab-strip registration remains nonblocking with the classic fallback. This is
  the contract exercised by the Issue #135
  [verification checklist](./verification.md#3-manual-acceptance-checklist).

- **Automatic, local Windows evidence:** `gofmt -l .` printed nothing;
  `go build ./...` and `go vet ./...` passed. `go test -count=1 -timeout 15m
  ./internal/webview2 ./host` passed (`internal/webview2` 2.011 s, `host`
  156.288 s), and `go test -count=1 -timeout 20m ./...` passed all packages
  (`host` 151.176 s). `node scripts/test-bridge.mjs` printed exactly
  `bridge, frame-state resize and drag vm behavior: ok`.
  `go run ./cmd/mullion doctor` succeeded on Windows 11/amd64 with WebView2
  `151.0.4129.107`, the required export present, and two 1920x1080 displays at
  125% and 100%; `MULLION_REQUIRE_WEBVIEW2=1 go test -count=1
  ./internal/webview2` passed.

- **Automatic matrix and publication evidence:** both `go build -tags
  mullion_dwm_caption_diag ./...; go test -count=1 -tags
  mullion_dwm_caption_diag ./...` and the corresponding
  `mullion_caption_passthrough_diag` build/test matrix passed.
  `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=windows GOARCH=amd64 go
  build ./...`, `GOOS=windows GOARCH=386 go test -count=1
  ./internal/webview2 ./internal/doctor ./host -run '^TestUnsupportedArchitecture'`,
  and `GOOS=windows GOARCH=arm64 go build ./...` passed. `pwsh
  scripts/leak-scan.ps1` was clean across 305 tracked text files and 258
  commits with 0 inspection errors. The default `go test -count=1 -race ./...`
  reports that race requires CGo; with `CGO_ENABLED=1`, race compilation stops
  because `gcc` is absent. That is unavailable toolchain coverage, not a code
  test pass or failure.

- **2026-08-31 serialized refinement / automatic, uncommitted:** Against the
  current `fix/issue-135-script-registration-barrier` tree, focused
  `internal/webview2` completion/precedence/registration tests passed (2.391 s)
  and focused host barrier/startup/mutation tests passed (4.990 s).
  `gofmt -l .` printed nothing; `go build ./...`, `go vet ./...`, and
  `go test -count=1 -timeout 20m ./...` passed (`host` 207.359 s;
  `internal/webview2` 3.848 s). Both diagnostic-tag builds and full tagged test
  suites passed (`mullion_dwm_caption_diag` host 205.374 s;
  `mullion_caption_passthrough_diag` host 202.807 s). Linux/amd64 and Windows
  amd64/ARM64 builds plus the Windows/386 unsupported-architecture tests passed.
  `node scripts/test-bridge.mjs` printed its expected success line;
  `scripts/leak-scan.ps1` was clean across 305 tracked text files and 258
  commits with 0 inspection errors. A writable temporary `GOCACHE` was used;
  successful builds still emitted a non-fatal sandbox warning when Go attempted
  to update the read-only module stat cache. `gcc` is unavailable, so the race
  gate was not run.

- **2026-08-31 tagged marker isolation / automatic, uncommitted:** After the
  bounded marker dispatcher and explicit frontend-ready rejection were added,
  focused default completion/counter/mutation tests passed (`internal/webview2`
  1.143 s; `host` 2.155 s), as did all tagged coordinator tests (1.423 s) and
  the diagnostic-command package test (2.168 s). The adversarial tag tests hold
  the observer callback indefinitely while proving both fail-safe and Close
  still publish the genuine completion, the dispatcher terminates after its
  bounded observer timeout, post-close enqueue is counted rather than stranded,
  and a fresh coordinator can start. Default build, vet, and uncached full tests
  passed (`host` 174.691 s; `internal/webview2` 3.054 s). The new tag's build,
  vet, and uncached full tests passed (`host` 199.355 s; `internal/webview2`
  3.572 s; diagnostic command 2.590 s). A tagged
  command binary built successfully outside the tree; the same command without
  the tag failed as expected because build constraints exclude all Go files.
  Writable external `GOCACHE`/`GOTMPDIR` were used; Go's read-only module stat
  cache still emitted the known non-fatal warning. These automatic results were
  not either half of the later supported-Runtime protocol.

- **2026-08-31 final paired live acceptance:** The separately hashed untagged
  first-document run and `mullion_script_completion_delay_diag` run used one
  frozen source identity on WebView2 151.0.4129.107 / Windows 11 and complete
  the Issue #135 paired checklist item. The tagged command's sole 4.155-second
  attempt exited zero after its real-callback Show/Quit sequence. Artifact,
  source, log and manifest hashes, chronology, owned diagnostic counts and all
  nonclaims are canonical in the
  [paired live record](./issue-135-paired-live-verification.md).

- **Live Runtime observation:** Against the then-current uncommitted tree,
  `go run .` in `examples/basic` exited 0 with WebView2 `151.0.4129.107`. Logs
  recorded aggregate required-script
  registration before `Navigate`; the first document then emitted
  document-created/DOM diagnostics, installed the resize cursor, completed
  bridge `Ping`, reached shell-ready, navigation-completed, and frontend-ready,
  and quit/shut down/exited its message loop cleanly. The visible capture showed
  `Pong from Go`, the native non-client titlebar path, and a restored window.
  Three fresh visible runs of that exact then-current tree likewise showed
  first-document `Ping`/pong, the
  native app-region path, diagnostics, and resize readiness, with no visible
  missing-script manifestation.

- **Live slow-start lifecycle:** The then-current uncommitted branch contents were copied
  to a temporary workspace; only that copy carried non-production
  instrumentation: a five-second sleep inside each
  `AddScriptToExecuteOnDocumentCreated` completion callback; final repository
  source was unchanged. On WebView2 `151.0.4129.107`, `examples/basic` reached
  the delayed required-registration phase; a private `wmNativeShow` request
  carrying the current first-run token was posted to the hidden Mullion `HWND`,
  then `WM_CLOSE`. At 2026-08-30 02:00:53, logs
  recorded `show applying`; `show failed, reason=webview embed already in
  flight`; `close requested`; `visible=false`; `close allowed`; `destroy
  requested`; `webview2 shutdown requested`; and teardown outside the loop.
  The one `Run` pre-start terminal failure, `quit while waiting for
  document-created script registration`, exited 1 as expected. After `webview2
  embedded`, no `asset serving ready`, `injected scripts registered`, `navigate
  requested`, `initial show gated`, `native host ready`, frontend-ready, or
  visible-window success appeared; no half-ready window surfaced. This directly
  exercises the Issue #135 slow-start Show/close boundary and complements, not
  replaces, deterministic tests; it does not establish the Runtime's real
  callback scheduling or ownership.

- **Video audit and anomaly disposition:** a user-supplied 99.6-second,
  30-fps recording decoded to exactly 2,988 consecutive frames. The preserved
  150 contact sheets include every frame; 25 disjoint read-only audits inspected
  every assigned cell, covering F1–F2988 without a gap or overlap. The user
  reported that the first click-hold-drag sometimes had no move until
  release/retry. Its strongest visible no-effect interval is F1597–F1633
  (00:53.200–00:54.400), but the recording has no mouse-button state and the
  estimated press origin is about 5–7 px below the outer top, plausibly inside
  the documented 8-px resize band. Treat it as a separate P1 candidate with an
  unverified mechanism, not Issue #135 and not a code-fix claim. F2822's
  disabled-to-enabled resize-cursor pair is expected focus/resize resynchrony
  (`resize.js` hides then refreshes zones); F91–F134 is the normal fixed-size
  native move trajectory with expected `WM_ENTERSIZEMOVE`/zone-disable logs.
  Each of five `Chrome_WidgetWin_0 Error=1411` onsets coincided with IDE
  Stop/Ctrl+C forced termination; source and documentation identify that as
  Chromium/WebView2 forced-stop noise, absent on graceful close, rather than
  Mullion class ownership or Issue #135. Task View, visibility transitions,
  rerun dialogs, console selection/scrolling, and paint blanks were normal
  user/tooling states. Raw video evidence remains local and is not published
  here.

- **Still not covered / `unverified`:** The tagged artifact is neither a claim
  about ordinary Runtime scheduling nor byte-equivalent to the untagged artifact.
  The manual untagged X interaction lacks an exit code and owned teardown, its
  exact visual Pong payload was not preserved, and its manual resize gestures
  were bottom-right rather than pure-right. The identical-artifact raw log
  independently supplies right-edge evidence; its one prepared, no-retry
  Computer Use attribution comes from the live session record. No serialized
  action receipt was retained, so the attribution is not independently
  reconstructable offline.
  Local race remains
  unavailable without `gcc`. The issue #129 Win10 attempt stopped at its first
  UUID-conflicting `registervm` failure, so no guest hash or same-artifact Win10
  execution is claimed.

## Issue #113

- **Automated, repo-local Windows host focus:** `go test -count=1 ./host -run '^(Test(NativeCaption|ShouldUseDWMCaption|DWMCaption|NativeHitTestDiagnostic|WindowProc)|TestTitlebarDragHitTestDiagnostic|TestFormatNativeHitTest)'` passed with repository-local `GOTMPDIR` and `GOCACHE`. It covers the policy matrix, lazy candidate composition, zero unread candidate calls, one call per real reader, unchanged decision precedence, latched diagnostic switches, the diagnostic formatter gate and zero allocations on disabled pure paths.
- **Automated diagnostic variants:** `go test -count=1 -tags mullion_caption_passthrough_diag ./host -run '^(TestNativeCaption|TestShouldUseDWMCaption|TestDWMCaption|TestNativeHitTestDiagnostic)'` passed; the same command with `-tags mullion_dwm_caption_diag` passed.
- **Automated full host package:** the first uncached full host-package test failed because Go build/test cache directories had been created inside the repository tree. `TestNoUpstreamBrandLeak` walks that tree and treated cached tool text as repository content containing forbidden upstream references. Removing the repository-local temporary cache and rerunning with temporary/cache directories outside the repository made `go test -count=1 ./host` pass and left the source tree clean for leak scanning. This was verification-environment contamination, not a production or host-test logic failure.
- **Automated deeper boundary:** no live `HWND`/DWM allocation measurement is claimed; the headless invariant forbids creating one in unit tests. The pure allocation guards are contracts, not fake live Win32 measurements.

## Boundary

The pure tests do not measure live `LazyProc.Call`, `GetWindowRect`, DPI/monitor queries, DWM results, logger implementation cost, or actual mouse-message frequency. A live Windows run with a real window remains required for those costs and for tooltip/caption visual behavior. No test creates a window.

> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: clarify final paired Issue #135 provenance and attribution boundaries.
