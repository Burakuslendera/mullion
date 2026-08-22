# mullion documentation

`mullion` is a Windows/amd64-only, CGo-free Win32 + WebView2 window host,
published as an MIT-licensed Go library. Unsupported Windows and non-Windows
targets remain compile-portable; this folder is the reference set for how the
supported host works, why it is shaped this way, and how a change is proved correct.

New here? [architecture.md](./architecture.md) is the map — read it first. Every
other document answers one narrower question below.

## The documents

| Question | Document |
| --- | --- |
| How does the host work, end to end? | [architecture.md](./architecture.md) |
| Bridge messages, source admission, fallback/frame authority, external WebView routes | [bridge.md](./bridge.md) |
| How do startup show gates and the render watchdog work? | [startup-gates-and-watchdog.md](./startup-gates-and-watchdog.md) |
| How does the host talk to WebView2? | [webview2-and-assets.md](./webview2-and-assets.md) |
| How do WebView2 zoom and native hit-testing stay aligned? | [webview2-zoom-and-native-hit-testing.md](./webview2-zoom-and-native-hit-testing.md) |
| How are assets served without a port? | [assets.md](./assets.md) |
| How are repository guards verified against false success? | [guard-verification.md](./guard-verification.md) |
| What are the guards' exhaustive authorities and proof ceilings? | [guard-authority-details.md](./guard-authority-details.md) |
| Why is the frame / hit-test / DPI code shaped like this? | [frame-and-dpi.md](./frame-and-dpi.md) |
| Canonical native hit-test geometry and issue #113 gates | [hit-test.md](./hit-test.md) |
| Snap, the non-client region and caption behaviour | [snap-and-nonclient-region.md](./snap-and-nonclient-region.md) |
| Where do those snap / non-client claims come from? | [snap-sources.md](./snap-sources.md) |
| What is the headless-versus-live Snap testing boundary? | [snap-testing-boundary.md](./snap-testing-boundary.md) |
| **Why is it done this way, and what would change that?** | [decisions/](./decisions/) |
| What was already tried, and why was it abandoned? | [lessons-and-dead-ends.md](./lessons-and-dead-ends.md) |
| The same question, for what a log line may say | [logging-dead-ends.md](./logging-dead-ends.md) |
| Dated automated/live verification records | [verification-records.md](./verification-records.md) |
| How do I prove a change actually works? | [verification.md](./verification.md) |
| What makes scripted GUI verification lie? | [gui-verification-traps.md](./gui-verification-traps.md) |
| What does a bug report have to contain? | [bug-reports.md](./bug-reports.md) |

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

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: consolidate Bridge protocol and origin-boundary routing to its canonical reference.
