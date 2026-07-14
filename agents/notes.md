# Notes and Documentation

How knowledge is written down in this repository, and how it is kept true. Read
[AGENTS.md](../AGENTS.md) first.

The rule this file serves: **work is not done until it is written down.** A fix
whose reasoning lives only in a commit diff will be undone by the next person who
finds the code surprising — and in a Win32 + WebView2 host, almost all of the code
is surprising. The comment answers *what*; the document answers *why this, and
not the obvious alternative*.

## Four questions, four homes

Every piece of durable knowledge answers exactly one of these. Put it where its
question is asked.

| The question it answers | Where it lives |
| --- | --- |
| *How does the system work today?* | the canonical doc for that area, under `docs/` |
| *Why is it this way, and what would change that?* | a record in [docs/decisions/](../docs/decisions/) |
| *What was tried before, and why was it dropped?* | [docs/lessons-and-dead-ends.md](../docs/lessons-and-dead-ends.md) |
| *What do we do next?* | the open-work list (the issue tracker; see [issues.md](./issues.md)) |

Mixing them is the failure mode. A canonical doc that carries a to-do list rots
into a wish list; an archive that carries an instruction gets followed years after
it stopped being true; a backlog that carries a debugging transcript stops being
read at all.

The second row is the one that is easiest to skip and most expensive to lose. The
code records *what* was chosen; nothing in the repository records *why*, or which
reasonable alternative was weighed and rejected, unless someone writes it at the
moment of choosing. A generated map of the codebase cannot recover it, because it
was never in the code. Six months later the artefact looks arbitrary, and somebody
"cleans it up".

So: a decision record is written when a change answers a *why* — a dependency
taken on or dropped, a reasonable alternative rejected, a permanent cost accepted,
an invariant imposed. Not for a bug fix, and not for a refactor that preserves
behaviour: a record per commit is a record nobody reads. The template, and the
rule that records are superseded rather than edited, are in
[docs/decisions/README.md](../docs/decisions/README.md).

A finished investigation therefore *moves*: the working conclusion goes into the
canonical doc, the reasoning behind the choice goes into a decision record, the
abandoned branch goes into the dead-ends doc, and only genuinely open work stays
open.

## Status and replacement

Any long-lived markdown file that is not obviously self-describing carries a
`status` field at the top:

- `active` — current, and a legitimate source for a decision.
- `draft` — research, a proposal, an unverified idea. **Never** cite it as a
  decision.
- `superseded` — replaced by something newer.
- `archived` — historical evidence, kept deliberately, not an instruction.
- `index` — navigation only. An index carries no decisions and must not be read as
  one.

`superseded` and `archived` files **must** carry a `replacement:` or `canonical:`
link to the document that is now correct. A historical file with no forward link is
a trap: it reads as authoritative and there is no way to discover that it is not.

## Mechanics

- **Length is capped, and the cap depends on who reads the file.** A rule file is
  loaded into context at the start of every session; a reference document is read
  once, on purpose. Rule files (`AGENTS.md`, `agents/*.md`, `CONTRIBUTING.md`):
  **250 lines, hard** — past ~230 stop adding sections, past ~245 split before
  writing anything new. Reference documents (`docs/*.md`): **400 lines, hard**,
  with a table of contents required past 250. `README.md` is exempt. The splitting
  rules live in [AGENTS.md](../AGENTS.md) and are not restated here: two copies of
  a rule is how one of them goes stale.
- Filenames are ASCII and topic-based. No dates in filenames — a date belongs in a
  heading or a footer, where it can be corrected.
- When a file moves, update every relative link that pointed at it, then search the
  tree for the old path as a bare string. Delete the directory it left behind if it
  is now empty.
- Do not copy the same evidence into two files. Link to it. Two copies diverge, and
  then neither can be trusted.
- End each document with a footer naming the last edit and its author, in your own
  name — for example:

  ```
  > Last updated: <date> | Editor: <your name> | Change: <one line, what and why>
  ```

  Never overwrite an existing signature with your own, and never adopt someone
  else's.

## Taking in external code or tools

This repository is MIT-licensed and public. Code that arrives from outside carries
both a legal obligation and an execution risk, and both are cheapest to handle
*before* the code is in the tree. In order:

1. **Inventory first.** Check what already exists under `scripts/` and in the test
   helpers. Extending an existing tool beats adding a new one. Every tool is a
   permanent maintenance tax paid by everyone after you.

2. **Search before writing.** If a maintained tool already does the job, use it.
   Writing a worse version of an existing tool from scratch is not thoroughness.

3. **Check the licence *before* taking anything.** Permissive only — MIT, BSD,
   Apache-2.0. If the licence is incompatible, unclear, or absent, **do not take
   the code**: find an alternative or write it yourself. This is not a judgement
   call to be resolved after the code is working, because by then the pressure runs
   the wrong way. Renaming a file does not remove a licence obligation, and taking
   only "the useful part" does not either. Record the source URL, the licence and
   the exact version taken, next to the code.

4. **Review external code line by line before it runs.** Look specifically for
   network calls, data exfiltration, unexpected file writes, and obfuscated or
   encoded blocks. **Never** execute a download-and-run pattern — `curl … | sh`,
   `iwr … | iex`, and every variant of piping a remote script into an interpreter.
   You cannot review what you have not read, and that pattern exists to prevent you
   from reading it.

5. **A tool is not evidence until the tool itself is verified.** This matters most
   for harnesses that emit `pass` / `fail`: a loose match against a log will happily
   report a pass for a run that never happened. Give any pass/fail harness fixture
   tests — including a fixture that *must* fail. An unverified harness is worse than
   no harness, because it is trusted.

6. **Document it or it does not exist.** A tool with no note explaining what it
   does and how to run it is invisible to the next agent, who will write a second
   one.

## Rule changes

Rule files are documents too, and they decay the same way. Changing them is
governed by the tiered authority in [AGENTS.md](../AGENTS.md): mechanical hygiene
is yours to do, an evidence-backed rewrite is allowed if you cite the evidence, and
deleting a rule or touching the protected core needs a human. If you are not sure
which tier applies, it is the highest one.
