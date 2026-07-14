<!--
Labels are applied during review: the priority of the issue this closes, plus the
areas it touches. You do not need to set them.
-->

## What changed, and why

<!-- The problem, then the change. If it fixes an issue, link it: "Closes #12". -->

## Verification

This project separates four things and does not blur them. A reviewer who cannot
tell which is which has to re-derive all four.

**Tested automatically** — which tests, which commands from the ladder in
[CONTRIBUTING.md](../CONTRIBUTING.md):

<!--
e.g. go test ./... (new: TestHitTestCaptionBandFollowsConfig)
     go build -tags mullion_dwm_caption_diag ./...
     GOOS=linux go build ./...
-->

**Verified live** — which items from the checklist in
[docs/verification.md](../docs/verification.md), on what display setup:

<!--
Required if this touches the frame, the hit test, DPI or snap. "It compiles" is
not acceptance in this architecture: a window that opens, renders, and has a
four-pixel-dead title bar compiles perfectly.

e.g. examples/basic on a 100% + 150% pair. Drag, double-click maximise,
     drag-down restore, all eight resize zones, system menu item states in both
     window states, Win+Left snap, dragged across the monitor boundary. Zero
     warnings, zero errors in the log.
-->

**Not covered** — and why:

<!--
Say it plainly. A reviewer who cannot see what you skipped will either re-derive
it at cost, or trust it wrongly.
-->

**Still uncertain** — with a label from [agents/policy.md](../agents/policy.md)
(`observed`, `likely`, `unverified`, `assumption`):

<!-- e.g. unverified: behaviour on a WebView2 runtime older than 131, which I do not have. -->

## Checklist

- [ ] Every behaviour change is locked by a test.
- [ ] No test creates a window, an HWND, or needs the WebView2 Runtime. The suite
      stays headless.
- [ ] If this changes a COM vtable, IID or slot order, the offsets are pinned by a
      test. The compiler cannot see an ABI mistake; only the test can.
- [ ] Documentation landed in the same pull request. If the fix taught the project
      something - a symptom, a root cause, a dead end - it is in `docs/`. Work is
      not done until it is written down.
- [ ] If this answers a *why* - a dependency taken on or dropped, a reasonable
      alternative rejected, a permanent cost accepted, an invariant imposed - it
      lands a record in [docs/decisions/](../docs/decisions/), including what
      would change our mind. Not needed for a bug fix or a behaviour-preserving
      refactor.
- [ ] `gofmt -l .` is empty, and `scripts/leak-scan.ps1` is clean.
