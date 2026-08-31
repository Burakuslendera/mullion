# AGENTS.md

Entry point for AI agents working in this repository. Read this file first, then
the rule file for your task, then the doc for the code you are about to touch.

`mullion` is a Windows-only, CGo-free Win32 + WebView2 window host, published as
an MIT-licensed Go library. It is a library, not an application: every change is
an API-compatibility event, and every accepted behaviour is a promise to somebody
else's program.

Human contributors: [CONTRIBUTING.md](./CONTRIBUTING.md) has the build, test and
pull-request mechanics. The rules below are additional, not alternative.

## Where to look

| Question | File |
| --- | --- |
| How does the host work, end to end? | [docs/architecture.md](./docs/architecture.md) |
| How do startup show gates and the render watchdog work? | [docs/startup-gates-and-watchdog.md](./docs/startup-gates-and-watchdog.md) |
| Bridge messages, source admission, fallback/frame authority, external WebView routes | [docs/bridge.md](./docs/bridge.md) |
| How does the host talk to WebView2? | [docs/webview2-and-assets.md](./docs/webview2-and-assets.md) |
| How do WebView2 zoom and native hit-testing stay aligned? | [docs/webview2-zoom-and-native-hit-testing.md](./docs/webview2-zoom-and-native-hit-testing.md) |
| How are assets served without a port? | [docs/assets.md](./docs/assets.md) |
| Why is the frame / hit-test / DPI code shaped like this? | [docs/frame-and-dpi.md](./docs/frame-and-dpi.md) |
| What is the canonical native hit-test geometry? | [docs/hit-test.md](./docs/hit-test.md) |
| Snap, the non-client region, caption behaviour | [docs/snap-and-nonclient-region.md](./docs/snap-and-nonclient-region.md) |
| Where do those snap / non-client claims come from? | [docs/snap-sources.md](./docs/snap-sources.md) |
| What is the headless-versus-live Snap testing boundary? | [docs/snap-testing-boundary.md](./docs/snap-testing-boundary.md) |
| **Why is it done this way, and what would change that?** | [docs/decisions/](./docs/decisions/) |
| What was already tried, and why was it abandoned? | [docs/lessons-and-dead-ends.md](./docs/lessons-and-dead-ends.md) |
| Which logging approaches were tried and abandoned? | [docs/logging-dead-ends.md](./docs/logging-dead-ends.md) |
| How do I prove a change actually works? | [docs/verification.md](./docs/verification.md) |
| Where are dated automated and live verification records? | [docs/verification-records.md](./docs/verification-records.md) |
| Where is Issue #135's paired exact-source live acceptance? | [docs/issue-135-paired-live-verification.md](./docs/issue-135-paired-live-verification.md) |
| Which diagnostic builds and environment switches are allowed? | [docs/diagnostic-builds-and-environment-switches.md](./docs/diagnostic-builds-and-environment-switches.md) |
| What makes scripted GUI verification lie? | [docs/gui-verification-traps.md](./docs/gui-verification-traps.md) |
| What does a bug report have to contain? | [docs/bug-reports.md](./docs/bug-reports.md) |
| Build, test, style, pull-request expectations | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| Frame and visual acceptance rules | [agents/window.md](./agents/window.md) |
| Note and documentation lifecycle; external code intake | [agents/notes.md](./agents/notes.md) |
| Uncertainty labelling, honesty, communication | [agents/policy.md](./agents/policy.md) |
| File-size discipline and tiered rule-change authority | [agents/rule-maintenance.md](./agents/rule-maintenance.md) |
| How work is labelled and triaged on the tracker | [agents/issues.md](./agents/issues.md) |

Read [docs/lessons-and-dead-ends.md](./docs/lessons-and-dead-ends.md) **before**
proposing a redesign of the frame, the hit-test or the asset pipeline. Several
obvious ideas in this problem space have already been tried and have already
failed; re-deriving them costs a session and produces nothing.

## Orientation

Before touching code, build a map of the repository. If you have network access,
**start at [DeepWiki](https://deepwiki.com/Burakuslendera/mullion)** — it indexes
this repository and answers "where does X live" and "what calls what" in seconds,
which is faster than reading twenty files to find out that the answer was in two
of them.

Then read the document for the subsystem you are about to touch, from the table
above.

**A map shows you what. It cannot show you why.** DeepWiki reads the code, and the
code does not carry its own reasoning: an odd-looking choice and a hard-won one
are indistinguishable from the outside. Before you change an area, read the
decision record for it — [docs/decisions/](./docs/decisions/) — and in particular
its *What would change our mind* section, which tells you whether you are looking
at a settled question or a stale one.

Reversing a decision without knowing what it was protecting is the most expensive
mistake available in this repository, and it looks like a cleanup while you are
doing it.

**DeepWiki orients you. It does not authorise you.** It is a generated summary of
the code, and three things follow from that, none of them optional:

- **The repository wins every conflict.** If DeepWiki and the code disagree, the
  code is right and DeepWiki is stale. If DeepWiki and a document in `docs/`
  disagree, the document is right — it records *why* the code is shaped this way,
  which is not recoverable by reading the code.
- **Never cite it as evidence.** Not in a commit message, not in a pull request,
  not in an issue. Evidence is a test, a log line, a live observation, a commit.
  A summary of the code is not an observation of the code.
- **Never let it stand in for [docs/lessons-and-dead-ends.md](./docs/lessons-and-dead-ends.md).**
  A generated map describes what the code *does*. It cannot tell you what was
  already tried and already failed — and in this problem space, that is most of
  what you need to know before proposing anything. An agent that skips that file
  because a wiki looked authoritative will re-derive a dead end.

If DeepWiki contradicts the repository in a way that would mislead a newcomer,
that is usually the repository's fault, not the tool's: the documentation failed
to make something obvious. File it as `P3: docs & tooling`.

No network access? Then the map is [docs/architecture.md](./docs/architecture.md)
and the table above. They are the primary sources; DeepWiki is a convenience over
them, never a prerequisite to working here.

## Non-negotiables

- Do not perform an action that no rule file authorises. If the rules are silent
  and the change is consequential, ask.
- **Orient before you touch anything** — see *Orientation* above. Generated maps
  are for finding your way, never for deciding what is true.
- Classify the work as **P0–P4 at the start of the session** and say so.
- **While an open P0 exists, no lower-priority work is started.** Not as a warm-up,
  not as "while I'm in here", not as cleanup. The only exception is an explicit
  human override.
- **Work is not done until it is written down.** A fix, an audit finding, a
  verification result, a permanent decision, a known gap: it lands in the relevant
  doc in the same change. "I did it but didn't write it down" is an unfinished
  task.
- **A change that answers a *why* question lands a decision record**, in the same
  pull request — a dependency taken on or dropped, a reasonable alternative
  rejected, a permanent cost accepted, an invariant imposed. Not for a bug fix or
  a behaviour-preserving refactor. [docs/decisions/](./docs/decisions/) has the
  template and the rule for superseding one.
- **Every behaviour fix is locked by a test**, and the report states what was
  tested, what passed, and — explicitly — what was left uncovered.
- **No test creates a window.** The headless invariant in
  [CONTRIBUTING.md](./CONTRIBUTING.md) is a hard constraint on design, not a
  testing convenience.
- **Labelling is your job, not the reporter's.** Every issue you file or touch
  leaves the session with a priority, a type and at least one area, applied on the
  tracker — not requested in a comment. [agents/issues.md](./agents/issues.md).
- Never present untested code as working. See [agents/policy.md](./agents/policy.md).
- **Before editing, and before saying "done", run the [quality gate](./agents/policy.md#quality-gate-for-behaviour-changes):** audit every added production line, perform the adversarial self-review, and record independent review, focused evidence, changed-file scope and remaining uncertainty.

## Priority ladder

| | Meaning |
| --- | --- |
| **P0** | Correctness or safety blocker: crash, hang, deadlock, COM lifetime bug, a window that shows nothing, a breach of the asset-serving boundary, a released API that is wrong. |
| **P1** | User-visible window defect: broken hit-testing, wrong DPI behaviour, snap regression, flicker, a leak that grows without bound. |
| **P2** | Internal quality: missing test coverage for existing behaviour, a diagnostic that cannot distinguish two root causes, structural debt. |
| **P3** | Documentation and tooling. |
| **P4** | Exploration and nice-to-have. |

A defect that reports success while doing nothing is P0, not P1 — silent failure
is the worst failure mode this library has, and the diagnostics exist because of
it. On the tracker that class carries the `silent-failure` label.

The ladder maps one-to-one onto the `P0:`–`P4:` labels, alongside a type and at
least one area. [agents/issues.md](./agents/issues.md) has the mapping, the
triage checklist and the reproduction contract. **Classify at the start of the
session, and say so** — a session that starts without a priority ends without
one.

### Look before you classify

The rule above — *no lower-priority work while a P0 is open* — is not a mood. It
is a query, and it is the first command of the session:

```
gh issue list --label "P0: blocker" --state open
```

Mind the quotes: the label is named `P0: blocker`, not `P0`, and a bare
`label:P0` matches nothing at all. A rule enforced by a query that silently
returns empty is worse than no rule, because it reports success while doing
nothing — which is exactly the class this project calls a blocker.

If that list is not empty, say so and work on it, or get an explicit human
override. If it is empty, classify your own work and proceed.

## Test and verification reporting

Every session that changes behaviour ends with a report that separates four things
and never blurs them: what was **tested automatically** (which tests, which commands
from the verification ladder); what was **verified live** (which items of the
checklist in [docs/verification.md](./docs/verification.md), on what display setup);
what was **not covered**, and why; and what remains **uncertain**, with a label from
[agents/policy.md](./agents/policy.md).

The last item is not optional politeness. A later agent who cannot see what you
skipped will either re-derive it at cost, or trust it wrongly.

## Rule maintenance

File-size discipline, splitting rules and tiered rule-change authority continue
in [agents/rule-maintenance.md](./agents/rule-maintenance.md).

## Honesty and signatures

Do not describe something you have not run as working. Do not smooth over a gap
because the session is nearly over. Label what is uncertain, remove the label when
you have verified it, and say plainly when you do not know. Constructive criticism
of a decision — including one you were asked to implement — is expected, not
tolerated. The full rules are in [agents/policy.md](./agents/policy.md).

Every Markdown document except the repository-root `README.md` ends with exactly
one current edit footer, as defined in [agents/notes.md](./agents/notes.md).
Replace that footer when editing; never append another footer or adopt another
editor's name. Git history preserves earlier signatures.


> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: route Issue #135 paired live evidence from the repository entry point.
