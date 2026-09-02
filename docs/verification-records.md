# Verification records

## Contents

- [2026-09 records](#2026-09-records)
- [Boundary](#boundary)

Earlier dated records live in
[`verification-records/2026-08.md`](./verification-records/2026-08.md), moved
verbatim when the 2026-09-02 issue #140 entry pushed this file past its 400-line
reference cap — the same reason the first records were split out of
[`verification.md`](./verification.md). Keep new
command results and live observations here; keep the acceptance rules and
checklist in the parent document.

## 2026-09 records

### 2026-09-02 — [Issue #140](https://github.com/Burakuslendera/mullion/issues/140) startup-timing Logger re-entry deadlock

- **Automatic, headless Windows evidence / fail-first:** on the unfixed tree `TestStartupTimingSummarySurvivesLoggerReentrantShow` (host/startup_timing_reentry_windows_test.go, no window created) failed in 2.01 s with `recordStartupFrontendReady deadlocked behind a Logger re-entry while startupMu was held`; on the fixed tree it passes in 0.01 s, and control `TestStartupTimingSummaryEmitsOnceForPlainLogger` asserts the summary is still emitted exactly once with a repeated readiness record staying silent. `go build ./...` and `go vet ./...` passed; `go test ./host -run 'TimingSummary|StartupTiming|FrontendReady' -v -count=1` passed 9/9; `go test ./host -count=1` passed (ok 143.9 s); `go test ./... -count=1` passed all 7 packages. The fix splits `logStartupTimingSummary` into `snapshotStartupTimingSummaryLocked` (latching the once-flag under `startupMu`) and `emitStartupTimingSummary` (byte-identical message, after unlock); a full `host.log.*`/host-lock audit found no other Logger-under-lock site. [Decision 0046](./decisions/0046-logger-never-runs-under-a-host-mutex.md) owns the invariant.
- **Not covered / `unverified`:** no local `-race` run — the CGo-free toolchain has no `gcc` (pre-existing repo-wide gap), so the race lane is not exercised here. [Issue #159](https://github.com/Burakuslendera/mullion/issues/159)'s generation-zero run-call admission re-entry via the `beginRun` drain is a different mechanism, remains open, and is not addressed by this change.
- **Independent review:** a read-only adversarial reviewer re-audited all 188 `host.log` sites against every host-owned mutex (no remaining Logger-under-lock site; `beginRun`'s `WarnCount`/`ErrorCount` reads are atomic counter loads on the host's own sink, not embedder Logger calls), re-verified once-semantics, test soundness, scope (`git diff --stat` shows only the listed files) and docs footers, and re-ran `go build ./...`, `go vet ./...`, the targeted 9/9 run and a full `go test ./host -count=1` (ok, 219.9 s) — all checks passed.
- **Live, real-window Windows evidence (2026-09-02, primary monitor, dpi=96):** a repository-external driver (kept out of the tree on purpose) ran the issue's exact shape live: `New` + `Run` with a `Logger` whose `Info` calls `Show()` when the startup-timing-summary line is emitted. The console shows the summary emit, the re-entrant `Show` entered from inside that same `Info` call, `window visible` 95 ms later, and `Show returned without error — no deadlock`; `frontend ready` followed and the render watchdog did not fire. On the unfixed tree this shape is the deterministic deadlock of the fail-first headless run above. The operator exercised the UI checklist on the same run (content render, drag from an `app-region: drag` header, edge resize, minimise/maximise, snap, page focus) and reported pass; the sole observation was that the page drew no close button, which is the documented design — the frontend draws its own caption buttons and drives them over the bridge — not a defect. The driver's process was ended by the orchestrator rather than closed from the page, so the graceful close path was not exercised live.

## Boundary

The pure tests do not measure live `LazyProc.Call`, `GetWindowRect`, DPI/monitor queries, DWM results, logger implementation cost, or actual mouse-message frequency. A live Windows run with a real window remains required for those costs and for tooltip/caption visual behavior. No test creates a window.

> Last updated: 2026-09-02 | Editor: ZCode (GLM-5.3-Flash) | Change: record the 2026-09-02 issue #140 fail-first, passing, independent-review and live-window evidence; move the 2026-08 records to the verification-records/ folder at the 400-line cap.
