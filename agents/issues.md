# Issues and labels

How work is classified on the tracker. The priority ladder itself lives in
[AGENTS.md](../AGENTS.md); this file is the mapping from that ladder to the
labels on the repository, and the rules for using them.

A label is not decoration. It is the query someone runs when they have twenty
minutes and want to spend them on the thing that matters most. A mislabelled
issue is worse than an unlabelled one, because it answers the query wrongly.

## Three axes

Every issue carries **exactly one priority**, **exactly one type**, and **at
least one area**. An issue with no priority is not triaged, and an issue with no
area cannot be routed.

### Priority - one, always

| Label | Ladder |
| --- | --- |
| `P0: blocker` | Crash, hang, deadlock, COM lifetime bug, a window that shows nothing, a breach of the asset-serving boundary, a released API that is wrong. |
| `P1: window defect` | Broken hit-testing, wrong DPI behaviour, snap regression, flicker, a leak that grows without bound. |
| `P2: quality` | Missing test coverage for existing behaviour, a diagnostic that cannot distinguish two root causes, structural debt. |
| `P3: docs & tooling` | Documentation, examples, scripts, CI. |
| `P4: exploration` | Nice-to-have, research, an idea that is not scoped yet. |

**While an open `P0: blocker` exists, no lower-priority work is started.** That
rule is in AGENTS.md and the label is how it is enforced: the P0 query is the
first thing an agent runs at the start of a session.

### Type - one, always

| Label | Use it when |
| --- | --- |
| `bug` | Something is wrong. |
| `regression` | It worked before and does not now. |
| `enhancement` | A new capability, or a better way to do one that exists. |

`regression` is separate from `bug` on purpose. This library's regressions arrive
silently - a frame profile, a vtable offset, a DPI rule - and they call for a
different first move: **find the commit that broke it before reasoning about the
code.** The bisect is cheaper than the theory, and the theory is usually wrong.

### Area - one or more

| Label | Covers |
| --- | --- |
| `area: frame` | `WM_NCCALCSIZE`, `WM_NCHITTEST`, DPI, snap, the custom title bar. |
| `area: webview2` | `internal/webview2`: the runtime loader, the COM interfaces, the event handlers. |
| `area: bridge` | The JavaScript-to-Go bridge and the scripts injected into every document. |
| `area: assets` | Serving the `fs.FS` over the virtual host. |
| `area: diagnostics` | Logging, the render watchdog, the startup show gate. |
| `area: build` | `go.mod`, build tags, cross-compilation, CI. |

More than one area is normal and is a signal in itself: a bug that spans
`area: frame` and `area: webview2` is usually a coordinate-system disagreement
between the window procedure and the controller, and that is where to look first.

## The two labels that carry weight

### `silent-failure`

**A defect that reports success while doing nothing is `P0: blocker`.** Not P1.
Applying `silent-failure` and a priority below P0 is a contradiction; fix the
priority.

This is the failure mode the library is built against. A `PutBounds` that returns
`S_OK` and moves nothing, a `Navigate` that resolves against an empty document, a
COM setter that quietly no-ops on an old runtime - none of them raise an error,
and each of them produces a window that looks fine to the process and broken to
the user. The render watchdog, the startup timing summary and the asset
diagnostics all exist because of this class, and the label is what makes it
queryable:

```
gh issue list --label "P0: blocker" --label "silent-failure" --state open
```

is the list nobody may ignore. Quote the label names: they contain a colon and a
space, and `label:P0` matches nothing.

### `needs-repro`

A frame or DPI report cannot be investigated without its environment. Apply
`needs-repro` until the issue contains:

- the output of **`mullion doctor`** - Windows build, GPUs, every monitor with its
  physical resolution and scaling, and the WebView2 runtime this machine would
  actually load, including whether it still exports the entry point mullion calls.
  It needs no checkout: `go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor`;
- **which monitor** the window was on, and whether it had been dragged there;
- the **build**, from the `mullion: version=` line the library logs at startup: a
  tag, a pseudo-version carrying the commit hash, or a disclosed `replace`;
- whether **`Config.URL`** was set (the loopback opt-in) and to what - the
  `mullion: asset source=` line reports it. It changes both the asset path and the
  threat model, so a report that omits it can send you hunting the wrong layer;
- whether it reproduces in **`examples/basic`**, unmodified;
- the **log**, with `MULLION_HITTEST_DIAG=1` for a hit-test report and
  `MULLION_TOOLTIP_TRACE=1` for a caption or tooltip one;
- the steps, in the order they were performed.

**Do not accept a hand-typed display setup.** Windows reports a *virtualised*
resolution to a process that is not DPI-aware, so a reporter reading their own
settings panel will tell you "1536x864" for a 1920x1080 monitor at 125% - and you
will spend the afternoon hunting a scaling bug that does not exist. The script
declares per-monitor awareness before it measures anything. That is the whole
reason it exists.

The full contract is in [docs/bug-reports.md](../docs/bug-reports.md).

## Labels that are not used

`documentation` is retired. `P3: docs & tooling` plus the relevant area says the
same thing and does not compete with the priority axis. Do not apply it.

`duplicate`, `invalid` and `wontfix` are closing labels, not triage labels: they
are applied at the moment an issue is closed, with a comment that says why.

`good first issue` and `help wanted` are additive and never replace a priority.

`question` is the one exemption from the three axes. A question is not tracked
work: it carries `question` alone, takes no priority, and is closed when it is
answered. If answering it reveals a defect or a gap, open a *separate* issue that
does carry the three axes, and link it.

## Start of session

Two commands, before any code is read:

```
gh issue list --label "P0: blocker" --state open
gh issue list --label "needs-repro" --state open --limit 5
```

The first enforces the rule in [AGENTS.md](../AGENTS.md): while a P0 is open, no
lower-priority work is started. The second is cheap and often free money - an
issue that has been sitting on `needs-repro` for a week is frequently one you can
reproduce in the two minutes it takes to run `examples/basic`.

## The agent labels, not the reporter

A reporter should not have to know this taxonomy, and will not. **Labelling is
the agent's job**, on filing and on triage, and it is done with the tracker, not
in a comment that asks someone else to do it.

**Filing.** An agent never opens a bare issue. The three axes go on at creation:

```
gh issue create --title "Caption band is 4px short at 150% scaling" --label "P1: window defect" --label "bug" --label "area: frame" --body-file report.md
```

**Triage.** An incoming issue is usually unlabelled, or labelled by instinct
(`bug` on something that is really a `regression`, `enhancement` on something
that is really a silent failure). Correct it in place - adding and removing in
one call, so the issue is never briefly in a contradictory state:

```
gh issue edit 42 --add-label "P0: blocker" --add-label "silent-failure" --add-label "regression" --remove-label "P2: quality" --remove-label "bug"
```

Say in a comment *why* the priority moved. A reporter whose P2 silently becomes a
P0 learns nothing; one who is told "this returns S_OK and paints nothing, which
is the class this project treats as a blocker" learns the rule.

**Pull requests.** Same call, `gh pr edit`. The pull request carries the priority
of the issue it closes and the areas it touches.

**Never leave a work issue without the three axes.** If any axis genuinely
cannot yet be determined, retain provisional priority, type and area, add
`needs-repro`, and mark that provisional classification `unverified` in the
issue body or a comment under [policy.md](./policy.md). Do not add an unlisted
tracker label. An issue without the three axes is invisible to every query in
this file, which means it is invisible to the next session.

**Do not invent labels.** The set above is the set. A missing label is a rule
change, not a convenience: propose it, with the evidence for why the existing
axes cannot express the thing, and see the tiered authority in
[AGENTS.md](../AGENTS.md). Taxonomy drift is how a tracker stops being queryable.

**Labels are cheap; closing is not.** An agent does not close a behaviour issue
without evidence for every changed contract. A deterministic contract requires a
focused production-path headless regression. Only an approved irreducibly
visual, window-manager, shell, or compositor residual may use applicable live
evidence instead, with the full exception record in the
[evidence boundary](../docs/verification/evidence.md). Reporter confirmation is
evidence to record, not permission to waive a deterministic test. `Cannot
reproduce` remains no closure reason: retain the work issue's three axes and
`needs-repro`.

## Triage checklist

1. Reproduce. If reproduction is unavailable, apply `needs-repro`, assign
   provisional priority/type/area, mark that provisional classification
   `unverified` in the issue body, and stop code investigation.
2. Assign the three axes. If the priority is not obvious, read the ladder in
   AGENTS.md - not the issue title.
3. Ask whether it fails silently. If it does, it is P0 and it gets
   `silent-failure`, regardless of how small the fix looks.
4. If it is a `regression`, find the commit before opening a code editor.
5. Link the document that explains the subsystem. If no document explains it,
   that gap is itself a `P2: quality` issue.

## Pull requests

A pull request carries **the priority of the issue it closes**, so that the
tracker's P0 query surfaces the fix and not only the report. It carries
`regression` if it fixes one. It carries the areas it touches, which is what
tells a reviewer whether they are qualified to review it.

A pull request that changes behaviour must contain a focused production-path
headless regression for every deterministic observable contract. It is returned
when a test is missing unless the missing test is for an explicitly approved
irreducibly visual/window-manager/shell/compositor residual whose exception
record names the boundary, approver, sufficient live evidence, not-covered
boundary, and uncertainty. “Hard to test” is not an exception. See [the
evidence boundary](../docs/verification/evidence.md).

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: require deterministic contract regressions and a reviewed narrow live residual in issue closure and pull-request triage.
