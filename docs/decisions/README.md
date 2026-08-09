# Decision records

Why the code is shaped this way.

A generated map of the repository - or the code itself - tells you *what* the
architecture is. Neither can tell you *why*, which alternatives were weighed, or
what it would take to change it. That information is only recoverable if someone
writes it down at the moment the decision is made, because six months later the
reasoning is gone and only the artefact remains. A newcomer then sees an odd
choice, assumes it was an accident, and reopens a question that was already
settled - or worse, reverses a decision without knowing what it was protecting.

These files are that record.

- [docs/](../) says how the code works today.
- [lessons-and-dead-ends.md](../lessons-and-dead-ends.md) says what was tried and
  failed. Dead ends.
- **These records say what was chosen, and what it cost.** Live decisions.

## Index

| # | Decision | Status |
| --- | --- | --- |
| [0001](./0001-own-webview2-com-layer.md) | The WebView2 COM layer is written here, not taken from a third-party binding | Accepted |
| [0002](./0002-no-local-port.md) | Assets are served over an in-process virtual host, never a local port | Accepted, guard scoped by 0012 and 0030 |
| [0003](./0003-keep-caption-bits.md) | The frameless frame keeps `WS_CAPTION` and `WS_SYSMENU` | Accepted |
| [0004](./0004-host-answers-window-controls.md) | The host answers the window control methods; `Config.Bridge` is optional | Accepted |
| [0005](./0005-queryinterface-not-version.md) | Capability detection is `QueryInterface`, never a version compare | Accepted |
| [0006](./0006-tests-stay-headless.md) | No test creates a window | Superseded by [0039](./0039-public-run-preflight-stays-headless.md) |
| [0007](./0007-non-windows-stub.md) | Other platforms compile and return `ErrUnsupportedPlatform` | Accepted |
| [0008](./0008-doctor-is-a-go-command.md) | The environment report is a Go command, not a script | Accepted |
| [0009](./0009-public-package-at-host.md) | The public package lives at /host, not the module root | Accepted |
| [0010](./0010-ci-requires-the-runtime.md) | CI requires the WebView2 runtime, so the export check cannot silently skip | Accepted |
| [0011](./0011-host-owns-rasterization-scale.md) | The host owns the WebView2 rasterization scale | Accepted |
| [0012](./0012-config-url-loopback.md) | Config.URL lets a caller serve the frontend itself; mullion still opens no socket | Accepted, guard exemption extended by 0030 |
| [0013](./0013-backdrop-is-a-mullion-command.md) | The screenshot backdrop is a mullion command | Accepted |
| [0014](./0014-bridge-origin-at-dispatch.md) | The injected bridge acts only on messages from the trusted origin | Accepted, follow-up landed as 0022 + 0023; fallback authority refined by 0037 |
| [0015](./0015-maximize-insets-for-autohide-taskbar.md) | Maximized geometry insets 1px on an auto-hide taskbar edge | Accepted, narrowed by 0019 |
| [0016](./0016-single-flight-embed.md) | The WebView2 embed is single-flight, and a destroyed window cancels it | Accepted |
| [0017](./0017-error-surface-by-navigation-state.md) | The error surface is identified by navigation state, not by its source | Accepted, extended by 0020, orderings replaced by 0021, provenance refined by 0037 |
| [0018](./0018-initial-placement-centered-on-primary.md) | The first window is centered on the primary monitor's work area, DPI-scaled | Accepted |
| [0019](./0019-maximized-hittest-stays-in-process.md) | The maximized hit-test never queries the shell | Accepted |
| [0020](./0020-absorb-failures-while-surface-loads.md) | Failure completions are absorbed while the error surface loads | Superseded by 0021, log level set by 0026 |
| [0021](./0021-error-surface-navigation-identity.md) | Error-surface completions are attributed by navigation id | Accepted; anticipated cancel gate landed as 0023, refined by 0024, log levels by 0026, getter authority by 0037 |
| [0022](./0022-new-windows-to-system-browser.md) | New windows are routed to the system browser, never opened in the host | Accepted, launch moved off the UI thread by 0029 |
| [0023](./0023-navigation-cancel-gate.md) | A top-level navigation off the trusted origin is cancelled, opt-in | Accepted, ordering corrected by 0027, launch moved off the UI thread by 0029 |
| [0024](./0024-benign-abort-in-process.md) | An aborted navigation is not a load failure when mullion serves the assets | Accepted |
| [0025](./0025-urls-are-logged-as-urls.md) | A URL reaching a log line is reduced as a URL, not as a filesystem path | Accepted, trip-wire fired by 0028 |
| [0026](./0026-navigation-failure-level-follows-classification.md) | A failed navigation is logged at the level the host's own classification gives it | Accepted |
| [0027](./0027-cancel-is-committed-after-the-runtime-performs-it.md) | A navigation cancel is committed only after the runtime has performed it | Accepted, event provenance and fallback restoration refined by 0037 |
| [0028](./0028-message-keeps-the-urls-inside-it.md) | A message keeps the http(s) URLs inside it | Accepted |
| [0029](./0029-system-browser-launch-off-the-ui-thread.md) | The system-browser launch runs off the UI thread, bounded | Accepted |
| [0030](./0030-guard-exempts-the-virtual-host-name.md) | The no-port guard exempts one virtual host name, not a file | Accepted |
| [0031](./0031-the-bytes-never-decide-the-content-type.md) | The bytes never decide the content type, and the boundary decides the name | Accepted; reparse-point consequence answered by 0033 |
| [0032](./0032-the-supported-go-floor-is-1-22.md) | The supported Go floor is 1.22, and it is a promise rather than a default | Superseded by 0033 |
| [0033](./0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md) | The Go floor is 1.24, so that an asset directory can be an `os.Root` | Accepted |
| [0034](./0034-webview2-hosting-is-windows-amd64-only.md) | WebView2 hosting is supported only on Windows/amd64 | Accepted |
| [0035](./0035-frontend-diagnostics-are-bounded.md) | Frontend-controlled diagnostics are bounded before reduction and retention | Accepted |
| [0036](./0036-one-source-plan-defines-origin.md) | One source plan defines the frontend origin | Accepted |
| [0037](./0037-event-values-preserve-getter-provenance.md) | Event values preserve getter provenance before granting fallback authority | Accepted |
| [0038](./0038-terminal-policy-owns-error-reporting.md) | Terminal policy owns each error report | Accepted, public pre-inner boundary refined by [0040](./0040-public-preflight-errors-belong-to-callers.md) |
| [0039](./0039-public-run-preflight-stays-headless.md) | Public Run preflight stays headless | Accepted; supersedes [0006](./0006-tests-stay-headless.md) |
| [0040](./0040-public-preflight-errors-belong-to-callers.md) | Public preflight errors belong to callers | Accepted; refines [0038](./0038-terminal-policy-owns-error-reporting.md) |

## When to write one

Write a record when a change answers a **why** question that the code cannot
answer for itself:

- A dependency is taken on, or removed.
- An alternative that a reasonable engineer would pick was rejected.
- The library accepts a permanent cost in exchange for something.
- A constraint is imposed on every future change (an invariant).

Do **not** write one for a bug fix, a refactor that preserves behaviour, or a
choice that the next person would make the same way without thinking. A record
per commit is a record nobody reads.

## The template

Copy [`template.md`](./template.md). Number the file with the next free integer
and a short kebab-case slug.

Every record carries the same six sections, and one of them is the point:

**What would change our mind.** A decision without a trip-wire is a dogma. State
the observation that would make this wrong - a runtime that removes the entry
point, a dependency that finally exposes the interface, a measurement that
contradicts the assumption. The next agent then knows whether it is looking at a
settled question or a stale one, without having to re-litigate it to find out.

## Superseding

Records are **never edited to change their meaning and never deleted.** The
record of a decision is evidence of what was known at the time; rewriting it
destroys the only audit trail there is.

To change a decision, write a new record and set the old one's status to
`Superseded by NNNN`, with a link. Update the index. The old record stays exactly
as it was - including the reasoning that turned out to be wrong, which is usually
the most useful part.

Fixing a typo or a broken link in an old record is fine. Changing what it claims
is not.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: index the refined headless Run exception and caller-owned public preflight boundary, with forward status links from decisions 0006 and 0038.
