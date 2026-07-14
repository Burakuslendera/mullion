# 0008. The environment report is a Go command, not a script

**Status:** Accepted

## Context

Environment is half of every frame or DPI report, and the tracker refuses to
investigate one without it: `needs-repro` in
[agents/issues.md](../../agents/issues.md) requires the environment block before
an issue can be worked on at all.

The tool that produced that block was `scripts/diagnostics.ps1`, and it could
only be run from a checkout of this repository, with PowerShell. The person
filing a window bug has neither. They have the library — they took it with
`go get` — and a Go toolchain, by definition, and nothing else can be assumed.

The issue template said so, in the same field, twice, and contradicted itself
doing it: *"Do not retype this from memory... a hand-written `1536x864` is the
number a DPI bug report must not contain"*, and then, four lines later, *"No
checkout of mullion? Then paste the environment you can gather by hand."* The
reporter was being asked for exactly the thing the triage rule rejects. The
issue then sat on `needs-repro` forever, because the reporter had no way to
satisfy it.

There is a second gap, and it is the more expensive one. mullion drives the
WebView2 runtime's own client DLL directly — the Evergreen runtime ships no
loader DLL (see [0001](./0001-own-webview2-com-layer.md)) — and Microsoft
documents `CreateWebViewEnvironmentWithOptionsInternal` as subject to change.
`TestRuntimeExportsTheEntryPointWeCallDirectly` pins it, but a test only ever
proves it on the machine that runs the test suite. The machine that matters is
the user's, and **on that machine the question could not be asked at all.** A
registry lookup answers "a runtime is installed", which is not the same question
and never was.

## Decision

The environment report is `mullion doctor`, a Go command under `cmd/mullion`,
and `scripts/diagnostics.ps1` is retired.

It runs with no checkout and no PowerShell:

```
go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor
```

It reports what the script reported — Windows build, GPUs, every monitor with
its physical resolution and scaling, measured with per-monitor DPI awareness
declared first — and then the two things a script could not:

- **which runtime this machine would actually load**, by calling the host's own
  discovery (`webview2.DescribeRuntime`), so a pinned fixed-version runtime is
  never mistaken for the installed Evergreen one;
- **whether that runtime still exports the entry point mullion calls**, by
  loading the client DLL and resolving the symbol. Exit code 0 means mullion can
  start on this machine; 1 means it cannot, and the block says why.

## Alternatives rejected

**Keep the script and translate it to Go later.** The script is good, and two
thirds of what it prints is unchanged here. But it cannot answer the export
question — that requires loading the DLL the host would load, through the code
path the host uses — and it cannot be run by the person who needs it most. A
diagnostic that the reporter cannot run is a diagnostic that produces a
`needs-repro` label and nothing else.

**Keep both.** Two tools that report the same environment diverge, and then
neither can be trusted; [agents/notes.md](../../agents/notes.md) says not to
copy the same evidence into two places, and it is right. The GPU and monitor
sections of the two tools were compared, line by line, on a two-monitor mixed-DPI
machine and matched exactly before the script was removed.

**Put the export probe in the command instead of in `internal/webview2`.** It
would have meant a second copy of the export name and the DLL search flags. The
day the host changes which entry point it calls, that copy keeps checking the
old one and cheerfully reports success — a diagnostic that reports success while
doing nothing, which is the failure class this project treats as a blocker. The
probe therefore lives beside the loader and calls `loadClient`, the same
function the host calls at startup.

**Export a `mullion.Diagnose()` from the library.** It would put the report in
the public API, where it becomes a compatibility promise, for the benefit of a
use case nobody has asked for. `internal/doctor` costs nothing to change.

## Consequences

The repository now ships a binary. `cmd/mullion` is not part of the library's
API, but once published it is a promise: the sub-command names and the shape of
the block are what a reporter, an issue template and any script that parses the
exit code will depend on. Changing them is a user-visible change, and gets the
same care as an API change.

`go run` stamps no VCS information — only `go build`, `go install`, or an
explicit `go run -buildvcs=true` do — so from a checkout the version line reads
`devel` with no revision behind it. The command says so, in the report, and says
what to run instead. It does not shell out to `git` to paper over it: the
version is read from the binary's own build info, which is the whole reason it
can be trusted (`version.go`).

The probe loads the WebView2 client DLL into the process that runs it. That
starts no browser and creates no window, but it is a native library load, and it
is why `doctor` is a separate short-lived command rather than something the host
does at startup.

## What would change our mind

- Microsoft ships a supported, documented entry point that does not require the
  loader DLL. The export check then stops being a diagnosis of a known risk and
  becomes a formality, and most of the reason this is a Go command rather than a
  script goes with it.
- A `go`-free installation becomes a normal way to consume mullion (a released
  binary, a package manager). The reporter then no longer has a Go toolchain by
  definition, and the assumption this decision rests on is gone.

## Evidence

- The self-contradiction it removes: `.github/ISSUE_TEMPLATE/bug_report.yml`, the
  Environment field, before this change.
- `internal/webview2/diagnose_windows.go`, and
  `TestDescribeRuntimeCannotBeSilentAboutTheExport`.
- The export-missing path was exercised, not assumed: a real DLL with no such
  export was pinned through `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER`, and the command
  reported `exports CreateWebViewEnvironmentWithOptionsInternal: NO` and exited 1.
- `internal/doctor/doctor_test.go`, including
  `TestFormatNeverPrintsTheHomeDirectory` — the first live run of the command
  printed a user's 8.3 short path (`...~1`) into a block written to be pasted in
  public, and the redaction now covers both spellings Windows hands out.
