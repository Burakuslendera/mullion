# 0033. The Go floor is 1.24, so that an asset directory can be an `os.Root`

**Status:** Accepted, supersedes [0032](./0032-the-supported-go-floor-is-1-22.md)

## Context

0032 was written one day before this record and set the floor at 1.22. The
observation that would change it was already named — in 0031's *What would change
our mind*, which 0032 pointed at rather than restating:

> Moving the supported Go floor to 1.24 would make `os.OpenRoot` available, close
> the reparse-point gap, and is the only thing that would.
>
> — [0031](./0031-the-bytes-never-decide-the-content-type.md), *What would change
> our mind*. 0032 carried the same claim in its *Consequences*: "`os.OpenRoot` is
> the only thing that closes it and needs 1.24."

Two measurements fired it, and the second corrects the first.

**The gap is real and reachable through a name nothing can reject.** A directory
junction planted inside the asset directory with `mklink /J`, which needs no
elevation, points outside it. `os.DirFS(dir).ReadFile("junction/secret.txt")`
returns the file from outside. The boundary cannot help: `junction/secret.txt` is
an ordinary name that passes every check mullion makes, and the redirection is in
the filesystem rather than in the string. This is the half of issue #103 that was
left open, and it is a defence-in-depth failure rather than an exploit — reaching
it needs a separate primitive that can write into the asset directory, such as a
content folder beside a portable executable.

**`os.Root` closes it, and 1.24 is enough.** Measured on go1.24.6 and go1.26.5:
`os.OpenRoot(dir).FS()` returns `*os.rootFS`, which answers `openat
escape/secret.txt: path escapes from parent` for the junction while serving
`index.html` and `sub/deep.txt` normally. `(*os.Root).FS() fs.FS` is in
`api/go1.24.txt`, not 1.25, so the floor needed is 1.24. The returned value
implements `fs.ReadFileFS` and `fs.StatFS`, so the `fs.ReadFile` call the asset
provider already makes takes the fast path.

**0032's "the only thing that would" was also wrong, and that matters here.** A
hardened `fs.FS` was written and measured on go1.22.12: walking the path
components and asking `GetFileAttributes` for `FILE_ATTRIBUTE_REPARSE_POINT`,
using only `golang.org/x/sys/windows`, which is already the sole dependency. It
refused the junction and served the legitimate files. So the gap *was* closable
on 1.22, and 0032 claimed otherwise without checking. That alternative is weighed
below rather than buried, because it is the reason this decision is a choice and
not a necessity.

## Decision

The supported Go floor is 1.24, and `os.OpenRoot(dir).FS()` is the documented way
to serve assets from a directory. `Config.Assets` says so, `README.md` and
`CONTRIBUTING.md` state the floor, and CI builds both the floor and the current
release rather than the floor alone.

mullion does not and cannot enforce it. `Config.Assets` is an `fs.FS`; a caller
who passes `os.DirFS` still gets the old behaviour, and the concrete type cannot
be inspected because wrapping is the documented way to use the API — 0031 rejected
type-switching for that reason. What changed is that the safe way is now sayable,
which it was not while the floor forbade it.

## Alternatives rejected

**Stay on 1.22 and ship a hardened `fs.FS` from this module.** Measured working
(see *Context*), so this is a real alternative and not a strawman. Rejected on
three counts. It is new permanent public API for a window host, which is the
wrong place for a filesystem primitive. It is weaker: the attribute check and the
open are two syscalls, so a junction planted between them still wins, while
`os.Root` opens handle-relative — `os/root_windows.go` sets
`objAttrs.RootDirectory` to the directory descriptor and passes
`FILE_FLAG_OPEN_REPARSE_POINT`, which is read from Go's source rather than raced
here, though the measurement below plants a junction after the `os.Root` is open
and it is still refused. And it would have to be
maintained against the platform forever, whereas `os.Root` is maintained by the
Go team. The cost of choosing it would have been paid by every future reader of
this repository; the cost of the floor move is paid once, by consumers, at
upgrade time.

**Stay on 1.22 and document the gap only.** This was the state after 0031 and
0032. Rejected because the documentation could not name a remedy: telling a
caller "a junction inside your asset directory escapes it" without being able to
say "so use `os.OpenRoot`" leaves them with nothing to do. A limitation a reader
cannot act on is a warning, not a mitigation.

**Move the floor to 1.25 or later** and take `(*os.Root).ReadFile` and the rest
of the 1.25 additions with it. Rejected because nothing here needs them: `FS()`
lands in 1.24, and the floor should be the oldest version that supports what the
library actually does.

**Require an `os.Root` in the API** — replace or supplement `Config.Assets fs.FS`
with a directory path or an `*os.Root`. Rejected as a breaking change to the
central field of the public API, and it would exclude `embed.FS`, which is the
common case and is immune to this class anyway. The recommendation carries the
same benefit for callers who take it, without taking the choice away.

## Consequences

**Consumers must be on Go 1.24 or newer.** This is the real price, and it is
charged to somebody else's build rather than to this repository. The floor itself
moves only once — `go.mod` said `go 1.22` from the start and 0032 changed no
number, it only wrote the existing number down as a promise. What this record
breaks is that promise, one day old. That is defensible only because it was never
released; if it had been, this would need a deprecation window instead. There is
no tag or changelog in the tree to check that against, so "never released" is
read from the absence of one rather than measured.

**The reparse-point gap is closed for callers who follow the recommendation, and
unchanged for those who do not.** `os.DirFS` still follows a junction. mullion
cannot detect which was passed, so this is a documentation guarantee rather than
an enforced one, and the honest statement of the fix is "the safe path exists and
is named", not "the boundary now refuses reparse points".

**What `os.Root` buys is narrower and blunter than "it keeps you inside the
directory", and an audit of this record measured both halves.** Neither changes
the decision; both change what may be claimed for it.

- *Narrower.* `os.Root` refuses reparse points whose tag is a **name surrogate** —
  junctions and symlinks. A hard link is not one. Measured: `mklink /H` inside the
  root, pointing at a file outside it, read `"OUTSIDE-VIA-HARDLINK"` through
  `os.Root` exactly as through `os.DirFS`. `mklink /H` needs no elevation, so an
  asset directory that arbitrary code can write into is not made safe by an
  `os.Root`. The remedy for that is an `embed.FS` or a directory nothing else
  writes to, and it always was.
- *Blunter.* It refuses those tags wherever they point, **including a target
  inside the root**. Measured: `dir/alias -> dir/real` served under `os.DirFS` and
  answered `path escapes from parent` under `os.Root`, so a build step that plants
  an internal junction for convenience turns a working asset into a `500`. It is a
  tag check rather than a containment check, and the cost lands on a legitimate
  layout.

The earlier draft of this record said `os.Root` answers "filesystem redirection".
It does not; it answers two tags. The distinction is written here because the
wider sentence is the one a reader would carry away and act on.

**Anything newer than 1.24 in the standard library is now available**, and the
invariant 0032 imposed loosens accordingly. The rule is unchanged in kind: no
symbol newer than the floor, whatever the floor is.

**CI runs four jobs where it ran two.** Both platforms build the floor and
`stable`. That is the cost of the word "newer" being a measured claim rather than
an assumption, which 0032 called for and did not apply.

## What would change our mind

- **A change in `os.DirFS` that made it refuse reparse points** would make the
  recommendation redundant and this floor unnecessary.
  `TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot` asserts both halves for
  exactly this reason: it fails if `os.DirFS` stops following the junction, not
  only if `os.Root` stops refusing it.
- **A change in `os.Root` that let it traverse a reparse point** would make the
  recommendation wrong, and the same test fails.
- **A report that Go 1.24 is out of reach for real consumers** would reopen the
  hardened-`fs.FS` alternative above, which is measured and therefore cheap to
  revisit.
- **A way for mullion to tell an `os.Root`-backed `fs.FS` from any other** — an
  interface the standard library exposed, say — would turn the recommendation
  into something enforceable, which is strictly better than a document.

## Evidence

- `go.mod:3` — `go 1.24`. Measured, not assumed: `GOTOOLCHAIN=go1.22.12 go build
  ./...` now fails with `go.mod requires go >= 1.24`, and
  `GOTOOLCHAIN=go1.24.6 go build/vet/test ./...` passes on every package.
- `api/go1.24.txt` carries `pkg os, method (*Root) FS() fs.FS`; `api/go1.25.txt`
  carries `ReadFile`, `WriteFile` and the rest. The floor is the earlier one.
- The junction, measured on go1.24.6 and go1.26.5 against the same fixture:
  `os.DirFS` read `"OUTSIDE THE ROOT"` through `escape/secret.txt`;
  `os.OpenRoot(root).FS()` answered `path escapes from parent`; both served
  `inside.txt` and `sub/deep.txt`. `Root.FS()` reported as `*os.rootFS`,
  implementing `fs.ReadFileFS` and `fs.StatFS`.
- `host/assets_windows_test.go`:
  `TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot` plants the junction with
  `mklink /J`, asserts both directions and then drives the whole boundary over
  the `os.Root` file system. It skips rather than passing vacuously where
  `mklink /J` is unavailable.
- The rejected 1.22 alternative was measured before it was rejected: a
  component-walking `fs.FS` over `windows.GetFileAttributes` and
  `FILE_ATTRIBUTE_REPARSE_POINT` refused `escape/secret.txt` and served
  `inside.txt` and `sub/deep.txt` on go1.22.12.
- `.github/workflows/ci.yml` — both jobs carry
  `strategy.matrix.go: ["1.24", "stable"]` and reference it from `setup-go`.
- **The two limits in *Consequences*, measured on one fixture.** `mklink /H` from
  inside the root to a file outside it: `os.DirFS` and `os.Root` both read
  `"OUTSIDE-VIA-HARDLINK"`. `mklink /J dir/alias dir/real`, target inside the
  root: `os.DirFS` read `"INSIDE-VIA-ALIAS"`, `os.Root` answered `openat
  alias/in.txt: path escapes from parent`. The control, an ordinary
  `index.html`, and the same file reached directly as `real/in.txt`, both served
  under both. Go's rule is in `os/root_windows.go` — the reject keys on
  `IO_REPARSE_TAG_SYMLINK` and `IO_REPARSE_TAG_MOUNT_POINT` being name
  surrogates, not on where the link points.
- **Not measured.** NTFS volume mount points, and directory or file symlinks:
  `mklink /D` and `mklink` both answered "You do not have sufficient privilege"
  on the audit machine. Junctions and hard links need none, which is why they are
  the two that could be established here.

> Last updated: 2026-07-30 | Editor: Claude (Opus 5) | Change: new record, superseding 0032 one day after it. The floor moves to 1.24 so os.OpenRoot(dir).FS() can be the documented way to serve a directory, which closes the reparse-point half of issue #103 for callers who take it. Measured: os.DirFS reads through an mklink /J junction, os.Root answers "path escapes from parent", and (*os.Root).FS() is in api/go1.24.txt rather than 1.25. Also records that 0032's "moving to 1.24 is the only thing that would close it" was wrong - a hardened fs.FS over GetFileAttributes was built and measured working on go1.22.12 - and rejects that alternative on API surface, on the TOCTOU race it leaves open, and on maintenance, rather than on capability.
