# 0007. Other platforms compile and return `ErrUnsupportedPlatform`

**Status:** Accepted

## Context

The library is Windows-only by construction: WebView2, Win32 window management and
the frameless hit-test model have no portable equivalent.

The cheapest way to say so is `//go:build windows` at the top of every file. But a
package whose files are all behind that tag has **no Go files at all** on Linux or
macOS. A cross-platform program cannot import it, `go build ./...` on a colleague's
machine fails with "build constraints exclude all Go files", and the failure lands
in someone else's project rather than in ours - for a dependency they only ever
call on Windows.

## Decision

The platform-independent surface - `Config`, `Logger`, `Colour`,
`ErrUnsupportedPlatform` - lives in files with no build tag. `host_other.go`
(`//go:build !windows`) declares a `Host` with every method the Windows build has:
`New` works and normalises the config, `Run` and `Show` return
`ErrUnsupportedPlatform`, and the rest are no-ops. `api_contract.go` declares the
exported surface as an interface and asserts `var _ hostAPI = (*Host)(nil)` - and
it is compiled on **both** platforms, so a method added to one build and not the
other is a build failure here rather than a surprise in a consumer's tree.

## Alternatives rejected

**Build tags only, no stub.** The least code, and arguably the most honest: the
package really is Windows-only, and a stub that does nothing is a stub that lies a
little. The cost is paid by the consumer, who must now split their own files
behind build tags to depend on us at all, and whose cross-platform build breaks
for a Windows-only feature they guard at run time anyway.

**Panic on the unsupported platform.** Impossible to ignore, which is the point. A
library still does not get to kill its consumer's process for a condition the
consumer can handle: an error the caller can check with `errors.Is` is strictly
more useful, and a cross-platform program that skips the desktop window on Linux is
a perfectly reasonable program.

**A portable abstraction over some other platform's web view.** What a genuinely
cross-platform library would do. Not here: everything this package is *for* -
`WM_NCCALCSIZE`, `WM_NCHITTEST`, the WebView2 COM chain, per-monitor DPI - is
Win32. An abstraction over it would be a facade that promises portability the
implementation does not have.

## Consequences

**There are two `Host` declarations, and they must not drift.** Every method added
to the Windows `Host` must also be added to the stub and to `hostAPI` - three
places, and only the third is enforced by the compiler. That enforcement is the
whole reason `api_contract.go` exists; delete it and the drift becomes a runtime
surprise on a platform nobody develops on.

The stub is dead weight in the source tree of every Windows build, and a consumer
who ignores the returned error gets a program that starts on Linux and does
nothing visible. That is a real cost and it is accepted: it is smaller than
handing every consumer a build-tag problem.

## What would change our mind

The library genuinely supports a second platform. At that point the stub is
replaced by an implementation, `ErrUnsupportedPlatform` narrows to whatever is
still unsupported, and `api_contract.go` earns its keep for a third time. Short of
that, nothing about this is contentious - which is precisely why it is worth
recording that the stub is deliberate and not an oversight to be tidied away.

## Evidence

- `host_other.go`: the stub, with the reason in its doc comment.
- `api_contract.go`: `hostAPI` and the compile-time assertion, in a file with no
  build tag.
- `.github/workflows/ci.yml`: the `portable` job runs `go build`, `go vet` and
  `go test` on `ubuntu-latest`, then cross-compiles for `windows/amd64` and
  `darwin/arm64`. A stub that stopped satisfying the API would fail there.
- `docs/verification.md`: `GOOS=linux go build ./...` is one of the automated
  gates, with the reason stated - "anyone who imports this package from a
  cross-platform program must be able to compile on Linux/macOS, even though the
  window cannot run there".
