<!--
Labels are applied during review: the priority of the issue this closes,
`regression` if it fixes one, plus the areas it touches. You do not need to set
them.
-->

## What changed, and why

<!-- The problem, then the change. If it fixes an issue, link it: "Closes #12". -->

## Verification

This project separates four evidence classes and does not blur them. The
[evidence boundary](../docs/verification/evidence.md) owns their ceilings and
the exception/report schema.

**Automated/headless** — list every deterministic observable contract, its
production path, focused test/command, gate ID, and fail-before/pass-after
result. Use the command matrix in
[verification/automated-gates.md](../docs/verification/automated-gates.md):

<!-- e.g. go test ./... (new: TestHitTestCaptionBandFollowsConfig) -->

**Runtime/live** — list each real Runtime or OS run, exact artifact/source
identity and hashes, OS/build/architecture/Runtime/configuration, action and
observed result:

<!-- Required when the changed contract depends on the real Runtime or OS. -->

**Manual/human** — list applicable scenarios from
[acceptance-checklist.md](../docs/verification/acceptance-checklist.md), real
display setup, exact human action, and observed visual/shell result:

<!-- Required for applicable visual, window-manager, shell, or compositor claims. -->

**Scripted-GUI** — identify the validated harness, target window identity,
exact native-frame action and observed result, frontend-ready signal where
relevant, passive logs, and visual artifacts. Do not credit a scripted
native-frame action as HTML/DOM/bridge/navigation/browser proof without separate
same-run evidence.

**Approved live residual (only if applicable)** — name the exact residual and
one allowed class (`visual`, `window-manager`, `shell`, or `compositor`), seam
or extraction attempts and why no deterministic observable remains, the
independent reviewer/maintainer, same-run identity/environment, exact action and
result. “Hard to test” is not an exception. Live evidence never replaces a
deterministic headless regression.

**Not covered** — name every exact unexercised boundary and why; do not write
generic `N/A`.

**Still uncertain** — label remaining claims `observed`, `likely`, `unverified`,
or `assumption` using [agents/policy.md](../agents/policy.md).

## Checklist

- [ ] Every deterministic observable contract in the behavior change has a
      focused production-path headless regression that fails before and passes
      after.
- [ ] Any contract without that test is listed as an approved irreducibly
      visual/window-manager/shell/compositor residual with the required fields
      above; “hard to test” is not accepted.
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

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: require the exact scripted-GUI action and observed result alongside harness, readiness, and artifact evidence.
