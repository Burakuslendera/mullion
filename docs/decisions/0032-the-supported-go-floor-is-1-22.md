# 0032. The supported Go floor is 1.22, and it is a promise rather than a default

**Status:** Superseded by [0033](./0033-the-go-floor-is-1-24-so-the-asset-root-can-be-a-root.md)

The body below is left exactly as it was written, including two claims 0033
measured to be false. *Consequences* says "`os.OpenRoot` is the only thing that
closes it"; *Alternatives rejected* says the 1.24 move buys something "which
nothing else can". A hardened `fs.FS` over `GetFileAttributes` closed the same gap
on go1.22.12, measured. (An earlier version of this note named *What would change
our mind* as the location; that was wrong, and the claim in that section of
[0031](./0031-the-bytes-never-decide-the-content-type.md) is marked there.) 0033
moved the floor anyway, for reasons of its own. Per the rule in this directory's
README, a record is never edited to change its meaning — the reasoning that turned
out to be wrong is the most useful part of it.

## Context

`go.mod` has said `go 1.22` since the module was created, and CI has built and
tested against it in both jobs. Neither of those is a statement to anybody: a
`go` directive is a build constraint, and a workflow file is not documentation.
Read from the outside, the floor was recoverable only by opening `go.mod`.

It is not a dormant number. `host/render_watchdog_windows_test.go:182` uses
`for range 8`, range-over-int, which is a 1.22 language feature, so the module
cannot move down to 1.21 without an edit. And it is not a free number either: it
is the reason `os.OpenRoot` is unavailable, which is the whole of the open half
of issue #103 and is written into decision 0031 as that record's trip-wire.

So the floor was already doing three jobs — constraining the standard library
this module may call, deciding an open security gap, and being cited by another
record — while living only in a build file. The version claims made about it were
also being measured one at a time, per issue, rather than resting on a stated
policy: 0031 had to reach for `GOTOOLCHAIN` to establish what `os.DirFS` refuses
on 1.22 versus 1.23, and found the assumption it started from was wrong.

What is measured, on this branch, at the time of writing:

- The module builds, vets and tests green on go1.22.12, checked with
  `GOTOOLCHAIN` rather than inferred.
- No standard-library symbol newer than 1.22 is called. `slices.Contains`
  (`internal/doctor/probe_windows.go:230`) is 1.21; `min`/`max`
  (`host/nccalc_windows.go:36`) are 1.21. `os.OpenRoot`, `maps`, `iter`,
  `unique`, `structs` and `testing/synctest` are absent, and range-over-func
  does not appear.
- CI runs exactly two jobs, `windows-latest` and `ubuntu-latest`, both pinned to
  `go-version: "1.22"`. There is no `strategy`/`matrix` block and no
  `go-version-file`.

That last point is the gap this record has to be honest about: the floor is
continuously verified and *the ceiling is not*. "1.22 and above" was half a
measurement.

## Decision

The supported Go floor is 1.22. It is stated where a consumer and a contributor
will each meet it — `README.md` beside the platform requirement, and
`CONTRIBUTING.md` in the prerequisites — and it is an invariant on the library:
no standard-library symbol and no language feature newer than 1.22 is used,
whatever toolchain a contributor happens to have installed.

**One half of this is not yet in force.** The floor is verified on every CI run
and the ceiling is not: both jobs are pinned to `"1.22"` and nothing builds the
current release. Making "and newer" a measured claim rather than an assumption
needs a `strategy.matrix` over `["1.22", "stable"]` in
`.github/workflows/ci.yml`, which this record calls for and which has not been
applied. Until it is, the promise this file makes is stronger than the evidence
behind it, and that is stated here rather than left for a reader to discover.

## Alternatives rejected

**Leave it in `go.mod` and say nothing.** This is what was in force, and it is
defensible for an application. It is not for a library: `AGENTS.md` opens with
"every change is an API-compatibility event, and every accepted behaviour is a
promise to somebody else's program", and the minimum toolchain is part of that
surface. It also leaves the constraint invisible to the person most likely to
break it — a contributor on a current Go who reaches for a newer standard-library
call, whose local build passes and whose CI run then fails for a reason the
repository never explained.

**Move the floor to 1.23.** Tempting while issue #103 was being worked, because
`filepath.Localize` was believed to be what makes `os.DirFS` refuse Windows
device names. Rejected because that belief is measurably false: go1.22.12 already
refuses them, through `dirFS.join` → `safefilepath.FromFS` → `IsReservedName`;
1.23 renamed the helper without changing the behaviour. Moving would have cost
consumers a toolchain upgrade and bought nothing. The measurement is in 0031.

**Move the floor to 1.24 and adopt `os.OpenRoot`.** This is the one alternative
that buys something real: it closes the reparse-point half of issue #103, which
nothing else can. Rejected for now on reach — 1.24 was released well after 1.22
and a window host is a dependency deep in somebody else's build, so the cost of
the upgrade falls on them and not on this repository. Recorded rather than
dismissed: it stays the named trip-wire in 0031 and below.

**Track whatever Go supports upstream** — that is, move the floor as old releases
stop receiving fixes. Rejected as a policy because it makes the floor move on
someone else's schedule, and the point of writing it down is that consumers can
plan against it. The upstream position is still worth knowing and is stated in
*Consequences* rather than hidden.

## Consequences

**No standard-library symbol newer than 1.22 may be used, permanently, until
this record is superseded.** That is the sentence to read before proposing one.
It is not a style preference; a call to a 1.23 symbol compiles fine on a
contributor's machine and breaks every consumer on the floor.

**The reparse-point gap in issue #103 stays open, and this record is why.**
`os.OpenRoot` is the only thing that closes it and needs 1.24. Issue #103 is
therefore not a bug awaiting a fix but a cost this decision accepts; it should be
labelled and read that way. `Config.Assets` documents the exposure for callers
who serve from a real directory.

**The floor is older than Go's own support window.** Go maintains the two most
recent major releases, so 1.22 no longer receives upstream fixes. This costs
consumers nothing — the floor is a minimum, and an application on a current
toolchain builds this module normally — but it does mean CI's floor job runs a
toolchain that will not be patched again, and that job should be read as a
compatibility check rather than as a security-relevant build.

**CI grows a second axis and gets slower**, once the matrix above is applied.
Building the floor and the current release doubles the job count. That is the
price of the word "newer" in the promise, and it is the cheapest honest way to
pay it. Until then the word is carried by a one-off local measurement, recorded
under *Evidence*, which no future run repeats.

## What would change our mind

- **A standard-library or language feature newer than 1.22 that this repository
  genuinely needs**, where the alternative is materially worse code rather than
  merely more of it. `os.OpenRoot` is the live candidate and is already named.
- **A measurement that the floor no longer holds**: the floor CI job failing, or
  `GOTOOLCHAIN=go1.22.x` failing locally, means a newer symbol has entered
  without anybody noticing and the decision is being violated rather than
  reconsidered.
- **Evidence that the floor costs consumers rather than serving them** — a report
  that mullion is hard to adopt *because* it targets 1.22, for instance through a
  transitive dependency that has already moved — would invert the reach argument
  that this record rests on.
- **A dependency raising its own floor.** `golang.org/x/sys` is the only one; if
  it requires a newer Go, this record is decided elsewhere and only needs
  updating to say so.

## Evidence

- `go.mod:3` — `go 1.22`. `.github/workflows/ci.yml:39` and `:74` — both jobs
  pinned to `"1.22"`, no `strategy`/`matrix` block anywhere in the file.
- The floor is load-bearing, not nominal: `host/render_watchdog_windows_test.go:182`
  uses `for range 8`, which is 1.22 range-over-int and does not compile on 1.21.
- Measured, not inferred, on this branch: `GOTOOLCHAIN=go1.22.12 go build ./...`,
  `go vet ./...` and `go test ./...` all pass, including the `host` package.
  Cross-compilation for `windows/amd64`, `darwin/arm64` and `linux/amd64` passes
  on the development toolchain, and `gofmt` is clean.
- The static half of the same check — every standard-library import in the tree
  enumerated, and each post-1.22 candidate looked for by name — found nothing
  newer than 1.21 in use. It is stated as a separate line because it is a weaker
  instrument than the compiler: it scans packages and named symbols, not every
  method added to a package already imported. The go1.22.12 build is the proof;
  this is the map that says where to look when it breaks.
- The 1.23 alternative was rejected on a measurement, in 0031's *Alternatives
  rejected* and *Evidence*: `os.DirFS.ReadFile("nul")` fails on go1.22.12,
  go1.23.12 and go1.26.5 alike.
- `os.OpenRoot` first appears in `$GOROOT/api/go1.24.txt`.
- **Not measured here.** Go's two-release support window is upstream policy, read
  from Go's own release documentation rather than observed in this repository.
  Nothing in this record depends on it beyond the paragraph that names it.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: consolidate accumulated edit signatures into the single current footer required by agents/notes.md; Git retains the earlier history.
