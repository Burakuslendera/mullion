# Policy and Communication

How to talk about what you know, what you suspect, and what you have not checked.
Read [AGENTS.md](../AGENTS.md) first.

This library's characteristic failure is a component that reports success and does
nothing: the WebView attaches, navigation completes, every call returns `S_OK`, and
the window paints white. A confident report is worth nothing here. What is worth
something is a report that separates what was observed from what was assumed.

## Uncertainty labelling

State only what you can support. When you cannot fully support a claim, label it —
in the pull request, in the document, and in the sentence itself.

| Label | Use it for |
| --- | --- |
| `observed` | Something actually seen: a log line, a test result, a value read at runtime, a window on a screen. The cause may still be unknown; the fact is not. |
| `likely` | A reasonable inference from the evidence, not yet confirmed. |
| `unverified` | Plausible, untested, or from a source you have not checked. |
| `assumption` | Something you are provisionally taking as true in order to proceed, without evidence. |

`observed` is about *what happened*; `likely` is about *why*. Keeping them apart is
most of the value. "The client rect is 46x39" is `observed`. "…because
`SWP_FRAMECHANGED` was called without `SWP_NOMOVE`" is `likely` until you have
removed the flag and watched it change.

**When a labelled claim is verified, the label must be removed** and the text
updated to say what was found. A permanent `unverified` on a claim that has since
been confirmed teaches the next reader to ignore the labels — which destroys the
system for everything that is still genuinely uncertain. If the old wording is kept
for context, mark what confirmed it.

Uncertainty belongs in the document, not only in the conversation. A doubt raised
in chat and not written down is a doubt that will be rediscovered from scratch.

## No false confidence

Reserve "this is definitely", "the cause is", "this fixes it" for claims backed by
evidence you can point at. Without that evidence, write the weaker sentence — it is
not a worse sentence.

Wrong:

```
The blank window is caused by the asset callback failing.
```

Right:

```
The window is blank and the diagnostic payload shows document=1, stylesheet=0,
script=0 (observed). That shape points at the asset path rather than navigation
(likely). Not yet confirmed: re-run with a Debug-level `Logger` and read the
`asset response served` / `asset response error` lines to see whether the
stylesheet request arrived at all, remembering that a served asset outside the
document/stylesheet/script buckets prints nothing (docs/architecture.md).
```

The second version costs one extra sentence and tells the next reader exactly what
to do. The first version, if wrong, costs them a day.

## "I don't know" is a complete answer

When you lack the evidence, say so, and then say what would settle it: which log
line, which test, which live check, which file. Guessing to fill a silence is the
one behaviour that makes an agent worse than useless, because a guess delivered in
the register of a fact is indistinguishable from knowledge.

Equally: **never present untested code as working.** "It compiles" is not "it
works", "the tests pass" is not "the window is correct", and a bounds log that
looks right is not a window that looks right. What counts as proof for window
behaviour is defined in [window.md](./window.md) and the
[evidence boundary](../docs/verification/evidence.md).

## Quality gate for behaviour changes

Apply this gate in the opening checklist of **every chat**, before editing code,
even when another agent supplied the diff. Run it again before saying "done".
The implementation is not accepted merely because it compiles or because a
previous agent called it complete.

1. **Audit necessity before implementation.** For every added production line,
   name the observable behaviour or invariant it protects, the existing owner
   you searched, and why reuse or deletion cannot satisfy the requirement.
   Reject duplicate abstractions, test-only hooks, retries, telemetry, speculative
   hardening and scope creep. A line added only to make a test convenient is not
   production justification. Tests and docs may be added to lock or explain an
   actual contract; they must not be used to legitimise unnecessary production
   code.
2. **Review the finished implementation adversarially.** Trace error and panic
   exits, timeout and cancellation, reentrancy, ownership and release order,
   ABI widths/vtable slots, and input/boundary paths. Check the negative case
   and the path where the underlying resource disappears or fails between two
   operations. Ask whether every new branch has a distinct contract; if not,
   remove it rather than preserving redundant code.
3. **Use a done gate, not a confidence statement.** Obtain an independent review
   or second set of eyes. For each changed observable contract, list the focused
   fail-first production-path headless regression, or the approved irreducibly
   visual/window-manager/shell/compositor residual that substitutes applicable
   live evidence. Live evidence never waives a deterministic contract. Then run
   the focused tests, `git diff --check`, and a changed-file scope check. The
   report names the production path, test result or live action, existing
   independent approver, artifact/environment, exact uncovered boundary, and
   remaining uncertainty using the labels above. “Hard to test” is not
   irreducibility; first extract or seam the decision. See the
   [evidence boundary](../docs/verification/evidence.md).
4. **Make the reasoning durable.** Comments explain only a non-obvious
   invariant, ownership, ordering, ABI or security reason; they do not narrate
   obvious control flow. Put the evidence and the actual runtime boundary in the
   canonical subsystem document, and update its single footer rather than
   starting a decision record for a bug fix or a behaviour-preserving audit.

Issue #98 is the ownership example: its focused fakes lock exactly-once COM
`AddRef`/`Release`, timeout, late callback, panic and event-registration paths,
while the real WebView2 callback schedule and rendering remain unverified. Do
not add another owner or claim a headless green test proves the runtime.
Issue #105 is the necessity example: its evidence was a directory request
classified as `500 read_error` and 1,000 renderer-chosen requests producing
`warns=0 errors=1000`; the local fix stats first so an existing directory is
`404 missing`, preserves the absent-favicon `204` convenience, and serves a
real `favicon.ico`. Its focused tests prove those response and log decisions,
not a live WebView2 request. Audit each of those added lines and retain only
the distinct contracts they establish.

## Repeat-prevention checklist

Use this checklist after reading the issue and again before handoff. Issue #102
shows why: one `in` check accepted inherited names such as `toString`, so a
typoed resize attribute consumed application clicks and emitted host warnings
even though the host correctly rejected the unknown edge. The contract was
allow-list membership, not a redesign of resize handling; keep that boundary
when preventing a repeat.

1. **Keep the issue boundary.** Write down the issue's requested observable
   behaviour and edit only the files and callers needed for it. Do not add
   adjacent cleanup, speculative hardening, abstractions, retries, telemetry,
   or unrelated tests, comments, and documentation.
2. **Justify every added line.** Before handoff, be able to name its observable
   contract, invariant, or test reason. Prefer reuse or deletion when no
   distinct reason exists; an abstraction or test hook added for convenience
   is not a justification.
3. **Test the boundary, not the implementation.** Cover the changed behaviour's
   positive and negative cases, inputs at its boundary, and the relevant
   failure or transition path. State exactly what those tests do not prove;
   do not treat a passing seam test as proof of an unexercised runtime.
4. **Audit independently before completion.** Before declaring implementation
   complete, reread the final diff as a second reviewer, without trusting the
   implementing narrative. Check scope, callers, error paths, ownership and
   boundary values, then remove unjustified lines before handoff.
5. **Make comments durable and handoff honest.** Add a code comment only for
   a non-obvious invariant or future-risk `why`, never to narrate obvious
   code. Report evidence, uncovered paths, and uncertainty. Push, commit, and
   closing the GitHub issue are not prerequisites for delivering the completed
   work.

This checklist prevents the #102 failure mode from becoming a broad rule:
preserve the smallest change that proves the requested contract, and record
the reason only where a future maintainer could otherwise break that contract.

## Honesty over agreeableness

- Say when a plan is wrong, including a plan you were asked to implement. Say it
  before you build it, not in the retrospective.
- Criticise the decision, the approach or the output — directly, and without
  padding. Agreement that is not earned is misinformation with better manners.
- Do not hide a gap because a session is nearly over, or because the change is
  otherwise good. An acknowledged gap is a task; a concealed gap is a bug with a
  head start.
- If you broke something, or misled someone earlier in the session, say so plainly
  and correct the record.

## Language and terminology

The project language is **English**, in the code, the documentation and the commit
history.

Technical terms are kept in their exact canonical form — `HWND`, `WM_NCCALCSIZE`,
`HTCAPTION`, `SWP_FRAMECHANGED`, hit-test, build tag, non-client area — and are not
paraphrased into prose synonyms for variety. This is a searchability rule as much
as a precision rule: a reader hitting a symptom greps for the symbol, and a
document that calls it "the sizing message" three different ways will not be found.
When a term first appears in a document, say what it does; after that, use it
exactly and consistently.

Depth is not verbosity. Explain the mechanism, then stop.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: require contract-partitioned headless proof and link the sole evidence boundary while retaining the existing independent review gate.
