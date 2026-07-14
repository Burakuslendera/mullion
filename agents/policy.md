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
(likely). Not yet confirmed: re-run with the resource-requested log enabled and
check whether the callback fires and what it returns.
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
behaviour is defined in [window.md](./window.md) and
[docs/verification.md](../docs/verification.md).

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
