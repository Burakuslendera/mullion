# Rule maintenance

Continuation of [AGENTS.md](../AGENTS.md). The rule wording below is preserved
verbatim from that entry point. In the preserved wording, “this file” continues
to mean the repository-root `AGENTS.md`, including in the rule-file limit row and
the protected-core references.

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
- the acceptance rules in [agents/window.md](./window.md);
- the uncertainty and honesty rules in [agents/policy.md](./policy.md);
- the headless-test invariant in [CONTRIBUTING.md](../CONTRIBUTING.md);
- the licence and external-code intake rules in [agents/notes.md](./notes.md).

If you are unsure whether a proposal is Tier 2 or Tier 3, it is Tier 3.

> Last updated: 2026-08-17 | Editor: OpenAI (GPT-5.6) | Change: preserve the repository-root AGENTS.md referent of moved deictic wording.
