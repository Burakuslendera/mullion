# Guard authority details

**Status:** active

## Contents

[Purpose](#purpose) · [The common false-success shape](#the-common-false-success-shape) ·
[Issue 94: no-port source authority](#issue-94-no-port-source-authority) · [Issue 107: public diagnostic boundary](#issue-107-public-diagnostic-boundary)
[Issue 108: publication scanner authority](#issue-108-publication-scanner-authority) · [Issue 99: production wiring locks](#issue-99-production-wiring-locks) ·
[Review history and recurrence checklist](#review-history-and-recurrence-checklist) · [Exact proof ceilings](#exact-proof-ceilings)

## Purpose

This exhaustive continuation of [guard-verification.md](./guard-verification.md)
covers issues #94, #107, #108 and their #99 rows. Read it before changing the
network guard, doctor sanitizer, publication scanner or CI leak-scan step; it
records adversarial shapes found after plausible fixes were already green.

The fixes do not enlarge product promises: they make existing promises fail
closed, connect them to production entrypoints and state test ceilings. Syntactic
network analysis, configured known-shape scanning and supplied home spellings
are explicit ceilings, not silent omissions.

## The common false-success shape

All three issues had a public claim and a relevant-looking helper, but an
input-selection, exception or presentation seam bypassed it while the command
still printed success or tests stayed green; documentation described policy
instead of measured proof.

Repair is authority-first: identify the exact `PASS`, `clean` or paste-ready
sink; enumerate input selection; make selection/parse/read failures fatal; bind
exceptions to the smallest repository-relative identity; and mutate the
production connection once. A detector-only table is not the #99 wiring lock.

## Issue 94: no-port source authority

### Repository selection

`TestNoNetworkListener` starts at the module root found by walking to `go.mod`.
Every directory is traversed except `.git`. There is no basename exemption: files
named like the guard or like an approved fixture remain visible when nested. Every
`.go` file is parsed regardless of build tag. Every configured shipped-text
extension is read. A walk, read or parse error returns from the scanner and fails
the named test; it cannot become an empty finding list.

The real-entrypoint test launches the current test binary in temporary modules and
runs only `TestNoNetworkListener`. Negative modules cover guard/loopback basename
spoofs, shipped text, cross-file declarations/assignments/types, extensionless
Winsock, malformed Go and injected read failure. Helper findings cannot replace
this child-process verdict.

### Go API identity

Imported package names are resolved to import paths. Renamed imports retain their
identity. A selector whose base has an `ast.Object` is a lexical shadow, not an
import. Selector references are checked rather than calls alone, so function
values and prohibited type references remain visible. Ambiguous dot imports of a
socket-capable package fail closed because this dependency-free guard cannot
recover receiver provenance without type loading mutually exclusive build files.

Do not restore method-name substring rules. They caused false positives on names
such as `net.Listener` and missed aliased calls, function values and server types.
The supported table is package-specific and must be reviewed when the pinned
`x/sys` surface changes.

### Winsock DLL resolution

The DLL tier recognizes every supported eager, lazy, must-load and `LoadLibrary`
entrypoint in `syscall` and pinned `x/sys/windows`. It follows:

- direct and renamed package selectors;
- local/package captured and ordinarily reassigned loaders, including a sibling
  package assignment and parenthesized assignment target;
- named/unnamed conversions, including generic instantiated function types;
- local/cross-file package type aliases and `LazyDLL` composite literals;
- literal, concatenated, inherited and cross-file module constants;
- built-in `string` conversions, including a parenthesized conversion callee; and
- explicit and extensionless Winsock names completed by the Windows loader.

`go/parser` links objects only within one file. Package constants, types, loaders
and unresolved ordinary assignments are therefore collected per directory plus
package name and evaluated as alternative definitions. That grouping is the
reason these tables are not reducible to a local AST walk. Object/name sets stop
cycles as unresolved; a cyclic type alias cannot be a compiling loader conversion,
so it is not promoted to a finding merely because this syntactic guard does not
type-check it. Run-time assembly, reflection and raw syscalls remain outside proof.

### Endpoint placement

Endpoint classification is candidate-relative because a shipped JavaScript,
PowerShell or HTML file is scanned as one source string while a Go literal is
unquoted first. A candidate may be:

- the host of a scheme URL;
- the host of a scheme-relative authority beginning at a source-token boundary;
- the host after optional userinfo;
- a bounded standalone endpoint; or
- a short legacy IPv4 spelling only when placement makes it an endpoint.

The classifier handles DNS-label `localhost`, browser-compatible decimal/octal/hex
legacy components and their one-to-four-part reduction, wildcard IPv4, and
bracketed IPv6 loopback/unspecified hosts. Root dots are accepted after full and
short authorities. Case folding precedes removal of the intercepted virtual host.
Credentials, URL paths, lookalike labels, version prose and candidates belonging
to an earlier URL remain clean.

A scheme-relative marker inside a URL path is not a second authority. Conversely,
a quoted scheme-relative or standalone value later in a shipped source file must
not be hidden by an earlier URL. Candidate-relative token ends and source mapping
boundaries are what separate those cases.

### Late-review endpoint and DLL forms

The final adversarial pass found three forms after the original #94 matrix and
the first replacement guard were already green. They are part of the authority
now; do not remove them as "extra test cases."

**A string conversion can hide a literal module name.** The loader call remains
direct and statically visible in both examples below:

```go
type dllName = string
windows.NewLazySystemDLL(dllName("ws2_32.dll"))

type genericName[T any] = string
windows.NewLazySystemDLL(genericName[int]("ws2_32.dll"))
```

`constantStrings` used to recognize only the unresolved predeclared identifier
`string`. The conversion argument was therefore literal, but its callee had an
`ast.Object` or an `IndexExpr`, so the guard discarded it. `stringType` now unwraps
parentheses and generic indices, follows same-file `TypeSpec` objects, then follows
every sibling-file package type collected by `packageTypeExpressions`. It accepts
an argument when any build-tag alternative resolves to the predeclared string
type; choosing one alternative would silently make the guard host-build-specific.
The direct table and real-child module both cover ordinary and generic aliases.
Replacing this resolution with the old built-in-only check makes those named
cases print `PASS` inside the child and fail the outer regression.

**Browser-special schemes consume surplus separators.** `http:////127.0.0.1`
and the backslash equivalent do not have an empty authority followed by path
text: WHATWG special-URL parsing consumes the slash/backslash run and treats the
next token as the host. `browserSpecialAuthorityStart` searches backward across
userinfo colons, requires a bounded `http` or `https` scheme, consumes at least
two separators, and returns the first non-separator byte. Generic `scheme://`
and source-bounded scheme-relative handling remain separate fallbacks. Disabling
this special-scheme step makes the direct IPv4/localhost/backslash fixtures and
the actual-child surplus-slash module clean. `http:////example.invalid/` remains
the material clean control.

**Mapped IPv6 must be classified after unmapping.** The late review initially
claimed both `::ffff:127.0.0.1` and `::ffff:0.0.0.0` were missed. Executable
mutation evidence corrected that claim: `netip.Addr.IsLoopback` already recognizes
the mapped loopback, while `IsUnspecified` does not recognize the mapped wildcard
until `Addr.Unmap` exposes its IPv4 identity. The bracketed-address branch now
unmaps before both predicates, making the rule explicit and symmetric. Removing
`Unmap` makes only the measured mapped-wildcard row fail.
Closure notes must cite that measured wildcard miss, not repeat the broader
pre-execution review claim.

### Endpoint exceptions

Exceptions apply only to endpoint findings; no file can exempt a listener/server
API or Winsock load. The full-path fixture set is:

- `host/loopback.go` and `host/loopback_test.go`;
- `host/source_plan_test.go` and `host/source_plan_windows_test.go`;
- one exact split loopback value in
  `host/architecture_gate_unsupported_windows_test.go`; and
- token-specific bracketed-loopback cases in `host/errorpage_test.go` and
  `host/systembrowser_windows_test.go`.

A permitted bracketed token cannot mask a second endpoint in the same literal.
Same basenames elsewhere have no authority; a new path needs an actual-entrypoint
spoof negative. The virtual-host pin is governed by
[decision 0030](./decisions/0030-guard-exempts-the-virtual-host-name.md).

## Issue 107: public diagnostic boundary

### One sink, several producers

Doctor's public formatter owns one writer. Every printable string reaches that
writer; labels and fixed prose do not bypass it. Reflection-based field inventory
tests make a new printable string field fail until it receives an end-to-end home
and UNC fixture. `Report.Homes` is input authority only and is never printed.

The same build string has two other public producers. `mullion version` routes the
actual `main` dispatch through `printVersionCommand`, its injected version source
and its output writer. The startup line keeps `host.Version()` exact for library
callers but sends it through `runtimeSummaryVersion` at the presentation boundary.
An amd64-only Windows test drives actual `Host.Run` with replaced DPI/discovery
seams; its injected unsupported result follows the summary and precedes COM,
class registration, callbacks, HWND creation and message pumping. The amd64 tag
is authority: WOW64 rejects architecture in `New` before those seams.

Keeping Windows seams in a portable test file breaks the Linux package before an
assertion runs; the startup regression must remain `windows && amd64`.

### Known-home matching

Homes are canonicalized for comparison only. Output retains the caller's original
slashes, Unicode spelling and useful suffix. Matching supports long and 8.3 homes,
Unicode simple folding, forward/mixed separators, extended drive prefixes and
extended UNC homes. Returned offsets refer to original bytes, so replacement does
not reconstruct or clean the displayed path.

A home match is accepted at end, before a separator or a verified prose boundary,
and rejected for a longer sibling. Space-number and `.Backup` components remain
siblings both before a separator and at end of text; closers and terminal sentence
punctuation remain prose. The adjacent token distinguishes those cases and keeps
a separate drive token distinct.

This precision is privacy-significant in both directions. Matching too little
leaks a known home; matching too much rewrites a different runtime folder as
`%USERPROFILE%` and destroys diagnostic identity.

### UNC host matching

UNC collapse is a second lazy forward pass after home replacement. It accepts
ordinary/extended UNC only at display/source token boundaries, including angle
wrappers. Doubled separators after a local component remain local; device and
extended drive paths are excluded.

Only machine becomes `<host>`; prefix, share, suffix, wrapper and punctuation stay
byte-for-byte. Dots inside a label remain identity while terminal dot/colon prose
is preserved.

HTTP(S) spans are recognized once, forward and allocation-free, including after a
key/value prefix. Extra scheme slashes and doubled path/query separators after
bracketed IPv6 remain URL text. Skipping each complete span makes the pass linear
instead of rescanning every prefix.

### Late-review backtick and inventory forms

Backticks are source/display wrappers, not machine-name bytes. Before the final
fix, ``folder `\\BUILD-NAS\share` `` failed the UNC start-boundary check because
the preceding byte was not admitted; a byte-zero ``\\BUILD-NAS` `` absorbed the
closing backtick into the host and then dropped it during replacement.
`uncAuthorityBoundary`, `uncHostByte`, `urlTextBoundary` and
`httpURLTextBoundary` now agree that a backtick delimits a token. The two focused
rows separately lock opening and closing behavior: removing the start boundary
leaves `BUILD-NAS` visible, while removing the terminator produces `\\<host>`
without the closing wrapper. HTTP(S) text inside backticks remains one skipped
URL span rather than being reinterpreted as UNC.

The reflected formatter inventory must cover container shape as well as today's
fields. Its recursive walk handles arrays and slices of strings or structs.
A synthetic `[1]string` plus `[1]struct{ Message string }` fixture executes that
branch even though current `Report` happens to use slices. Reverting the array
case makes `TestFormatSanitizesEveryPublicStringField` report an empty inventory
instead of `Warnings[]` and `Sections[].Message`. This is a #99 recurrence lock,
not evidence that today's formatter previously printed an array leak.

The sanitizer knows only the supplied current-home spellings. Foreign profiles,
filesystem aliases, percent-encoded paths and unrelated secrets are outside its
claim. Those limits must remain explicit in verification reports.

## Issue 108: publication scanner authority

### Source plan and exact identities

The script validates that its own root equals Git's canonical top level and
that Git's index is below that root. Source-selecting Git environment variables
are rejected. It binds one exact `HEAD` commit OID, reads a raw NUL-delimited
`ls-tree` map for deletion classification, and reads stage-0 index entries with
mode, object ID and stage. Unmerged/non-stage-0, sparse-directory, gitlink,
unsupported, malformed or failed entries are fatal.

The bound `HEAD` map is metadata only; its file blobs are not a second scan
corpus. An indexed path is skipped before blob retrieval only when its worktree
path is safely absent, `ls-files --deleted` reports that exact raw path, the
index flag is ordinary (not skip-worktree or assume-unchanged), and its
stage-0 mode/object ID exactly matches `HEAD`. Missing paths with any ambiguity,
non-deleted status or index divergence fail closed.

Every other stage-0 indexed regular/symlink blob is fetched by object ID through
byte-oriented `cat-file --batch`. A safe present worktree path is read as raw
bytes only for an indexed path; equal index/worktree bytes coalesce into one
content revision, while a changed worktree is scanned separately. Tracked
symlinks use their link-target blob and never dereference a worktree target.
Regular-path reparse parents/leaves and Windows case/separator filesystem
collisions are fatal. On POSIX, a literal backslash remains part of the exact
Git path and cannot authorize its slash-spelled neighbor.

### Files and decoding

Git NUL records and object responses are parsed as bytes, not native-command
text. Path UTF-8 decoding is strict but preserves a leading `EF BB BF` as
literal `U+FEFF`; no slash/backslash rewrite, case fold, Unicode normalization
or lossy replacement is used for identity or allowance lookup. Content is read
once per distinct revision and strictly decoded as BOM-aware UTF-8,
UTF-16LE or UTF-16BE. UTF-32 BOMs, malformed text, missing objects and all
Git/read/decode failures are terminal. Binary exclusions remain explicit and
are reported once per logical exact path; zero selected text revisions are
fatal.

Detector families inspect configured roots, drive spellings, ordinary/extended
UNC hosts, product shapes, hashes and other named publication forms. This is a
configured known-shape scan, not a general secret detector. Full `HEAD`-only
file content after staged deletion and untracked files remain outside scope.

### Allowances

There is no basename or whole-file skip. A normal allowance binds an exact raw
Git path, detector family, exact full or named capture, and expected count.
For each active path allowance, counts are computed independently per distinct
path/content revision: equal index/worktree bytes consume once; no revision may
exceed `Expected`; and at least one revision must equal it. Under-use without
an exact expected revision is stale; an over-count is a finding.

Workflow hashes require the exact value of a step-level `uses` mapping under
the root `jobs` mapping, a job and its `steps` sequence. Quoted keys normalize
only within that executable parser; comments, block scalars, unrelated
`steps` mappings and copied hashes cannot consume an allowance. Commit-message
allowances remain a separate aggregate and activate only from their exact
stage-0 index anchor. A missing/deleted anchor retires that allowance.

### History

A valid non-shallow `HEAD` is mandatory. Replacement refs and grafts are
rejected; enumeration and message reads disable replacement objects. Both
`git log` commands force UTF-8 output while Git respects a commit's declared
source encoding, so repository/global output settings cannot NUL-hide a
reachable message. Invalid heads, malformed IDs, empty history or decode
errors are fatal. Unreachable commits and history absent from the clone remain
outside the verdict.

### CI wiring

The Windows lock parses the actual job and `steps` sequence. It requires exactly one
checkout/setup/scan in order, exact pins, current-source full-depth checkout, no
source override and no job/step condition or non-fatal key. Comments do not end
the job. `uses`, `name`, `run`, `if` and `continue-on-error` must be step-level;
`fetch-depth`, `ref`, `repository` and `path` must be direct children of `with`.

Scalar/comment/nested-data decoys, another job, late pins and quoted keys cannot
spoof it. Script action authority and the Go current-workflow lock remain independent.

## Issue 99: production wiring locks

The production seams locked here are not all separate rows in #99:

| Boundary | Production artifact driven | Disconnect that must fail |
| --- | --- | --- |
| no-port | real named `TestNoNetworkListener` child | traversal, parse/read errors, detector result or verdict disconnected |
| leak scan | real PowerShell script in temporary Git repositories | source/index/history/decode/allowance failure prints clean |
| CI | actual `.github/workflows/ci.yml` job and step blocks | checkout/setup/scan source, pin, order or fatality changes |
| doctor | actual `Format` writer and reflected printable fields | a field bypasses the sanitizer |
| CLI | actual `main` version branch with injected source/output | command routes around public sanitization |
| startup | actual Windows `Host.Run` with headless seams | summary uses raw `Version()` or skips the injected source |

Mutation evidence belongs in [guard-verification.md](./guard-verification.md). A
useful mutant compiles, changes the production connection and produces the named red signal; a syntax error is not semantic evidence, and disposable copies must be removed after capture.
### Closure boundary

This change closes #94's original measured holes plus the late string-alias,
special-scheme and mapped-wildcard forms above, and #107's released paste-ready
redaction defects plus the final-review backtick wrapper. It closes #108 for
the measured forward-slash drive and ordinary/extended UNC detector defects.
Stricter selection, decoding, history and workflow behavior is adjacent
recurrence hardening, not evidence that those already-fail-closed controls were
broken.

For #99, it resolves rows **#85** (`stripExemptName` authority), **#12**
(literal-path/unreadable scanner inputs), **#71** (full-history checkout and
fatal commit-scan failures), plus the later array-inventory recurrence gap.
The table locks #107 doctor/CLI/startup connections as #107 evidence, not extra
#99 rows; every other #99 cluster remains tracker work.

## Review history and recurrence checklist

Repeated reviews found defects after focused/full suites were green: shadows,
conversions, generic string/DLL aliases, package assignments, candidate-relative mixed-radix/root-dot endpoints, special-scheme runs, mapped wildcard IPv6, encoded external hosts, virtual-host disqualifiers, strict Git/text/YAML authority, sibling/wrapped/backtick paths, array-aware inventory, portable tags and quadratic scanning. Preserve fixtures, not round counts.

Before editing any of these authorities:

1. read this document and applicable decision record; 2. state accepted source/runtime forms and the explicit ceiling; 3. inspect the final green/paste-ready branch; 4. enumerate selection, decoding, exception and transformation steps; 5. add an accepted fixture, material clean control and fatal inspection case; 6. drive the real named test, script, CLI branch or `Host.Run` seam; 7. apply one compiling production-disconnect mutant and record its red signal; 8. run the full verification ladder; 9. report automatic, live, uncovered and uncertain evidence separately.

Do not "simplify" package grouping to a per-file AST walk, URL placement to a
whole-string prefix, UNC recognition to any doubled separator, workflow parsing
to token search, history to ordinary `git log`, or public redaction to one caller:
each is a measured false-success regression documented above.

## Exact proof ceilings

The network guard is syntactic source analysis; it does not prove external dependencies, reflection, raw syscalls or run-time-assembled names.

The publication scanner covers stage-0 index blobs except precisely proved ordinary unstaged-deletion copies skipped before fetch, safe present worktree revisions for indexed paths, reachable real-object messages in the validated clone and only its configured patterns. The bound `HEAD` tree is metadata for deletion classification, not file-content coverage. Staged-deleted `HEAD`-only blobs, untracked files, binary payloads, unreachable history, encrypted/obfuscated data and unnamed secret families are outside its verdict.

Doctor redaction transforms values only against supplied known homes and UNC host syntax; it does not discover foreign profiles or filesystem aliases. Headless unit tests do not prove live Windows/WebView2 frame, snap or DPI behavior; stronger claims require stronger proof first.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: bind publication scanning to exact raw Git paths, distinct index/worktree revisions, metadata-only HEAD classification and fail-closed deletion/allowance rules.
