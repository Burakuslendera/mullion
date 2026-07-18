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
| How does the host talk to WebView2, and how are assets served? | [docs/webview2-and-assets.md](./docs/webview2-and-assets.md) |
| Why is the frame / hit-test / DPI code shaped like this? | [docs/frame-and-dpi.md](./docs/frame-and-dpi.md) |
| Snap, the non-client region, caption behaviour | [docs/snap-and-nonclient-region.md](./docs/snap-and-nonclient-region.md) |
| Where do those snap / non-client claims come from? | [docs/snap-sources.md](./docs/snap-sources.md) |
| **Why is it done this way, and what would change that?** | [docs/decisions/](./docs/decisions/) |
| What was already tried, and why was it abandoned? | [docs/lessons-and-dead-ends.md](./docs/lessons-and-dead-ends.md) |
| How do I prove a change actually works? | [docs/verification.md](./docs/verification.md) |
| Build, test, style, pull-request expectations | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| Frame and visual acceptance rules | [agents/window.md](./agents/window.md) |
| Note and documentation lifecycle; external code intake | [agents/notes.md](./agents/notes.md) |
| Uncertainty labelling, honesty, communication | [agents/policy.md](./agents/policy.md) |
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

## File size discipline

The limit is not tidiness. A rule file is loaded into context at the start of
every session, so its length is a tax charged on work that has not happened yet.
A reference document is read once, by someone who came looking for it. The two
are not the same object and do not get the same limit.

| Files | Limit |
| --- | --- |
| Rule files — this file, `agents/*.md`, `CONTRIBUTING.md` | **250 lines, hard.** Past ~230, stop adding sections and split. |
| Reference documents — `docs/*.md` | **400 lines, hard.** Past 250, the file must open with a table of contents. |
| `README.md` | Exempt. It is the landing page and stays one file. |

Splitting rules:

- Split at a logical boundary, move the content **verbatim**, use an ASCII
  filename, add the new file to the table above, and re-count the lines.
- **Nothing is lost in a split** — a split is a move, never an edit. If you find
  yourself rewriting a sentence while splitting, you are doing two changes at
  once; stop and do them separately.
- The old file links to the new one at the point where the content was removed.
  A reader who lands mid-topic must be able to find the rest.

Check it, do not estimate it. A file that quietly grows past its limit is the
same failure as a rule that quietly goes stale: nobody notices until an agent has
already acted on it.

## Tiered rule-change authority

Rule files decay. A stale rule is not neutral; it is active harm, because agents
obey it. Updating the rules is therefore legitimate — but the authority is tiered,
and the tier depends on whether the *meaning* of a rule changes. The evidence for
what a rule used to say is the commit history; that is what makes an in-place
rewrite safe.

**Tier 1 — the agent decides (mechanical hygiene).** Fixing a broken link or a
typo, updating a path after a file move, marking a statement that is no longer
current as `historical`, and opening a continuation file when a rule file reaches
the line limit. None of these change what a rule means.

**Tier 2 — allowed, with evidence.** Adding a new repeat-prevention rule after a
real failure, and rewriting a stale rule in place. Conditions: cite the evidence
(the commit, the failing test, the log, the live observation) that justifies it;
scan the other rule files for duplication and contradiction; and if the meaning
changed, say *which rule changed and why* in both the commit message and the
affected document.

**Tier 3 — explicit human approval only.** Deleting a rule, changing anything in
the protected core, and creating a new rule file that is not a continuation of an
existing one. Without approval the idea is written up as a **rule candidate** in
the pull request description and named in the final report — it does not go into
the rule files.

**Protected core** (changes only with explicit human approval):

- the *Non-negotiables* and *Priority ladder* sections of this file;
- the acceptance rules in [agents/window.md](./agents/window.md);
- the uncertainty and honesty rules in [agents/policy.md](./agents/policy.md);
- the headless-test invariant in [CONTRIBUTING.md](./CONTRIBUTING.md);
- the licence and external-code intake rules in [agents/notes.md](./agents/notes.md).

If you are unsure whether a proposal is Tier 2 or Tier 3, it is Tier 3.

## Honesty and signatures

Do not describe something you have not run as working. Do not smooth over a gap
because the session is nearly over. Label what is uncertain, remove the label when
you have verified it, and say plainly when you do not know. Constructive criticism
of a decision — including one you were asked to implement — is expected, not
tolerated. The full rules are in [agents/policy.md](./agents/policy.md).

Sign the documents you edit with **your own name**. Never copy another agent's
signature from an existing footer: a footer records who made *that* change, and
inheriting someone else's name destroys the only audit trail the notes have.
Historical signatures are left exactly as they are.
