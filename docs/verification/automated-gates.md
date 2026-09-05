# Automated gates

Status: active

Run the command matrix in this order. The `GATE-*` identifiers are stable
references for reports and CI; this file is the only human-facing command
matrix. Every native build, test, and run is independently checked. A non-zero
`$LASTEXITCODE` aborts the ladder, and commands are not chained with `;`.

## Gate script
### Environment isolation

The default gates, including `GATE-TEST`, run with
`MULLION_REQUIRE_WEBVIEW2` absent. The machine gate is genuinely opt-in: it runs
only when the caller initially supplied `MULLION_REQUIRE_WEBVIEW2=1`; otherwise
it is documented as `not run / unverified`, not as a pass. Its inner `finally`
always removes the variable, preventing the three opt-in machine tests from
leaking into later full/tagged suites. Subsequent gates run without the
machine-test requirement variable; `GATE-DOCTOR` remains real Runtime/export
discovery and `GATE-LIVE-DEMO` remains a live native launch. Only the outer
`finally` restores the caller's original process value after the ladder exits.

```powershell
$hadRequireWebView2 = Test-Path Env:MULLION_REQUIRE_WEBVIEW2
$priorRequireWebView2 = $env:MULLION_REQUIRE_WEBVIEW2
$machineOptIn = $hadRequireWebView2 -and $priorRequireWebView2 -eq '1'
try {
    Remove-Item Env:MULLION_REQUIRE_WEBVIEW2 -ErrorAction SilentlyContinue
    function Invoke-CheckedNative {
        param([string]$Name, [scriptblock]$Command)
        & $Command
        if ($LASTEXITCODE -ne 0) { throw "$Name failed with exit code $LASTEXITCODE" } }
    # GATE-FORMAT
    Invoke-CheckedNative 'gofmt' {
        $gofmtOutput = gofmt -l .
        if ($gofmtOutput) { throw 'gofmt reported unformatted files' }
    }
# GATE-BUILD
Invoke-CheckedNative 'go build' { go build ./... }
# GATE-VET
Invoke-CheckedNative 'go vet' { go vet ./... }
# GATE-TEST
Invoke-CheckedNative 'go test' { go test -count=1 ./... }
# GATE-RACE
Invoke-CheckedNative 'go test -race' { go test -count=1 -race ./... } # test-only CGo-enabled lane
# GATE-BRIDGE
Invoke-CheckedNative 'bridge test' { node scripts/test-bridge.mjs }   # exact embedded bridge in a Node VM
    # GATE-WEBVIEW2-MACHINE (run only when initially opted in)
    if ($machineOptIn) {
        try {
            $env:MULLION_REQUIRE_WEBVIEW2 = '1'
            Invoke-CheckedNative 'WebView2 machine discovery/report tests' {
                go test -count=1 ./internal/webview2 -run '^(TestFindRuntimeOnThisMachine|TestRuntimeExportsTheEntryPointWeCallDirectly|TestDescribeRuntimeCannotBeSilentAboutTheExport)$'
            }
        } finally {
            Remove-Item Env:MULLION_REQUIRE_WEBVIEW2 -ErrorAction SilentlyContinue
        }
    } else {
        Write-Host 'GATE-WEBVIEW2-MACHINE not run / unverified: caller did not opt in with MULLION_REQUIRE_WEBVIEW2=1'
    }
# GATE-DOCTOR
Invoke-CheckedNative 'go run doctor' { go run ./cmd/mullion doctor }   # direct runtime/export execution
# GATE-DIAG-DWM-BUILD
Invoke-CheckedNative 'dwm diagnostic build' { go build -tags mullion_dwm_caption_diag ./... }
# GATE-DIAG-DWM-TEST
Invoke-CheckedNative 'dwm diagnostic test' { go test -count=1 -tags mullion_dwm_caption_diag ./... }
# GATE-DIAG-CAPTION-PASSTHROUGH-BUILD
Invoke-CheckedNative 'caption passthrough diagnostic build' { go build -tags mullion_caption_passthrough_diag ./... }
# GATE-DIAG-CAPTION-PASSTHROUGH-TEST
Invoke-CheckedNative 'caption passthrough diagnostic test' { go test -count=1 -tags mullion_caption_passthrough_diag ./... }
# GATE-DIAG-SCRIPT-COMPLETION-BUILD
Invoke-CheckedNative 'script completion delay diagnostic build' { go build -tags mullion_script_completion_delay_diag ./... }
# GATE-DIAG-SCRIPT-COMPLETION-TEST
Invoke-CheckedNative 'script completion delay diagnostic test' { go test -count=1 -tags mullion_script_completion_delay_diag ./... }
try { $env:GOARCH = 'amd64'
    $env:GOOS = 'linux'
    # GATE-PORTABLE-LINUX-AMD64
    Invoke-CheckedNative 'linux/amd64 build' { go build ./... } # stub portability
    $env:GOOS = 'windows'
    # GATE-SUPPORTED-WINDOWS-AMD64-BUILD
    Invoke-CheckedNative 'windows/amd64 build' { go build ./... } # supported WebView2 target
    $env:GOARCH = '386'
    # GATE-UNSUPPORTED-WINDOWS-386-TEST
    Invoke-CheckedNative 'windows/386 test' { go test -count=1 ./internal/webview2 ./internal/doctor ./host -run '^TestUnsupportedArchitecture' } # WOW64 execution
    $env:GOARCH = 'arm64'
    # GATE-PORTABLE-WINDOWS-ARM64-BUILD
    Invoke-CheckedNative 'windows/arm64 build' { go build ./... } # compile portability only
} finally { Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue }
# GATE-LEAK-SCAN
Invoke-CheckedNative 'leak scan' { pwsh scripts/leak-scan.ps1 } # configured shapes in tracked text and complete reachable history
# GATE-LIVE-DEMO
$demoExitCode = 1
try {
    Push-Location examples/basic
    go run .
    $demoExitCode = $LASTEXITCODE
} finally { Pop-Location }
if ($demoExitCode -ne 0) { throw "go run failed with exit code $demoExitCode" } # live demo
} finally {
    if ($hadRequireWebView2) {
        $env:MULLION_REQUIRE_WEBVIEW2 = $priorRequireWebView2
    } else {
        Remove-Item Env:MULLION_REQUIRE_WEBVIEW2 -ErrorAction SilentlyContinue
    }
}
```

The demo captures `go run`'s status before `Pop-Location` restores the working
directory. The final `Remove-Item` restores native builds even when a
cross-target command fails. The Linux build proves that a Win32 symbol has not
leaked out of a `_windows.go` file; Windows/amd64 is the supported WebView2
hosting target, Windows/386 executes the unsupported-architecture gates under
WOW64, and Windows/ARM64 is compile-only. None of those cross-target gates
proves WebView2 hosting.


CI MUST set `CGO_ENABLED=0` on production build/test lanes. The race gate is
an uncached, test-only CGo-enabled lane and is not shipping evidence.

## Gate coverage

| ID | Command or lane | What it catches |
| --- | --- | --- |
| `GATE-FORMAT` | `gofmt -l .` | Formatting drift; non-empty output fails. |
| `GATE-BUILD` | `go build ./...` | Compile errors on the default Windows path. |
| `GATE-VET` | `go vet ./...` | Suspicious syscall `unsafe.Pointer` use, printf verbs, and struct tags. |
| `GATE-TEST` | `go test -count=1 ./...` | Uncached pure-logic invariants: issue #112's exact `int64` ceiling scaling over signed inputs/maximum DPI and zero-DPI fallback; preservation of positive `MaxInt32` Config metrics; invalid, outside-endpoint and full-signed-span half-open rects; clipped wrap bands and caption-button thirds; independent midpoint resize saturation/non-overlap; all corner/edge, controls, caption, client and maximized results; active-profile and in-process/no-shell maximized behavior; and bounded/invalid diagnostics from the production geometry constructor. It also covers non-client rect adjustment, style-bit composition, asset name-to-MIME mapping, diagnostic log parsing, **every COM vtable offset and IID in `internal/webview2`**, `TestNoNetworkListener` (fail-closed source/fixture endpoint guard), `TestNoUpstreamBrandLeak`, `TestNoNonASCIIInSource`, process-global run-token isolation, Logger re-entry teardown coverage, and the deterministic production cleanup seam `TestLoadClientFreesTheModuleWhenTheExportIsMissing`. Real Windows `LoadLibrary`/`GetProcAddress`/`FreeLibrary` behavior is not covered. |
| `GATE-RACE` | `go test -count=1 -race ./...` | Uncached race coverage for timer, callback, and shared-state seams; it does not run a real message pump or prove UI-thread scheduling. |
| `GATE-BRIDGE` | `node scripts/test-bridge.mjs` | Exact embedded bridge bytes in a dependency-free Node VM. |
| `GATE-WEBVIEW2-MACHINE` | Required Runtime block above | Exactly three real Runtime discovery/report tests, only when the caller initially opts in with `MULLION_REQUIRE_WEBVIEW2=1`; otherwise `not run / unverified`, not pass. |
| `GATE-DOCTOR` | `go run ./cmd/mullion doctor` | Direct uncached runtime/export discovery and resolution; it does not prove hosting, rendering, or a visible loaded window. |
| `GATE-DIAG-DWM-BUILD` / `GATE-DIAG-DWM-TEST` | `-tags mullion_dwm_caption_diag` | Keeps the DWM/caption diagnostic build and its tests compilable and executable. |
| `GATE-DIAG-CAPTION-PASSTHROUGH-BUILD` / `GATE-DIAG-CAPTION-PASSTHROUGH-TEST` | `-tags mullion_caption_passthrough_diag` | Keeps the caption passthrough diagnostic build and tests live. |
| `GATE-DIAG-SCRIPT-COMPLETION-BUILD` / `GATE-DIAG-SCRIPT-COMPLETION-TEST` | `-tags mullion_script_completion_delay_diag` | Keeps the bounded Issue #135 diagnostic build and tests live; the tag is not a release artifact. |
| `GATE-PORTABLE-LINUX-AMD64` | `GOOS=linux GOARCH=amd64 go build ./...` | Portable stub compilation. |
| `GATE-SUPPORTED-WINDOWS-AMD64-BUILD` | `GOOS=windows GOARCH=amd64 go build ./...` | The supported Windows target's compilation. |
| `GATE-UNSUPPORTED-WINDOWS-386-TEST` | `GOOS=windows GOARCH=386 go test ... -run '^TestUnsupportedArchitecture'` | WOW64 execution of production rejection before Runtime, DLL, COM, callback, class, or HWND work. |
| `GATE-PORTABLE-WINDOWS-ARM64-BUILD` | `GOOS=windows GOARCH=arm64 go build ./...` | Compile portability only. |
| `GATE-LEAK-SCAN` | `pwsh scripts/leak-scan.ps1` | A fail-closed configured-shape scan of every selected stage-0 index entry and each distinct safely resolved worktree revision for indexed paths, with the ordinary unstaged-deletion exception only when its bound `HEAD` mode/object identity proves it; reachable commit messages from a valid, non-shallow `HEAD` are scanned separately. Staged deletions, old `HEAD`-only file blobs, untracked files, binary payloads, unreachable commits, encoded/obfuscated values and unknown secret classes are not covered. See [guard-verification.md](../guard-verification.md) for the exact source ceiling. |
| `GATE-LIVE-DEMO` | `go run .` in `examples/basic` | Status-preserving live demo launch; launch alone is not proof of rendering or frame behavior. |

`GATE-TEST`'s source guard is deliberately narrow. `TestNoNetworkListener`
parses every Go file regardless of build tag; resolves prohibited APIs, all
supported Winsock loaders, named and unnamed conversions, local and cross-file
package type aliases, and parenthesized string conversions; and scans Go strings
plus shipped raw text for bounded standalone, scheme, and scheme-relative
authority endpoints with IPv6 path/userinfo controls. The real named guard runs
temporary modules. Exact fixtures and the case-folded intercepted host have
rule-specific exceptions; comments, run-time assembly, reflection, raw numeric
syscalls, and dependencies remain outside this source proof.

`TestNoUpstreamBrandLeak` rejects configured forbidden-reference needles in
tracked text extensions, and `TestNoNonASCIIInSource` rejects non-ASCII code
points in Go source. The run-token gates use process-global non-zero Run
identities so a stale private command from one Host is not admitted by another
Host when a deterministic seam supplies the same `HWND`. The Logger gate proves
a callback that re-enters `Quit` while teardown waits for the outer `Hide` still
admits/posts both commands and completes without deadlocking Run admission.

Diagnostic tags require both their build and uncached test pair. Environment
switches remain default-off and are documented in
[diagnostics.md](./diagnostics.md); this matrix does not turn them on.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: bound the leak-scan description to selected stage-0 index and safe worktree revisions, proved ordinary unstaged deletions, separate commit messages, and explicit noncoverage.
