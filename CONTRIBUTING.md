# Contributing

`mullion` is a Windows-only, CGo-free Win32 + WebView2 window host. Everything in
this file exists to keep two properties true: the test suite runs anywhere, and a
window change is never accepted on the strength of "it compiles".

AI agents working in this repository read [AGENTS.md](./AGENTS.md) first; it
points back here for the mechanics.

## Prerequisites

- A Go toolchain and a Windows 10/11 machine for the full flow.
- The WebView2 Runtime — needed only to run the demo, never to run the tests.
- No C compiler. If a change requires CGo, it is the wrong change.

## The verification ladder

Run these in order. Each one is cheap; each one catches a different class of
mistake. Do not skip to the demo.

```
gofmt -l .                                   # must print nothing
go build ./...
go vet ./...
go test ./...
go test -race ./...                          # the message pump is thread-affine
go build -tags mullion_dwm_caption_diag ./... # diagnostic build tag still compiles
go test -tags mullion_dwm_caption_diag ./...  # ... and its gated tests still pass
go build -tags mullion_caption_passthrough_diag ./... # ... and the other one
go test -tags mullion_caption_passthrough_diag ./...
GOOS=linux go build ./...                     # build-tag hygiene: no Win32 leak
pwsh scripts/leak-scan.ps1                    # nothing private is published
cd examples/basic && go run .                 # live demo
```

`GOOS=linux go build ./...` is not decoration. It is the only automatic check
that a Win32 symbol has not leaked out of a `_windows.go` file into a portable
one. It fails loudly and early; treat a failure as a build-tag bug, not as a
platform complaint.

`go test -race ./...` is the one step that wants a C toolchain: on Windows the
race detector links through a mingw-w64 gcc at test time. That does not soften
the "no C compiler" rule above — the library builds and ships CGo-free — but on
a machine without gcc this step cannot run. Say so in your report rather than
skipping it silently; CI runs it on every push.

## Tests stay headless

**No test may call `Run()`, create an `HWND`, or require a display, and no test
requires the WebView2 Runtime by default.** This is not a style preference — it
is what makes the suite runnable in CI and on a contributor's machine of any OS,
and a test nobody can run is a test that stops being true.

The one opt-in, and only for a machine that is meant to have a runtime:
`MULLION_REQUIRE_WEBVIEW2=1` turns the two runtime-dependent tests in
`internal/webview2` — that a runtime is discoverable, and that it still exports
the entry point the host calls — from *skip when absent* into *fail when absent*.
It is unset by default, so `go test ./...` still runs anywhere with nothing
installed; it is set only where a runtime is guaranteed (CI, whose runner ships
one), so a green windows job proves that export was checked rather than quietly
skipped. Why a skip there is dangerous enough to earn the one exception is in
[docs/decisions/0010](./docs/decisions/0010-ci-requires-the-runtime.md).

The consequence for design: window-affine logic must be factored into pure
functions that take plain values and return decisions. The hit-test resolver, the
`WM_NCCALCSIZE` client-rect computation, the DPI rect transforms, the style-bit
profiles, the asset-path resolution and the bridge dispatch are all shaped this
way precisely so they can be tested without a window. If you find yourself
wanting to spin up a real `HWND` to test something, extract the decision instead.

Behaviour that genuinely cannot be reduced to a pure function — that the frame is
smooth, that Snap Layouts open, that the first painted frame is correct — is not
covered by tests at all. It is covered by the live demo, and it must be stated as
such.

## Every behaviour fix is locked by a test

A fix without a test is a fix that will be undone by the next refactor. Write the
test so that it **fails before your change and passes after it**, and name it
after the contract it locks, not after the function it calls. The DPI
round-trip and no-compounding tests in `monitor_windows_test.go` are the model: they
encode the rule, so a future "simplification" that reintroduces double-scaling
cannot land silently.

When you cannot write a test — the effect is visual, or lives in the window
manager — say so explicitly in the pull request and describe the manual check you
performed instead. Silence is read as coverage.

## "It compiles" is not evidence

Frame, hit-test, DPI, snap and paint behaviour is accepted only after the live
demo checklist in [docs/verification.md](./docs/verification.md) has been run on a
real machine by a human looking at a real window. A green build, a passing test
suite, a correct bounds log and a plausible screenshot of the *code* are not
substitutes. This library's entire failure history is of things that reported
success and rendered nothing; see
[docs/lessons-and-dead-ends.md](./docs/lessons-and-dead-ends.md).

The frame and visual acceptance rules — what counts as proof, and what merely
looks like it — are in [agents/window.md](./agents/window.md). Read them before
touching hit-testing or the non-client area.

## Code style

- `gofmt` is the formatter and the arbiter. No hand-tuned alignment.
- Windows-only code lives in a file ending `_windows.go` **and** carries an
  explicit `//go:build windows` line. Both. The suffix alone is easy to lose in a
  rename, and the tag alone hides the platform from a reader scanning the tree.
- Portable files (`host/config.go`, `internal/logsafe`) must not import the Win32
  shims, must not reference an `HWND`, and must compile on Linux.
- One concern per file, matching the existing layout. When a file grows past
  roughly 250 lines it has usually acquired a second concern; split it there.
- **Source in another language never lives inline in a Go string literal.** A
  document or program — HTML, JavaScript, CSS, anything with a structure of its
  own — gets its own file next to the package and is compiled in with
  `//go:embed`; `host/errorpage.html` and the `host/*.js` scripts are the
  pattern. Genuine fragments (a CSS selector default, a JSON fixture in a test)
  stay inline. The file reads and edits in its own language, and
  `scripts/leak-scan.ps1` holds `.html`/`.js`/`.css` sources to the same ASCII
  rule as `.go`.
- User-supplied strings — filesystem paths, URIs, bridge payloads — pass through
  `internal/logsafe` before they reach a log line. Diagnostics should be readable
  without being a disclosure.
- Exported API changes are a compatibility event: new `Config` fields must have a
  zero value that preserves current behaviour.

## Commits and pull requests

- One concern per commit. Imperative subject line.
- The body says **what changed, why, and what was verified** — which commands from
  the ladder above, which live checks, and, explicitly, what was *not* covered.
- A pull request that changes frame, hit-test, DPI or snap behaviour without a
  live-demo result in its description is incomplete, and will be treated as such.
- Documentation is part of the change, not a follow-up. If a fix teaches the
  project something — a symptom, a root cause, a dead end — it lands in `docs/`
  in the same pull request. Work is not done until it is written down; see
  [agents/notes.md](./agents/notes.md).

## Filing an issue

Issues carry a priority (`P0:`–`P4:`), a type (`bug`, `regression`,
`enhancement`) and at least one area (`area: frame`, `area: webview2`, …). A pull
request carries the priority of the issue it closes.

Two rules are worth knowing before you file:

- **A defect that reports success while doing nothing is `P0: blocker`**, however
  small the fix looks, and it gets the `silent-failure` label. A window that
  paints nothing while every call returns `S_OK` is this library's worst failure
  mode.
- **A frame or DPI report needs its environment**, or it gets `needs-repro`: the
  WebView2 runtime version, the monitor and scaling setup, whether it reproduces
  in `examples/basic` unmodified, and the log — with `MULLION_HITTEST_DIAG=1` for
  a hit-test problem.

The full taxonomy and the triage rules are in
[agents/issues.md](./agents/issues.md).

> Last updated: 2026-07-16 | Editor: Claude (Opus 4.8) | Change: Code style gains the inline-foreign-source rule — a document or program in another language lives in its own file and is embedded, never written inline in a Go string literal (maintainer direction; `host/errorpage.html` and `host/*.js` are the pattern).
