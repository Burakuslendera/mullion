# mullion documentation

`mullion` is a Windows-only, CGo-free Win32 + WebView2 window host, published as an
MIT-licensed Go library. This folder is its reference set: how the host works, why it
is shaped this way, and how a change to it is proved correct.

New here? [architecture.md](./architecture.md) is the map — read it first. Every
other document answers one narrower question below.

## The documents

| Question | Document |
| --- | --- |
| How does the host work, end to end? | [architecture.md](./architecture.md) |
| Why is the frame / hit-test / DPI code shaped like this? | [frame-and-dpi.md](./frame-and-dpi.md) |
| Snap, the non-client region and caption behaviour | [snap-and-nonclient-region.md](./snap-and-nonclient-region.md) |
| Where do those snap / non-client claims come from? | [snap-sources.md](./snap-sources.md) |
| **Why is it done this way, and what would change that?** | [decisions/](./decisions/) |
| What was already tried, and why was it abandoned? | [lessons-and-dead-ends.md](./lessons-and-dead-ends.md) |
| How do I prove a change actually works? | [verification.md](./verification.md) |

## Why the decision records matter

A generated map of the repository — or the code itself — tells you *what* the
architecture is. Only a [decision record](./decisions/) tells you *why*, which
alternatives were weighed, and what it would take to change one. Reversing a decision
without knowing what it was protecting is the most expensive mistake available here,
and it looks like a cleanup while you are doing it. Start at the
[decisions index](./decisions/README.md).

## For contributors and agents

The build, test and pull-request mechanics are in
[CONTRIBUTING.md](../CONTRIBUTING.md). An AI agent reads [AGENTS.md](../AGENTS.md) and
[agents/](../agents/) first. Whichever you are: read the document for the subsystem
you are about to touch before you change it, and read its decision record before you
change *why* it works that way.

> Last updated: 2026-07-15 | Editor: Claude (Opus 4.8) | Change: add a docs/ index so the folder view lands on a readable map instead of a bare file list.
