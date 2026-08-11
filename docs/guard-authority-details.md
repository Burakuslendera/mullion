# Guard authority details

**Status:** active

## Contents

- [Purpose](#purpose)
- [The common false-success shape](#the-common-false-success-shape)
- [Issue 94: no-port source authority](#issue-94-no-port-source-authority)
- [Issue 107: public diagnostic boundary](#issue-107-public-diagnostic-boundary)
- [Issue 108: publication scanner authority](#issue-108-publication-scanner-authority)
- [Issue 99: production wiring locks](#issue-99-production-wiring-locks)
- [Review history and recurrence checklist](#review-history-and-recurrence-checklist)
- [Exact proof ceilings](#exact-proof-ceilings)

## Purpose

This is the exhaustive continuation of [guard-verification.md](./guard-verification.md)
for issues #94, #107 and #108 and their rows in #99. Read it before changing the
network guard, doctor sanitizer, publication scanner or CI leak-scan step. The
short document says what the authorities are; this one records the shapes that
repeated adversarial review found after the first plausible fixes were already
green.

The fixes do not enlarge the product promises. They make the existing promises
fail closed, connect them to production entrypoints and state what their tests do
not prove. A future agent should not reopen these issues merely because a source
guard is not semantic whole-program analysis, the publication scanner is not a
general secret scanner, or doctor cannot identify a home spelling it was never
given. Those are named ceilings, not silent omissions.

## The common false-success shape

All three issues had the same structure:

1. a public claim existed;
2. a helper or scanner looked relevant;
3. an input-selection, exception or presentation seam could bypass it;
4. the named command still printed success or the test suite stayed green; and
5. documentation described the intended policy rather than the measured proof.

The repair rule is therefore authority-first. Identify the exact code that emits
`PASS`, `clean` or paste-ready output; enumerate every input-selection step before
that output; make selection/parse/read failures fatal; bind exceptions to the
smallest repository-relative identity; and mutate the production connection once.
A detector-only table is useful, but it is not the #99 wiring lock.

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

### Files and decoding

The script validates that its own root equals Git's canonical top level and that
Git's index is below that root. Source-selecting Git environment variables are
rejected. Tracked names come from raw NUL-delimited `git ls-files`; glob characters
and non-ASCII names remain data. Binary exclusions are explicit and counted. Zero
selected text files is fatal.

Each file is read once and strictly decoded as BOM-aware UTF-8, UTF-16LE or
UTF-16BE. UTF-32 BOMs and malformed text are fatal inspection errors, never
UTF-16-looking clean input. Detector families inspect configured roots, drive
spellings, ordinary/extended UNC hosts, product shapes, hashes and other named
publication forms. This is a configured known-shape scan, not a general secret
detector.

### Allowances

There is no basename or whole-file skip. An allowance binds normalized
case-sensitive Git path, detector family, exact full capture or named capture,
and expected count. History allowances also require their tracked source anchor.
Deleted anchors retire their allowance. Under-use and over-use are findings.

Workflow hashes require the exact value of a step-level `uses` mapping under the
root `jobs` mapping, a job and its `steps` sequence. Quoted keys normalize and an
inline value comment is removed. Block scalars, comments, unrelated `steps`
mappings and copied hashes cannot consume an allowance.

### History

A valid non-shallow `HEAD` is mandatory. Replacement refs and grafts are rejected;
enumeration and message reads also disable replacement objects. Both `git log`
commands force their output encoding to UTF-8, while Git respects a commit's own
declared source encoding, so repository/global output settings cannot NUL-hide a
reachable message from ASCII detectors. Invalid heads or decode errors are fatal.

Unreachable commits and history absent from the clone remain outside the verdict.
That is why CI checkout depth is part of the authority rather than a convenience.

### CI wiring

The Windows lock parses the actual job and `steps` sequence. It requires exactly
one checkout/setup/scan in order, exact pins, current-source full-depth checkout,
no source override and no job/step condition or non-fatal key. Comments do not
end the job. `uses`, `name`, `run`, `if` and `continue-on-error` must be step-level;
`fetch-depth`, `ref`, `repository` and `path` must be direct children of `with`.

Scalar/comment/nested-data decoys, another job, late pins and quoted keys cannot
spoof it. Script action authority and the Go current-workflow lock remain
independent.

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

Mutation evidence belongs in [guard-verification.md](./guard-verification.md).
A useful mutant compiles, changes the production connection and produces the named
red signal. A syntax error is not semantic evidence. Disposable copies must be
removed after the result is captured.

### Closure boundary

This change closes #94's original measured holes plus the late string-alias,
special-scheme and mapped-wildcard forms above. It closes #107's released
paste-ready redaction defects plus the backtick wrapper found during final review.
It closes #108 for the two defects that issue measured: forward-slash drive paths
and ordinary/extended UNC hosts passed the configured detector. The stricter
selection, decoding, history and workflow behavior is adjacent recurrence
hardening, not evidence that those already-fail-closed #108 controls were broken.

For #99, this work resolves the original rows **#85** (`stripExemptName`
authority), **#12** (literal-path and unreadable scanner inputs) and **#71**
(full-history checkout plus fatal commit-scan failures), and the later
array-inventory recurrence gap recorded on the issue. The table above also locks
#107's doctor/CLI/startup production connections, but those are #107 closure
evidence rather than extra original #99 rows. Every other #99 cluster remains
tracker work; closing #99 would contradict its own cluster plan.

## Review history and recurrence checklist

Repeated independent reviews found defects after focused and full suites were
green: shadows, conversions, generic string/DLL aliases and package assignments;
candidate-relative mixed-radix/root-dot endpoints, special-scheme separator runs,
mapped wildcard IPv6, encoded external hosts and virtual-host disqualifiers;
strict Git/text/YAML authority; sibling/wrapped/backtick paths, array-aware public
field inventory, portable build tags and quadratic scanning. Preserve fixtures, not round counts.

Before editing any of these authorities:

1. read this document and the applicable decision record;
2. state the exact accepted source/runtime forms and the explicit ceiling;
3. inspect the final green/paste-ready output branch;
4. enumerate selection, decoding, exception and transformation steps;
5. add an accepted fixture, a material clean control and a fatal inspection case;
6. drive the real named test, script, CLI branch or `Host.Run` seam;
7. apply one compiling production-disconnect mutant and record its red signal;
8. run the full verification ladder in repository order; and
9. report automatic, live, uncovered and uncertain evidence separately.

Do not "simplify" package grouping to a per-file AST walk, URL placement to a
whole-string prefix, UNC recognition to any doubled separator, workflow parsing
to token search, history to ordinary `git log`, or public redaction to one caller.
Each is a measured false-success regression documented above.

## Exact proof ceilings

The network guard is syntactic source analysis. It does not prove the behavior of
external dependencies, reflection, raw syscalls or run-time-assembled names.

The publication scanner covers only Git-tracked, successfully decoded text and
reachable real-object messages in the validated clone, and only its configured
patterns. It does not inspect untracked files, binary payloads, unreachable
history, encrypted/obfuscated data or unnamed secret families.

Doctor redaction transforms values only against supplied known homes and UNC host
syntax. It does not discover foreign profiles or filesystem aliases. Headless
unit tests do not prove live Windows/WebView2 frame, snap or DPI behavior; stronger claims require stronger proof first.

> Last updated: 2026-08-10 | Editor: OpenAI (GPT-5.6) | Change: add exact late-review reproductions and mutation signals, correct Git output-encoding authority, and map #94/#107/#108 closure separately from resolved #99 rows #85/#12/#71 plus the later array gap.
