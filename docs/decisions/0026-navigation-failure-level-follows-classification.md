# 0026. A failed navigation is logged at the level the host's own classification gives it

**Status:** Accepted

## Context

Six clicks on an in-origin link in `examples/basic`, after the issue #72 fix
landed, produced this (issue #79):

```
WARN  mullion: navigation failed, status=9, id=4
DEBUG mullion: navigation aborted, not arming the error surface, status=9
WARN  mullion: navigation failed, status=9, id=5
DEBUG mullion: navigation aborted, not arming the error surface, status=9
... six times
```

Every warning is contradicted by the line under it. The host decided each of
these aborts was benign (0024) and did nothing about it, which is the correct
outcome — but it had already reported the completion as a failure one line
earlier.

The two halves sat on opposite sides of that warning. `NavigationCompleted`
consumed the cancel gate's own `OperationCanceled` first (0023) and stayed
quiet; then it warned on `!success`; then it handed the completion to the
error-surface machine, where the abort, the superseded surface `Navigate` and
the absorbed straggler were each classified as expected and logged at debug.
Same category of event, two levels, decided by which side of one `if` the
classification happened to live on.

The warning could not have been right there. Whether a failed completion is a
failure the host is reporting depends on state the machine owns — whether the
surface is in flight, whether this id is the surface's own, where the
navigation that started under this id was going — and the callback runs before
any of that is consulted. Anything it logged was a guess at a level.

Two costs, and the second is the one that matters:

- **The log contradicts itself**, which is what the report was written about.
- **`SessionWarnCount` counts events the host suppressed.** The startup timing
  summary's warn count is the "did this run come up clean" signal, and it is the
  first thing a bug report is read for.

  Being exact about the second, because the issue's own framing is looser than
  the code: `logStartupTimingSummary` emits the count once, when the frontend
  reports ready, so it is a snapshot rather than a running total. The six clicks
  in the log above happened after that line was printed and did not reach it. A
  suppression *before* frontend-ready does — and that is not a corner case, it
  is issue #72's own shape, where the navigation that aborts and restarts is the
  startup navigation. Later suppressions inflate no counter because nothing else
  reads one; they only leave the contradiction above in the log. Same bug, two
  reach.

Auditing the paths for this record turned up a third, unreported: the surface's
own load dying produced **two** warnings for one dead surface — the callback's
generic one and the machine's `fallback error surface load failed, not
retrying` — so the one ending that most deserves an accurate count was
double-counting it.

## Decision

The completion callback hands failures down unlogged. **A failed completion is
reported exactly once, by the branch that classified it, carrying the status and
the navigation id, at the level that classification deserves.**

The level rule: **warn where the host is reporting a failure, debug where the
host attributed the completion and classified it as expected and handled.**

The eight endings a completion can have, and what each owes:

| Ending | Level |
| --- | --- |
| `armErrorSurface` — a real failure, replace the document | warn |
| the surface's own load failed — the seal (0021) | warn |
| absorbed with no identity available (0020's fallback) | warn |
| absorbed, positively attributed to another navigation (0021) | debug |
| an abort mullion served itself, on-origin (0024) | debug |
| the surface's own `Navigate` superseded (0021) | debug |
| the cancel gate's own `OperationCanceled` (0023) | debug |
| success | no failure line |

The warning for the arming ending lives **in `armErrorSurface`**, not at its two
call sites. Arming *is* the host deciding a failure is real and worth replacing
the document over, so it is exactly the set of completions a warning should
count; one site cannot drift from the other, and the invariant that the surface
is never in flight without a warning behind it becomes structural. The
`showing fallback error surface` info line the caller writes after it is
unchanged — it says what was done, not that something failed.

`noteOrderedOutcome`'s absorb stays at warn while the identified one drops to
debug, and the asymmetry is the point: absent identity, that branch is also
where the surface's own load dying lands — 0020 absorbs every failure in the
window precisely because it cannot tell whose it is, and the seal is unreachable
there. Debug would delete the only trace of a dead surface at the one place the
machine cannot name it.

## Alternatives rejected

**Keep the warning and make the machine's follow-up line warn too, saying the
earlier one was superseded** (the cheaper of issue #79's two options). Rejected:
it fixes the contradiction between two lines by making the count worse — every
suppressed abort would then count twice instead of once. The count is the part
that has to be true.

**Hoist `benignAbort` ahead of the log.** The obvious small fix, and issue #79
warned against it for the right reason. `benignAbort`'s own inputs are
callback-visible, but the *reachability* of its branch is not: it is only
consulted for a completion already classified foreign, with no surface in
flight. Hoisting it either duplicates that dispatch at the call site — two
copies of the rule that decides whether an abort even gets asked about — or
applies it in states the machine never would. It also fixes exactly one of the
four suppressions.

**Split the machine into a pure classify pass and an apply pass, and log between
them.** The tidy version, and it keeps the log lines in written order. Rejected
on risk for the benefit: these transitions are the most-audited code in the
repo (0017, 0020, 0021, 0024, four live-verified issues), and the order of the
mutations is load-bearing — arming resets the generation id, the claim window
opens and closes across branches. Rewriting all of it to move a log level is a
poor trade.

**Let the machine report the level back through its return value and keep one
log site in the callback.** Rejected on two counts: the line would print *after*
the branch's own description, so the log reads backwards (`navigation aborted,
not arming` before `navigation failed`); and the return value would need a third
state to tell the seal from the arming, which is a classification the callback
has no other use for.

**One uniform rule: every suppression is debug, including the unattributed
absorb.** Simpler to state, and rejected for the reason the asymmetry exists —
see the Decision.

## Consequences

**The warn count now counts failures the host did not suppress.** A startup
whose navigation aborted and was suppressed contributes 0 to the summary where
it contributed 1 per abort; a dead fallback surface contributes 1 where it
contributed 2. This is a behaviour change to a diagnostic other records quote —
0019's `SessionWarnCount=0` and any past report's count were counting a
different set of events.

**A suppressed failure is now only in the log at debug level.** An embedder
whose `Logger` drops debug lines — which is the common production setting — sees
nothing at all for a benign abort. That is the intent, and it means a live
report about aborts has to be collected with debug enabled.

**Every failure report carries `status=` and `id=`**, including the gate's
cancel line, which previously had the status but not the id.

**A branch that classifies a failure now owes it a line.** A future ending added
to the machine that returns without logging makes a failure vanish from the log
entirely — the failure mode the old unconditional warning could not have. The
one-line-per-completion table in
`host/errorsurface_logging_windows_test.go` is the trip-wire: it asserts the
whole line, verbatim, for all eight endings.

**The warning is tied to arming.** A future caller that arms the surface for a
reason that is not a navigation failure would emit a false `navigation failed`.
Both current callers arm behind `!success`.

## What would change our mind

- Navigation ids becoming unavailable in practice (a runtime or a build where
  `GetNavigationID` fails) would make 0020's fallback the common path, and its
  warning the noise this record set out to remove.
- A live report where the fault *was* a suppressed abort would say the debug
  level hides too much, and the answer would be a distinct level or a counted
  suppression rather than a return to the generic warning.
- A structured `Logger` — levels and fields instead of pre-formatted strings —
  would make "suppressed" a field on one event rather than a choice between two
  methods, and this record's table would collapse into it.

## Evidence

Commit on branch `fix-79-suppressed-abort-warn`. The contract is locked by
`host/errorsurface_logging_windows_test.go`:
`TestNavigationFailureIsReportedOnceAtItsClassifiedLevel` drives the seven
machine endings and asserts each writes exactly one line, verbatim, level
included; `TestGateCancelledCompletionIsReportedWithItsNavigation` covers the
eighth; and `TestSuppressedAbortsDoNotInflateSessionWarnCount` walks issue #79's
own sequence — six in-origin aborts leaving the count at 0, and a real failure
still moving it to 1.

Measured, not assumed: with the two source files reverted and the test file
kept, all eight endings and the count test fail. Two mutants were run against
the asymmetry and each was killed by the one case that owns it — the
unattributed absorb dropped to debug fails
`an absorb the machine could not attribute keeps its warning`, and the
attributed absorb raised to warn fails `an attributed straggler is absorbed
quietly`.

Not verified live. The change alters log levels and line text only; the
`examples/basic` in-origin-navigation item in
[verification.md](../verification.md) reads the affected lines and was extended
with the warn count, `observed` for the headless behaviour and `unverified`
for the live log.

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: new record - a failed completion is reported once by the branch that classified it, at the level that classification deserves (issue #79); the warning moves into armErrorSurface and 0020's unattributed absorb keeps its warning.
