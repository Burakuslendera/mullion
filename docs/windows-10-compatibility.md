# Windows 10 x64 compatibility research

**Status:** draft — research and recommendation for [issue #132](https://github.com/Burakuslendera/mullion/issues/132), not an accepted support decision.

This record separates the operating-system floor imposed by Mullion's current
native calls from the narrower release promise proposed for support. It does not
claim that a Windows 11 development machine, a cross-build, or a headless test
has proved that a release works on Windows 10.

## Contents

- [Recommendation pending issue #132](#recommendation-pending-issue-132)
- [Observed repository boundary](#observed-repository-boundary)
- [Win11-built executable on Windows 10](#win11-built-executable-on-windows-10)
- [WebView2 Runtime deployment and capabilities](#webview2-runtime-deployment-and-capabilities)
- [DWM and Windows 11-only presentation](#dwm-and-windows-11-only-presentation)
- [Current limitations and release conditions](#current-limitations-and-release-conditions)

## Recommendation pending issue #132

**Recommend the compatibility-test baseline as Windows 10 22H2, build family
19045, x64, with the latest applicable cumulative or ESU update and a supported
WebView2 Runtime.** This is a recommendation for a repeatable compatibility
target, not a claim that Windows 10 itself remains in general support and not a
commitment until issue #132 decides it.

The technical API floor is lower: Mullion calls
[`SetProcessDpiAwarenessContext`](https://learn.microsoft.com/windows/win32/api/winuser/nf-winuser-setprocessdpiawarenesscontext)
with `PER_MONITOR_AWARE_V2`. Microsoft lists that API's minimum desktop client as
Windows 10 version 1703. The host treats a missing or failed PMv2 setup as a
startup error, rather than silently falling back to DPI-unaware coordinates
([`host/dpi_windows.go`](../host/dpi_windows.go) and
[`host/host_windows.go`](../host/host_windows.go)). Therefore Windows 10 1703 is
the lowest release that can plausibly satisfy today's native API path; it is not
evidence that the whole host has been tested there.

Microsoft's direct [Edge and WebView2 supported-operating-systems page](https://learn.microsoft.com/en-us/deployedge/microsoft-edge-supported-operating-systems)
lists Windows 10 SAC 1709 and later, plus named LTSC editions. That external
runtime statement and the PMv2 API floor do not establish Mullion's own window,
COM, DPI, or renderer compatibility. Choosing 22H2/19045 avoids turning every
historical Win10 release into a separate support promise while retaining one
repeatable, x64 Win10 baseline.

The lifecycle distinction is material. Microsoft's [Windows 10 lifecycle
notice](https://learn.microsoft.com/en-us/lifecycle/announcements/windows-10-end-of-support)
states that Windows 10 22H2 reached general end of support on 2025-10-14; that
is an operating-system security/support fact, not a native API result. Separately,
Microsoft states that Edge and the WebView2 Runtime will continue to receive
updates on Windows 10 22H2 until **at least October 2028**, without requiring
the Extended Security Updates program. That is a WebView2/Edge update horizon,
not a restoration of Windows 10 general support. Issue #132 must choose and name
which promise Mullion makes: tested compatibility on this retired OS baseline,
or a stronger ongoing support/security commitment that the project can actually
maintain.

## Observed repository boundary

The following are **observed from the current source and CI configuration**:

- WebView2 hosting is supported only for a `windows/amd64` process. The handwritten
  COM call encodings depend on the Windows x64 ABI; see
  [decision 0034](./decisions/0034-webview2-hosting-is-windows-amd64-only.md).
- A Windows/386 process is intentionally rejected before DPI setup, runtime
  discovery, DLL loading, COM, callbacks, class registration, or `HWND` creation.
  CI executes those production rejection paths under WOW64.
- Windows/ARM64 remains compile-portable only. It is not a runtime-support claim.
  Non-Windows builds receive the API-compatible unsupported-platform stub.
- CI builds and tests `windows/amd64` on `windows-latest`, executes the focused
  Windows/386 rejection checks, and cross-compiles Windows/ARM64, Linux/amd64,
  and Darwin/ARM64. The exact matrix is in
  [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) and the contract is
  described in [CONTRIBUTING.md](../CONTRIBUTING.md).
- The normal suite is deliberately headless. It does not create a window, embed a
  control, or render a first frame; therefore it cannot prove Win10 shell, DPI,
  DWM, COM scheduling, or rendering behaviour.

`GOOS=windows GOARCH=amd64 go build` proves source compilation for the Windows
x64 target. It does not test a Windows 10 loader, system DLL export, WebView2
runtime, GPU driver, or a real `HWND`. A successful Win11 CI build is similarly
not Win10 live evidence.

## Win11-built executable on Windows 10

**Unverified live behaviour:** an executable built on Windows 11 for
`windows/amd64` should be tested unchanged on the clean Win10 target; rebuilding
on Win10 is not a substitute. The binary can start only if all of these hold:

1. the target is an x64 Windows process and supports the native API path,
   especially PMv2 DPI;
2. a usable x64 WebView2 Runtime is discoverable, or a validated Fixed Version
   directory is explicitly pinned;
3. `EmbeddedBrowserWebView.dll` exports
   `CreateWebViewEnvironmentWithOptionsInternal`, which Mullion calls directly;
4. the app can create and use its WebView2 user-data directory; and
5. environment, controller, event registration, navigation, and rendering all
   succeed against that OS/runtime/GPU combination.

The first three conditions are partly diagnosable today: `mullion doctor` reports
the selected runtime and the required export. It intentionally does **not** claim
that environment/controller creation, window creation, or rendering will work;
see [the doctor contract](../README.md#diagnostics). The last two conditions need
a live window.

### Build modes, release provenance, and CPU baseline

#### Ordinary native development

On an x64 Windows development machine, a consumer building or running Mullion
from an IDE normally needs no target environment assignments. Go's native
defaults already select the host target, so ordinary development can use the
IDE's normal build/run command or, from PowerShell:

```powershell
go build .\examples\basic
go run .\examples\basic
```

Those defaults make development convenient; they are not a release provenance
record. In particular, an IDE build does not by itself document the selected Go
toolchain, CGo setting, CPU baseline, source revision, or artifact hash. Do not
ask consumers to persist release settings with `go env -w`: that changes later,
unrelated Go commands in the user's environment and is not part of Mullion's
library contract.

#### Controlled release or compatibility artifact

Build one release candidate on Windows 11 and carry that exact artifact into the
clean Windows 10 VM. The intended x64 command must explicitly keep the CGo-free
and broad CPU baseline. Run the following from the intended clean checkout in a
new PowerShell session; the assignments affect only that PowerShell process:

```powershell
$status = git status --short
if ($status) { throw 'release build requires a clean checkout' }
git rev-parse HEAD
go version

New-Item -ItemType Directory -Force .\dist | Out-Null
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:GOAMD64 = 'v1'

$out = Join-Path (Get-Location) 'dist\app.exe'
go build -trimpath -buildvcs=true -o $out .\cmd\your-app
Get-FileHash $out -Algorithm SHA256
go version -m $out

Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH, Env:GOAMD64
```

Replace `./cmd/your-app` with the actual consumer entry point; the important
provenance contract is that the Win10 VM receives the resulting `app.exe`, not a
binary rebuilt inside the VM. Record the `git rev-parse HEAD` result, `go version`,
`go version -m .\dist\app.exe`, and the SHA-256 above. `-trimpath` avoids embedding
the local source path; `-buildvcs=true` embeds usable source-control metadata when
the checkout and Go toolchain can provide it. Exact hashes still require the same
source, Go version, flags, and other build inputs, so a matching hash is transfer
evidence rather than a general reproducibility guarantee.

The explicit `CGO_ENABLED=0` makes the published artifact independent of a local
C compiler, matching Mullion's CGo-free design. It does not make Windows race
tests CGo-free: `go test -race` still needs mingw-w64 `gcc`. The explicit
`GOOS`/`GOARCH` prevent a release from silently inheriting an unintended target.
After transfer, run `Get-FileHash` against the received file on Windows 10 and
compare it to the recorded value before executing it. Also inspect the PE machine
type and import table where available (for example, `dumpbin /headers /imports
.\dist\app.exe`). The PE import report is evidence of what the loader must resolve;
it is not evidence that delayed `LazyProc` calls, WebView2, or rendering work.

[`GOAMD64=v1` is Go's default amd64 baseline](https://go.dev/wiki/MinimumRequirements)
and generates instructions all 64-bit x86 processors can execute. `v2` adds
SSE3 and other instructions; `v3`/`v4` add further CPU requirements. Go checks a
requested microarchitecture at startup, so a `GOAMD64=v2+` release can fail on an
older x64 CPU before Mullion has a chance to diagnose WebView2. Do not raise this
baseline without a separate, explicit CPU-support policy.

There is an independent browser-runtime caveat: Microsoft's combined
[Edge/WebView2 supported-OS page](https://learn.microsoft.com/en-us/deployedge/microsoft-edge-supported-operating-systems)
says that Microsoft Edge version 128 stops supporting CPUs without SSE3. The
wording names Edge, not a separately measured WebView2 Runtime failure, so the
exact WebView2 effect on an SSE3-less Win10 CPU is **unverified** here. The safe
release condition is to require SSE3 for the WebView2-capable baseline, or to
obtain and record a real runtime result on such a CPU; a Go `v1` binary alone
does not settle that dependency.

### Required clean-VM proof

Before publishing the recommendation as a support promise, run the release-like
x64 executable in a clean Windows 10 22H2/19045 VM with no developer WebView2
artifacts. Record the exact OS build/revision, architecture, GPU driver, runtime
distribution and version, and Mullion build identity. At minimum, prove:

1. `mullion doctor` finds the expected runtime and required export;
2. a normal `Host.Run` creates the environment, controller, window, and first
   rendered document, then closes cleanly;
3. startup diagnostics contain no unexpected native/runtime failure;
4. one single-DPI launch and a mixed-DPI transition (100% to 125% or 150%) keep
   the window and WebView bounds aligned; and
5. the same checks run once with Evergreen and once with the chosen Fixed Version
   runtime, if both are promised.

The visual, DPI, and Snap portions require the live checklist in
[acceptance-checklist.md](./verification/acceptance-checklist.md#manual-acceptance-checklist); a screenshot or a successful process exit
alone is insufficient.

## WebView2 Runtime deployment and capabilities

Microsoft requires a WebView2 Runtime on the client for both
[Evergreen and Fixed Version distribution](https://learn.microsoft.com/microsoft-edge/webview2/concepts/distribution).
Evergreen is shared and automatically updated, but Microsoft explicitly says a
small number of Windows 10 machines may lack it and recommends installation or a
user-facing recovery path. Managed policies can also leave a client behind a
current runtime, so feature detection remains necessary. Fixed Version packages a
specific runtime with the app and gives reproducibility, but its servicing and
security updates become the application's responsibility; see
[Evergreen versus Fixed Version](https://learn.microsoft.com/microsoft-edge/webview2/concepts/evergreen-vs-fixed-version).

**Observed local implementation:** runtime discovery accepts an Evergreen
registry registration or the `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` override; a
pinned folder wins over discovery. It selects only `EBWebView\\x64` for an amd64
process and checks the required direct export before creation. It has no Mullion
minimum-runtime-version policy beyond that export; see
[`internal/webview2/loader_discovery_windows.go`](../internal/webview2/loader_discovery_windows.go)
and [`loader_client_windows.go`](../internal/webview2/loader_client_windows.go).

The direct client-DLL route bypasses the SDK loader's version gate. Runtime
features are therefore checked by `QueryInterface`, not guessed from a version:

- missing `ICoreWebView2Controller3` is a warning path; it weakens Mullion's
  explicit raw-pixel/rasterization-scale policy and is especially risky on a
  mixed-DPI setup;
- background colour uses `ICoreWebView2Controller2` and is also warned rather
  than fatal when unavailable; and
- non-client-region support is capability-detected through Settings9. The source
  records that older runtimes can return `E_NOINTERFACE` cleanly.

These are intentional degradation paths, not proof of acceptable visual parity.
The [WebView2 development guidance](https://learn.microsoft.com/microsoft-edge/webview2/concepts/developer-guide)
also recommends feature detection because Evergreen clients can be delayed by
policy or update state. A Fixed Version runtime should be tested and recorded by
exact version, then updated deliberately.

## DWM and Windows 11-only presentation

Mullion requests `DWMWA_WINDOW_CORNER_PREFERENCE` (`33`) after applying its
native frame. [Microsoft documents that attribute](https://learn.microsoft.com/windows/win32/api/dwmapi/ne-dwmapi-dwmwindowattribute)
as supported starting with Windows 11 build 22000. In the current source a failed
set or readback logs a warning and does not abort startup
([`host/style_windows.go`](../host/style_windows.go)); Windows 10 therefore does
not receive rounded-corner preference and may receive avoidable warning noise.

**Unverified live behaviour:** the exact HRESULT, logging shape, shadow, border,
caption, and non-client visual result on the proposed Win10 baseline have not
been captured. A later implementation may add a capability-aware suppression or
fallback only after that evidence is gathered; this research record does not
authorize such a change.

## Current limitations and release conditions

- There is no declared Mullion minimum Windows release today. `Windows 10/11`
  in contributor material is broader than the observed compatibility evidence.
- `windows/amd64` is the only hosting target. Windows/386 rejection and
  Windows/ARM64 compilation must not be presented as host support.
- A runtime/export-positive `doctor` result is necessary but not sufficient for
  a window that hosts and renders correctly.
- Evergreen provides update and security benefits but no per-app fixed version;
  Fixed Version provides version control but adds package size and servicing work.
- The direct internal runtime export is a deliberate dependency. CI checks that
  it exists, but neither that check nor a cross-build proves live Win10 hosting.
- Windows 11-only DWM attributes must remain optional on Windows 10.

Issue #132 should convert this draft into either an accepted compatibility
decision with a maintained test matrix, or a narrower statement that does not
promise unverified Win10 behaviour.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: point live compatibility proof to the canonical acceptance checklist.
