# 0034. WebView2 hosting is supported only on Windows/amd64

**Status:** Accepted

## Context

`internal/webview2` is a hand-written COM binding. Its vtable layouts are shared
across Windows builds, but three controller calls also have architecture-specific
argument encodings:

- `ICoreWebView2Controller.PutBounds` passes a 16-byte `RECT` through a pointer,
  as required by the Windows x64 ABI for an aggregate of that size.
- `ICoreWebView2Controller2.PutDefaultBackgroundColor` packs the four-byte
  `COREWEBVIEW2_COLOR` into one integer argument.
- `ICoreWebView2Controller3.PutRasterizationScale` passes the bit pattern of a
  `double` as a `uintptr`. Go's Windows/amd64 syscall bridge mirrors the integer
  argument registers into XMM registers, which puts the value where the native
  x64 callee reads it.

The runtime discovery helper contradicted those assumptions. `archFolder`
mapped `amd64` to `x64`, `386` to `x86`, and `arm64` to `arm64`, and its unit
test made all three mappings a promise. Windows/386 and Windows/ARM64 also
cross-built successfully. A successful build therefore looked like evidence of
runtime support even though it did not exercise a COM call.

The failure is silent rather than self-diagnosing. On 386, `uintptr` is only four
bytes, so converting `math.Float64bits(1.5)` to `uintptr` discards the high half;
the measured result under WOW64 was zero. The browser receives a rasterization
scale of zero. For `RECT`, the x86 ABI passes the aggregate on the stack by value
and the ARM64 ABI passes a 16-byte aggregate in two integer registers, whereas
the current code passes a pointer. The ARM64 consequence follows the published
ABI and Go syscall implementation but has not been measured on ARM64 hardware.
That is precisely why implementing ARM64 from the specification alone would be
an unsafe widening of the claim.

## Decision

WebView2 hosting is supported only when the target is `windows/amd64`.

`archFolder` is the single architecture gate. It returns `x64` for `amd64` and a
clear unsupported-Windows-architecture error for every other `GOARCH`, including
`386` and `arm64`. `findRuntime` calls the gate before environment or registry
discovery. Consequently `FindRuntime`, `RuntimeClientPath`, `DescribeRuntime`,
and `CreateEnvironmentWithOptions` all reject the process before probing for,
loading, or calling a WebView2 client DLL. No controller can be obtained through
the supported creation path, so the x64-only `PutBounds`, background-colour, and
rasterization-scale encodings are unreachable on unsupported targets.

`ErrUnsupportedArchitecture` preserves the reason across those entry points.
`Host.Run` checks that specific discovery error before COM initialization or
creating an `HWND`; ordinary missing-runtime errors retain the existing deferred
embed behaviour. This distinction makes `StartHidden` safe as well: an
unsupported target cannot enter its message loop and later collapse the ABI
error into a generic failed `Show`.

Windows/386 and Windows/ARM64 remain compile-portable. CI cross-builds them, but
labels those builds as portability checks rather than runtime support. The
supported Windows CI target remains amd64 and is the only Windows target on
which runtime-backed checks make a support claim. Non-Windows source portability
from decision 0007 is unchanged.

## Alternatives rejected

**Implement x86 and ARM64 call encodings now.** Rejected because neither target
has the hardware-backed evidence required for handwritten COM ABI work. The x86
rasterization bug has been measured, but a complete correction also needs the
aggregate and floating-point cases tested through a real controller. ARM64 has
only specification and source-code analysis. Guessing correctly enough to build
would recreate the problem this decision removes.

**Make unsupported Windows targets fail to compile.** Rejected because build
portability is useful to applications that share a dependency graph across
platforms, and it is not necessary for safety. The early runtime gate is before
DLL loading and every ABI-sensitive controller call. Keeping the builds also
lets CI detect accidental source-portability regressions, provided their names
do not call those builds supported.

**Gate only `PutBounds` and `PutRasterizationScale`.** Rejected because it is too
late and incomplete. By then the native runtime has already been discovered,
loaded, and called to create an environment and controller. A single gate before
discovery gives diagnostics and hosting the same answer and prevents future
ABI-sensitive calls from bypassing a scattered list of method checks.

**Select the machine architecture instead of the process architecture.**
Rejected because a native DLL must match the process. An amd64 process on an
ARM64 machine is still governed by its process ABI; machine identity cannot
justify loading an ARM64 client into it or treating an ARM64 build as verified.

## Consequences

Users running a Windows/386 or Windows/ARM64 binary receive an explicit error
that WebView2 hosting supports only `windows/amd64`, instead of a blank,
mis-scaled, or corrupt controller. `mullion doctor` receives the same error from
`DescribeRuntime`, reports no usable runtime, and exits unsuccessfully without
loading WebView2.

The cost is real: a native 32-bit or ARM64 application can compile with mullion
but cannot host a mullion window. It must ship an amd64 process, isolate the host
in an amd64 process, or choose another binding. Compile portability is therefore
a weaker contract than runtime support and must always be labelled as such.

The runtime folder mapping is intentionally narrower than the set of folders a
WebView2 installation may contain. Discovery no longer advertises `x86` or
`arm64`, because finding those binaries cannot make the controller calls safe.

## What would change our mind

Support may widen only after all of the following exist for a candidate
architecture:

1. architecture-specific call files whose encodings are derived from the native
   ABI and do not reuse the amd64 argument assumptions;
2. headless tests for every pure pack/layout decision and compile gates for the
   architecture-specific files;
3. execution on physical or otherwise trustworthy architecture-matched Windows
   hardware with a real WebView2 Runtime, covering environment and controller
   creation, non-zero fractional rasterization scales, repeated bounds updates,
   DPI transitions, background colour, resize, and teardown; and
4. captured evidence that the resulting window paints at the requested bounds
   and scale, rather than merely returning successful HRESULTs.

A Go syscall implementation that gains a documented architecture-independent
way to express aggregate and floating-point COM arguments could reduce the
amount of per-architecture code, but it would not remove the hardware evidence
requirement.

## Evidence

- Issue #109 records the x64-specific encodings, the successful but misleading
  Windows/386 and Windows/ARM64 cross-builds, and the WOW64 measurement where
  converting `Float64bits(1.5)` to 32-bit `uintptr` produced zero.
- `internal/webview2/interfaces_windows.go` documents the Windows/amd64 ABI
  contract and points to Go's amd64 syscall bridge behaviour.
- `internal/webview2/loader_discovery_windows.go` calls `archFolder` before its
  injected candidate-discovery, disk, and DLL-version dependencies.
- `internal/webview2/loader_discovery_windows_test.go` pins `amd64 -> x64`,
  proves `386` and `arm64` return `ErrUnsupportedArchitecture` without invoking
  any discovery dependency, and proves amd64 proceeds through selection. It
  creates no window and requires no runtime.
- `host/host_windows.go` places the runtime-discovery continuation before COM,
  class registration, and window creation while leaving ordinary missing-runtime
  errors to the embed path. `host/pure_helpers_test.go` proves the unsupported
  branch never invokes that continuation and that the run guard is released for
  a succeeding sequential attempt.
- `internal/doctor/probe_windows_test.go` injects an unsupported
  `DescribeRuntime` result and pins its mapping to `Found=false`, a clear
  `Problem`, no export state, and an unusable report.
- `internal/doctor/doctor_test.go` pins the paste-ready unsupported-architecture
  text, including the process architecture and supported target.
- `.github/workflows/ci.yml` labels amd64 as the supported Windows runtime target
  and 386/ARM64 builds as compile-only portability checks.
- On 2026-08-06 `go test -count=1 ./...` passed; Windows/amd64,
  Windows/386 and Windows/ARM64 builds all compiled. The amd64 live smoke used
  WebView2 151.0.4129.59 and completed two sequential window sessions. No
  unsupported-architecture binary was executed, by design.

---

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: accept the conservative windows/amd64-only WebView2 hosting boundary, retain clearly labelled compile portability for unsupported Windows architectures, and require hardware-backed ABI evidence before widening support.
