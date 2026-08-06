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

`archFolder` is the single architecture decision. It returns `x64` for `amd64`
and a clear unsupported-Windows-architecture error for every other `GOARCH`,
including `386` and `arm64`; `ValidateArchitecture` exposes that same decision
to consumers which must gate their own native setup. `findRuntime` calls it
before environment or registry discovery. Consequently `FindRuntime`,
`RuntimeClientPath`, `DescribeRuntime`, and `CreateEnvironmentWithOptions` all
reject the process before probing for, loading, or calling a WebView2 client DLL.

The Go-implemented COM callback vtables are process-wide and initialized through
one `sync.Once`, not per object, but the initialization is lazy. No
`windows.NewCallback` trampoline is allocated at package initialization; the
first supported environment/handler construction builds the shared WebView2
tables only after discovery accepted amd64. The backdrop window callback follows
the same rule, and `mullion doctor` asserts that command dispatch did not allocate
it before probing. Unsupported entry points therefore cannot spend callback slots
merely by importing either package.

The internal error remains the discovery cause. Public `host.Run` translates it
to `host.ErrUnsupportedArchitecture` while wrapping both sentinels, so
`errors.Is` is stable for callers and the returned text still names `GOARCH` and
`windows/amd64`. `host.New` uses `ValidateArchitecture` before process-DPI work;
`Run` rejects the retained result before rediscovery, COM initialization, class
registration, callback creation, or HWND creation.

The doctor asks `DescribeRuntime` before reading or expanding the pinned runtime
path and, on this sentinel, returns without DPI, registry, GPU, home-directory,
or monitor-callback probes. Windows/386 CI executes these production discovery,
host and doctor entries as a real process under WOW64. Windows/ARM64 remains
compile-only; only amd64 makes a runtime-support claim.

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

Users running a Windows/386 or Windows/ARM64 binary receive an explicit public
host error that WebView2 hosting supports only `windows/amd64`, instead of a
blank, mis-scaled, or corrupt controller. `errors.Is` distinguishes it from
ordinary startup failures. `mullion doctor` reports the same architecture error
and exits unsuccessfully without reading a pinned path or probing the machine.

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
- `internal/webview2/loader_discovery_windows.go` calls the architecture decision
  before its injected candidate-discovery, disk, and DLL-version dependencies.
- `internal/webview2/architecture_gate_unsupported_windows_test.go` drives the
  real production entries and proves their injected COM callback factory and
  shared vtables remain untouched on an unsupported process.
- `host/host_windows.go` checks the architecture in `New` before DPI setup and
  in `Run` before discovery/native startup, translating it to the public
  `host.ErrUnsupportedArchitecture` without losing the internal cause.
  `host/architecture_gate_unsupported_windows_test.go` counts both forbidden
  dependency calls and pins the absence of HWND/class/callback state; the amd64
  test is the DPI-call positive control.
- `internal/backdrop/backdrop_windows.go` creates its one process-lifetime
  callback lazily in `Show`; `cmd/mullion doctor` checks that allocation state
  before probing, so an eager initializer changes the production command result.
- `internal/doctor/probe_windows_test.go` proves a wrapped architecture sentinel
  is handled before either reading or resolving the pinned runtime path.
  `internal/doctor/architecture_gate_unsupported_windows_test.go` drives the
  production probe and pins the absence of machine-probe results.
- `.github/workflows/ci.yml` runs those architecture-tagged production tests and
  the real `cmd/mullion doctor` command as a Windows/386 process under WOW64,
  while ARM64 remains compile-only. The amd64 lane runs runtime-dependent tests
  with `-count=1` and directly executes `mullion doctor`, so test caching or
  removal cannot manufacture runtime/export evidence.
- On 2026-08-06 the original audit ran `go test -count=1 ./...`; all three
  Windows targets compiled, and the amd64 live smoke used WebView2
  151.0.4129.59 for two sequential window sessions. That observation preceded
  the new WOW64 execution gate and remains historical evidence only.

---

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: accept the conservative windows/amd64-only WebView2 hosting boundary, retain clearly labelled compile portability for unsupported Windows architectures, and require hardware-backed ABI evidence before widening support.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: make callback tables and the backdrop callback lazy behind command/architecture decisions, expose the public host sentinel, count forbidden DPI/discovery calls, gate doctor path and machine probes, and execute the real Windows/386 doctor entry under WOW64.
