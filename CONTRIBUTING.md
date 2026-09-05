# Contributing

`mullion` is a Windows/amd64-only, CGo-free Win32 + WebView2 window host. Everything
in this file exists to keep two properties true: the test suite runs anywhere, and
a window change is never accepted on the strength of "it compiles".

AI agents working in this repository read [AGENTS.md](./AGENTS.md) first; it
points back here for the mechanics.

## Prerequisites

- Go 1.24 or newer, and a Windows 10/11 amd64 machine for the full flow. 1.24 is
  the supported floor and an invariant, not an accident: nothing here may use a
  standard-library symbol or language feature newer than it, whatever you have
  installed. It is 1.24 because that is where `os.OpenRoot` arrives.
  [decisions/0042](docs/decisions/0042-go-1-24-remains-the-released-consumer-floor.md).
- The WebView2 Runtime is optional for the default suite, but required to run the
  demo and the opt-in `GATE-WEBVIEW2-MACHINE` lane selected by
  `MULLION_REQUIRE_WEBVIEW2=1`. Both Windows CI lanes set that requirement.
- The library builds and ships CGo-free, so its normal build needs no C compiler.
  Local Windows `-race` testing is separate: Go's race detector needs a
  mingw-w64 `gcc`.

## The verification ladder

Run the canonical [automated gate matrix](./docs/verification/automated-gates.md#automated-gates)
in order. It owns the executable commands, fail-fast behavior, stable
`GATE-*` IDs, diagnostic pairs, machine lanes, cross-target cleanup, and live
demo. Do not copy the ladder into a contribution or maintain a second command
table.

The matrix's Windows/amd64 lane is the supported WebView2 hosting target.
Windows/386 executes the unsupported-architecture gates under WOW64, and
Windows/ARM64 is compile-only.
The opt-in `GATE-WEBVIEW2-MACHINE` lane requires
`MULLION_REQUIRE_WEBVIEW2=1` and a real Runtime, and runs exactly three real
discovery/report checks. `TestLoadClientFreesTheModuleWhenTheExportIsMissing`
is a deterministic production cleanup seam in default `GATE-TEST`; real Windows
`LoadLibrary`/`GetProcAddress`/`FreeLibrary` behavior is not covered.

The race lane is test-only and needs a mingw-w64 `gcc` on Windows. The library
still builds and ships CGo-free. If that toolchain is unavailable, report the
race lane as not run rather than silently omitting it; CI runs it on every push.

## Tests stay headless

**No default test may create or destroy an `HWND` or window class, require a
display, inspect or mutate desktop/monitor/input/shell state, enter native COM,
call a Runtime-owned interface, activate the shell or browser, dispatch a
native message loop, or require the WebView2 Runtime.** The closed native
boundary in [the evidence boundary](./docs/verification/evidence.md) is
exhaustive: only bounded test-owned ABI memory, valid Go-owned callback/vtable
fixtures, deterministic injected effect seams, and the named isolated `WM_QUIT`
self-test may cross; every other Win32 entry point and indirect call remains
forbidden.

A test may call public `Run()` only through a deterministic seam or build path
that proves return before runtime discovery and every native boundary, with
assertions that the forbidden seams were not reached. Choosing an input that is
merely expected to return early is not proof. This narrow exception preserves
public preflight coverage without weakening headless CI
([decision 0039](./docs/decisions/0039-public-run-preflight-stays-headless.md)).
Window-affine logic must be factored into pure functions that take plain values
and return decisions; use those functions and existing production seams rather
than spinning up a real `HWND`.

## Every behaviour fix has a contract proof

Every behaviour fix MUST have a focused headless regression for each
deterministic observable contract. Only an independently approved,
irreducibly visual, window-manager, shell, or compositor residual may use
applicable live evidence instead. The contract partition, production path,
test or approved residual, live artifact/environment, exact uncovered boundary,
and uncertainty label are mandatory in the report. “Hard to test” is not an
exception. See [the evidence boundary](./docs/verification/evidence.md).

## "It compiles" is not evidence

Frame, hit-test, DPI, Snap and paint behavior still require the applicable live
demo checklist in
[acceptance-checklist.md](./docs/verification/acceptance-checklist.md#manual-acceptance-checklist)
on a real machine by a human looking at a real window. Live evidence is
additional to headless regressions for every reducible contract; it does not
replace them. A green build, passing test suite, bounds log, or screenshot of
code is not visual acceptance. This library's failure history is of things that
reported success and rendered nothing; see
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
- **Source in one language never lives inline inside a file of another — in
  any direction.** Not HTML in a Go string, not C# in a PowerShell here-string,
  not a script block in YAML: a document or program gets its own file, and the
  host file loads it — `//go:embed` in Go (`host/errorpage.html`, `host/*.js`),
  `Get-Content -Raw` in PowerShell (`scripts/screenshot.cs`). Genuine fragments
  stay inline: a CSS selector default, a one-line style string, a
  `"<html></html>"` test fixture. A document composing what its own standard
  defines — HTML carrying its `<style>` block — is one document, not a
  violation. `scripts/leak-scan.ps1` holds every source extension to the same
  ASCII rule as `.go`.
- User-supplied strings — filesystem paths, URIs, bridge payloads — pass through
  `internal/logsafe` before they reach a log line, and through the reducer that
  fits the input. A URI takes `logsafe.URL`, never `logsafe.FileName`: `URL`
  bounds the whole value and refuses to print a host it cannot print in full
  (decisions/0025). A sentence that *contains* a URI takes `logsafe.Message`,
  which finds the URLs inside it (decisions/0028). Diagnostics should be
  readable without being a disclosure.
- `github.com/Burakuslendera/mullion/host` is a released consumer surface. Before
  changing an exported declaration, `Config` field, interface or callback
  signature, sentinel error/`errors.Is` identity, import path, documented
  default/precedence, or callback contract, compare the `host` API on Windows and
  non-Windows with the latest released tag and record the result in the pull
  request. Cover zero, negative, and opt-in semantics; `host/api_contract.go`
  checks only build-variant parity, not this release check. Clean cutover applies
  only to internal/unexported or unreleased paths. An intentional incompatibility
  requires an explicit compatibility/migration decision and release note.

## Commits and pull requests

- One concern per commit. Imperative subject line.
- The body says **what changed, why, and what was verified** — name the
  applicable `GATE-*` IDs, checklist scenario IDs, and explicitly what was not
  covered.
- A pull request that changes frame, hit-test, DPI or Snap behavior must include
  the applicable live-demo result and the contract-partitioned evidence required
  by [the evidence boundary](./docs/verification/evidence.md); missing a
  deterministic proof or an approved residual record is incomplete.
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

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: align the contributor machine lane with three Runtime checks and classify the missing-export cleanup seam as default headless evidence.
