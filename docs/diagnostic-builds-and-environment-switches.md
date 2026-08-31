# Diagnostic build tags and env switches

This is the canonical continuation of
[verification.md section 5](./verification.md#5-diagnostic-build-tags-and-env-switches).
The gate list referenced below remains in that parent document.

Diagnostics exist because the frame bugs in this library are *invisible* — the
window looks right and behaves wrong. Each switch trades a little runtime cost
or a little behaviour for a lot of visibility.

| Switch | Kind | What it does | When to turn it on |
| --- | --- | --- | --- |
| `mullion_dwm_caption_diag` | build tag | Builds an alternative caption/DWM extension path and logs the frame decisions it makes, so the default path can be compared side by side against it. | Double title bar, missing or extra shadow, wrong rounded corners, native caption leaking during startup, maximize glyph flicker. |
| `mullion_caption_passthrough_diag` | build tag | Builds a variant of the caption hit-test/passthrough behaviour and traces which component claims each caption-area point. | Drag works but caption buttons do not (or the reverse), snap layouts flyout does not appear on hover, hover state stuck after the pointer leaves. |
| `mullion_script_completion_delay_diag` | build tag | Adds the internal Issue #135 coordinator and diagnostic command. Only after each of the first two **required** registration handlers receives a genuine Runtime `Invoke`, the callback returns promptly and a goroutine bounded to ten seconds delays publication of that captured result into the existing barrier. The ordinary build selects a compile-time-selected no-op implementation; the tag alone is inactive until the internal command starts it. | Exact-tree supported-Runtime slow-start proof: re-entrant `Show` while the first real completion is held, then `Quit`/close while the second is held. Never ship this artifact. |
| `MULLION_HITTEST_DIAG=1` | env | Emits one line per hit-test decision: point, region, returned code. | Any drag/resize/cursor complaint; mandatory when changing hit-test geometry. |
| `MULLION_TOOLTIP_TRACE=1` | env | Traces caption-control tooltip show/hide/lifetime. | Tooltips that stick, never appear, or appear on the wrong control. |

Rules:

- A diagnostic tag is a **diagnostic**, never a release configuration. Ship the
  default path; use tags to find out why the default path is wrong.
- `mullion_script_completion_delay_diag` is deliberately behaviour-changing
  and exists only for this bounded timing proof. It has no environment switch,
  public `Config`, or release-path setting. It never fabricates or replaces
  completion, delays the
  Runtime's `Invoke` return or reference-release schedule, reorders/skips the
  four-step barrier, or touches optional tab-strip registration. Its exact
  `real_callback_held`, `release_requested`, `real_callback_published_*`,
  `publication_suppressed_*`, and `coordinator_closed` markers distinguish a
  real callback, explicit release, fail-safe timeout, and teardown suppression.
  Marker delivery uses one ordered finite dispatcher and a 100 ms observer-call
  timeout. A full queue or timed-out observer drops later markers and increments
  the command-visible failure counters; it never delays Close or the ten-second
  publication bound. Go cannot terminate arbitrary user code, so at most one
  timed-out marker invocation may remain blocked until it returns or the process
  exits; the dispatcher itself stops after dropping its finite remainder.
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

> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: bound Issue #135 marker observation independently from real completion publication.
