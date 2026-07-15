# 0010. CI requires the WebView2 runtime, so the export check cannot silently skip

**Status:** Accepted

## Context

The host drives the WebView2 runtime's client DLL export
(`CreateWebViewEnvironmentWithOptionsInternal`) directly, with no loader DLL
([0001](./0001-own-webview2-com-layer.md)), and Microsoft documents that entry
point as subject to change.
`TestRuntimeExportsTheEntryPointWeCallDirectly` in `internal/webview2` is the one
test that would notice it vanish: it loads the DLL the host would load and
resolves the symbol.

That test skips when no runtime is installed, as the headless invariant requires
([CONTRIBUTING.md](../../CONTRIBUTING.md)): the suite has to run on any machine,
runtime or not. But a skip is invisible in a green run. On a runner without a
runtime, `go test ./...` reports success while the one check that guards the sole
Known Limitation did nothing — the silent-success class this project calls a
blocker.

Whether that was actually happening was a question about the runner, and it was
measured, not assumed: a non-fatal `mullion doctor` step on `windows-latest`
reported `WebView2 149.0.4022.98 (Evergreen)`, `exports …Internal: yes`. So the
tests were passing, not skipping. Nothing *guaranteed* that, though — a runner
image that dropped the runtime would flip the check to a silent skip with no
signal at all.

## Decision

The windows CI job sets `MULLION_REQUIRE_WEBVIEW2=1`, which turns the two
runtime-dependent tests' "no runtime" skip into a failure. A green windows job
now proves the export was checked. The variable is unset everywhere else, so the
default `go test ./...` still skips when no runtime is present and runs anywhere.

This carves exactly one documented opt-in into the headless invariant: by default
no test requires the runtime; with the variable set, two do. The default — the
suite a contributor runs — is unchanged, which is what the invariant exists to
protect.

## Alternatives rejected

**Gate on `mullion doctor` in CI instead of on the tests.** A CI step that runs
the shipped diagnostic and asserts exit 0 would close the same gap without
touching the headless invariant at all — the tests stay pure skips, and the
environment assertion lives in CI config where environment assertions arguably
belong. It was the tidier option on the invariant. It was rejected so the export
check stays *in the test that documents the contract*, exercised by the test
suite rather than by a parallel path in a YAML file that can drift from it. The
cost — one opt-in in the invariant — is paid once and written down here.

**Install the runtime on the runner unconditionally.** Unnecessary: the runner
already ships it (measured). Adding an Evergreen bootstrapper step would spend
~30–60s per run to guarantee something that is already true. If a future runner
drops the runtime, `MULLION_REQUIRE_WEBVIEW2=1` fails loudly and the install step
is added *then*, against a real need, not a hypothetical one.

**Leave it a skip and rely on the weekly probe.** A non-fatal probe reports but
does not gate; pairing a red informational step with a green required suite is a
mixed signal, and mixed signals decay into ignored ones.

## Consequences

The windows CI job now depends on a runtime being present on the runner. If
GitHub's image ever drops the Evergreen WebView2 Runtime, the job fails until the
runner installs one or the requirement is lifted — a loud, correct failure rather
than a silent pass. That is the whole point, but it is also a new external
dependency of the build, and it is named here so the failure is legible when it
happens.

The headless invariant now has exactly one opt-in, off by default; the suite a
contributor runs on any OS still needs nothing installed. Combined with the
weekly schedule, this is a standing early-warning wire: a runtime or export
regression surfaces within a week even if nobody pushes.

## What would change our mind

- GitHub's runner drops the Evergreen runtime. The requirement then needs an
  explicit install step, or a self-hosted runner that has one; the trade-off in
  the second rejected alternative comes back onto the table.
- Microsoft ships a supported, stable entry point that does not require driving
  the DLL directly ([0001](./0001-own-webview2-com-layer.md),
  [0008](./0008-doctor-is-a-go-command.md)). The export check becomes a
  formality, and the requirement can relax back to a skip.

## Evidence

- `.github/workflows/ci.yml`: the `MULLION_REQUIRE_WEBVIEW2: "1"` env on the
  windows job.
- The mechanism and its lock: `internal/webview2/loader_windows_test.go`,
  `TestRequireWebView2TurnsSkipIntoFailure` — unset skips, set fails, proved with
  a fake `*testing.T` so neither branch has to actually skip or fail the binary.
- The measurement: the `mullion doctor` probe on `windows-latest` reported
  `WebView2 149.0.4022.98 (Evergreen)`, export present, in CI run for the commit
  that added the probe.
- Locally, where a runtime is present, `MULLION_REQUIRE_WEBVIEW2=1 go test ./...`
  passes — the machine tests run rather than skip.
