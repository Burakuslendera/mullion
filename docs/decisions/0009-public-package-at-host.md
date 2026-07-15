# 0009. The public package lives at /host, not the module root

**Status:** Accepted

## Context

The public package was the module root: 77 `.go` files directly under
`github.com/Burakuslendera/mullion`. GitHub renders a repository's README *below*
its root file listing, and a Go package cannot be split across directories, so
those files — 93 top-level entries in all — stood between a visitor and the first
line of prose. Reaching the README meant scrolling past the whole implementation.

That is a real cost for a library whose landing page is its pitch, and the file
count is not going to shrink: one concern per file is a deliberate rule
([CONTRIBUTING.md](../../CONTRIBUTING.md)), and roughly half of the 77 are the
tests that lock behaviour. The only lever left is where the package lives.

## Decision

The public package is `host`, imported as
`github.com/Burakuslendera/mullion/host`, and everything that was at the module
root now lives under `host/`. The **module** path is unchanged, so `go get`,
`go run …/cmd/mullion@latest`, and the version lookup in `host/version.go`
(`modulePath`) are all untouched — only the *package* moved.

Call sites read `host.New`, `host.Config`, `host.Colour`. The root now holds nine
files and the directory tree, and the README renders on the first screen.

## Alternatives rejected

**Keep the flat root.** The honest option, and what most flat Go libraries do —
but not with 77 files. The landing page is the first thing a prospective user
sees, and 93 rows of `*_windows.go` before a sentence of explanation is a cost
paid by every visitor, forever, to save the maintainer a single move.

**Move to `…/mullion/mullion`.** Keeps the package name, so call sites do not
change — only the import lines. Rejected for the doubled path segment: an import
that stutters is read far more often than the call sites are written, and at
v0.0.x the break is close to free either way.

**Split into internal sub-packages behind a thin root package.** Preserves the
import path, which is the one real cost below. But it means carving working,
tested code into several packages and re-exporting an API surface through the
root — a large, risky refactor of behaviour that already works, for a cosmetic
gain. The move is mechanical by comparison: no logic changes, and `git mv` keeps
the history.

## Consequences

The import path changed, which breaks anyone already importing the root package.
That is close to free now — there is no tagged release yet, so nothing resolves to
the old layout — and it would not have been free later. This is the reason to do
it before the first tag rather than after.

Every file that was at the module root is now under `host/`, and the package was
renamed from `mullion` to `host`. Living reference docs were repointed. The
decision records before this one are left as they were and predate the move: a
bare root filename in one (`assets_windows.go`, `nccalc_windows.go`, …) now lives
at `host/<file>`, and a `mullion.X` qualifier is now `host.X`.

`host/leak_test.go` walked `.` to scan the whole repository, which worked only
because the package sat at the root. It now locates the module root explicitly
(`moduleRoot`), so the move did not silently shrink the brand-leak, no-socket and
non-ASCII guards to a single directory.

## What would change our mind

- A tagged release with external importers would make the import-path break
  expensive rather than free; the same move would then owe a major-version story
  it does not owe today.
- GitHub renders the README above the file list, or the listing becomes
  collapsible. The whole reason for the move is a rendering order we do not
  control; if that order changes, a flat root costs nothing and reads more
  directly.

## Evidence

- The move commit, and `git ls-tree --name-only HEAD`: 93 top-level entries and
  77 root `.go` files before, 17 entries and none after.
- The gate ladder on the moved tree: `go build`, `go vet`, `go test ./...`,
  `-race`, both diagnostic build tags, `GOOS=linux`/`darwin` cross-builds, and
  `scripts/leak-scan.ps1` clean.
- The guard scope was proved, not assumed: a forbidden needle planted under
  `docs/` (outside `host/`) failed `TestNoUpstreamBrandLeak`, confirming
  `moduleRoot` still scans the whole tree.
