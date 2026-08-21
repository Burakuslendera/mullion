# 0042. Go 1.24 remains the released consumer floor

**Status:** Accepted, supersedes [0033](./0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md)

## Context

Decision 0033 raised the supported Go floor from 1.22 to 1.24 so callers could
serve an asset directory with `os.OpenRoot(dir).FS()`. That remains the standard
library primitive that gives the documented reparse-point protection, and
`(*os.Root).FS()` first exists in Go 1.24.

0033 justified breaking the Go 1.22 promise partly by saying that promise had
never been released and that there was no tag to check. The repository history
contradicts that premise. Releases `v0.0.1` and `v0.0.2` both contain a `go.mod`
with `go 1.22`. The floor increase therefore changed a released consumer
contract. 0033 must remain as the record of the reasoning used at the time, but
it can no longer be the authoritative account of the release history.

Two different compatibility claims also need separate names. The **consumer
floor** is the oldest Go release supported by the module. The CI **stable lane**
tracks the latest stable Go release to detect forward-compatibility failures.
A successful stable lane proves compatibility with that tested release and
commit; it does not move the consumer floor to that release.

## Decision

Go 1.24 remains the released consumer floor. The authoritative declaration is
`go.mod`'s `go 1.24` directive. Code, documentation, and the floor CI lanes must
remain usable with Go 1.24. The security reason remains
`os.OpenRoot(dir).FS()`: it gives callers a standard-library asset-root option
that refuses the junction and symlink traversal described in 0033 without
adding a Mullion filesystem API.

CI keeps both `1.24` and `stable` coverage. `1.24` enforces the consumer floor;
`stable` follows the latest stable Go release and checks forward compatibility.
A newer stable release passing does not raise the minimum. A newer stable
release failing does not make Go 1.24 unsupported.

The module does not add a `toolchain` directive. The `go` directive states the
language/module minimum. For initial toolchain selection, the `go` command
considers the `go` and `toolchain` directives from the current workspace or,
outside workspace mode, the main module; a dependency module's `toolchain`
directive does not enter that initial selection. With automatic switching
enabled, such as `GOTOOLCHAIN=auto`, a `toolchain` directive can select a newer
toolchain and download it when it is not available locally, subject to network
and toolchain availability. That behavior is not part of this consumer-floor
decision. The version of `golang.org/x/sys` is independent too: neither
retaining Go 1.24 nor testing a newer stable Go release
requires a dependency bump. A future dependency change must stand on its own
evidence.

This record is the single current owner of the floor rationale and release
history. Active support documents and CI may state or enforce the contract, but
they should link here rather than duplicate this history. Historical decisions
are never rewritten to make later evidence appear known at the time: 0033 keeps
its reasoning and stale premise, carries only a superseded status, and points
here.

## Alternatives rejected

**Return the floor to Go 1.22 because released versions supported it.** The
release history makes the compatibility cost real, but it does not remove the
security and maintenance reasons for using `os.OpenRoot`. Returning to 1.22
would require withdrawing the asset-root recommendation or owning the weaker,
platform-specific filesystem implementation rejected by 0033. Neither is a
better current contract than keeping the already-adopted 1.24 minimum and
recording its cost honestly.

**Raise the floor whenever the stable lane advances.** This would turn a
forward-compatibility measurement into a recurring consumer break and make
"minimum" mean "latest". The two lanes answer different questions and stay
separate.

**Freeze the forward-compatibility lane at one newer Go version.** A fixed
version would preserve one historical result but stop detecting regressions
against the current release. Durable records preserve notable runs; CI remains
the moving measurement.

**Add a `toolchain` directive.** It is not required to declare `go 1.24`.
Repository checkout and contributor commands run with Mullion as the main
module, so under automatic switching the directive could select or download a
preferred toolchain and blur testing of the declared floor. CI already pins the
floor and stable lanes explicitly. Adding a `toolchain` directive to Mullion
would not enter the initial selection for a build whose main module merely
depends on Mullion; that consumer's main-module or workspace configuration
controls the initial selection. This is separate from `go 1.24`, which remains
a minimum that the consumer's module graph must satisfy.

**Tie the Go floor to a `golang.org/x/sys` upgrade.** The floor is justified by
a standard-library API and its security properties, not by a dependency
version. Coupling unrelated upgrades would obscure the reason for each change
and create release cost without evidence.

**Correct 0033 in place.** Replacing its never-released premise would make the
old record read as though the tag evidence had been known when it was written.
The decision lifecycle requires a superseding record so the mistake, its
correction, and the current authority remain auditable.

## Consequences

Consumers of `v0.0.1` and `v0.0.2` could build with Go 1.22; consumers of the
current module must use Go 1.24 or newer. Keeping the floor accepts that released
compatibility break rather than explaining it away. A consumer pinned below
1.24 must upgrade Go or remain on an older Mullion release.

Future code must continue to compile at the 1.24 floor. APIs introduced after
1.24 require a separately justified floor decision even when the stable lane
already passes them. Conversely, supporting 1.24 does not imply that every
future Go release is automatically compatible; the stable lane measures that
claim continuously.

The permanent repository cost is maintaining both floor and stable CI coverage
and keeping their meaning explicit in active support statements. The consumer
cost is the Go 1.24 requirement. The benefit is a security recommendation based
on `os.OpenRoot` without a new public filesystem abstraction or permanent
platform-specific implementation in this module.

`os.OpenRoot` keeps the limits recorded in 0033: callers choose whether to pass
its `fs.FS`, and its Windows protection is not a general defence against every
kind of filesystem redirection. This decision preserves that analysis; it
supersedes 0033 because the release-history premise and compatibility accounting
were stale.

## What would change our mind

- **A supported, race-safe asset-root implementation available below Go 1.24**
  that provides the required reparse-point protection without Mullion owning a
  platform filesystem API would reopen lowering the floor.
- **Measured evidence that maintained Mullion consumers cannot move to Go 1.24**
  would require reconsidering the migration cost and an explicit compatibility
  or deprecation policy; the existence of old release tags alone is already
  accounted for here.
- **A production requirement that cannot be implemented while compiling with Go
  1.24** would justify considering a new floor decision. A passing latest-stable
  CI run, by itself, never fires this trip-wire.
- **A Go runtime change that removes or weakens the `os.OpenRoot` protection on
  which the asset-directory recommendation relies** would invalidate the
  security basis and require a new mitigation, whether or not the numeric floor
  changed.
- **A stable-lane failure caused by an incompatibility with the latest Go
  release** would require fixing or explicitly revising forward compatibility;
  it would not, by itself, raise the consumer floor.

## Evidence

The searchable evidence chain has distinct owners:

- `go.mod` owns the live declaration: `go 1.24`. The CI `1.24` lanes own the
  executable floor check; the `stable` lanes own the moving latest-release
  compatibility check.
- `git show v0.0.1:go.mod` and `git show v0.0.2:go.mod` each return `go 1.22`.
  The tagged files own release history; current prose cannot replace them.
- [Decision 0033](./0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md)
  owns its historical `os.OpenRoot` reasoning and the stale never-released
  premise. Its superseded status and this record own the correction; the old
  body is not rewritten.
- `host/assets_windows_test.go`'s
  `TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot` owns the measured
  `os.Root`/`os.DirFS` junction distinction. The floor lanes own commands such as
  `go build ./...`, `go vet ./...`, and `go test -count=1 ./...` under Go 1.24;
  the stable lanes run the corresponding compatibility checks under the latest
  release.
- [Run 32425771310](https://github.com/Burakuslendera/mullion/actions/runs/32425771310)
  on `00c33b8` exposed a Go 1.24/Go 1.27
  formatter disagreement: both `1.24` jobs and `portable (stable)` passed, while
  `windows (stable)` stopped at `gofmt`. It did not establish a source or runtime
  incompatibility.
- Formatting commit
  [`f69f129`](https://github.com/Burakuslendera/mullion/commit/f69f129)
  made the ABI manifest stable under both Go 1.24 and Go 1.27 formatters.
  [Run 32427012163](https://github.com/Burakuslendera/mullion/actions/runs/32427012163)
  then completed successfully on that commit: all 5 jobs passed, including both
  `stable` jobs on Go 1.27. This proves Go 1.27 compatibility for that commit; it
  does not raise the consumer floor above Go 1.24.
- Issue [#127](https://github.com/Burakuslendera/mullion/issues/127) owns the
  reconciliation request and links the same tags, stale premise, and successful
  remote run. Durable verification records should point to issue #127 and this
  decision instead of restating the policy.

These observations do not prove compatibility with untested future Go releases,
make Go 1.22 supported by the current module, test a `toolchain` directive, or
justify changing `golang.org/x/sys`. The junction test can skip where `mklink /J`
is unavailable and does not claim that `os.Root` blocks hard links; 0033 records
those limits. The formatting commit and its dual-toolchain checks cover source
format stability, not runtime behavior. Remote CI owns the listed Go 1.27 result;
local commands do not substitute for that remote evidence.

> Last updated: 2026-08-21 | Editor: OpenAI (GPT-5.6) | Change: clarify main-module/workspace-only toolchain selection while preserving the Go 1.24 floor and released Go 1.22 history.
