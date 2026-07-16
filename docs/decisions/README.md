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
| [0002](./0002-no-local-port.md) | Assets are served over an in-process virtual host, never a local port | Accepted |
| [0003](./0003-keep-caption-bits.md) | The frameless frame keeps `WS_CAPTION` and `WS_SYSMENU` | Accepted |
| [0004](./0004-host-answers-window-controls.md) | The host answers the window control methods; `Config.Bridge` is optional | Accepted |
| [0005](./0005-queryinterface-not-version.md) | Capability detection is `QueryInterface`, never a version compare | Accepted |
| [0006](./0006-tests-stay-headless.md) | No test creates a window | Accepted |
| [0007](./0007-non-windows-stub.md) | Other platforms compile and return `ErrUnsupportedPlatform` | Accepted |
| [0008](./0008-doctor-is-a-go-command.md) | The environment report is a Go command, not a script | Accepted |
| [0009](./0009-public-package-at-host.md) | The public package lives at /host, not the module root | Accepted |
| [0010](./0010-ci-requires-the-runtime.md) | CI requires the WebView2 runtime, so the export check cannot silently skip | Accepted |
| [0011](./0011-host-owns-rasterization-scale.md) | The host owns the WebView2 rasterization scale | Accepted |
| [0012](./0012-config-url-loopback.md) | Config.URL lets a caller serve the frontend itself; mullion still opens no socket | Accepted |
| [0013](./0013-backdrop-is-a-mullion-command.md) | The screenshot backdrop is a mullion command | Accepted |

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

Every record carries the same five sections, and one of them is the point:

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
