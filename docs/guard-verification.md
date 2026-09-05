# Guard verification and recurrence prevention

**Status:** Active

## Contents

- [Purpose](#purpose)
- [Resolution status](#resolution-status)
- [The shared failure pattern](#the-shared-failure-pattern)
- [Issue 94: no-listener source authority](#issue-94-no-listener-source-authority)
- [Issue 108: publication leak-scan authority](#issue-108-publication-leak-scan-authority)
- [Issue 107: doctor public-output authority](#issue-107-doctor-public-output-authority)
- [Issue 99: production wiring and mutation proof](#issue-99-production-wiring-and-mutation-proof)
- [Observed mutation ledger](#observed-mutation-ledger)
- [Maintenance checklist](#maintenance-checklist)
- [Proof boundaries](#proof-boundaries)

## Purpose

This is the detailed fix record for issues #94, #107 and #108, and for the part
of #99 that allowed those three boundaries to stay green after their policy was
disconnected. It exists so a later contributor does not rediscover the original
holes, add another keyword, and unknowingly restore the same false-success shape.

The exhaustive accepted/rejected shapes and maintenance traps continue in
[guard-authority-details.md](./guard-authority-details.md).

The product promises are unchanged:

- mullion opens no listening socket and serves embedded assets in process;
- the publication scan must not call an incomplete inspection clean;
- the doctor block is designed to be pasted into a public issue without exposing
  a known current-home spelling or UNC machine name.

What changed is the authority behind those promises. Each boundary now selects
inputs by repository-relative identity, fails when an intended input cannot be
inspected, exercises its real production entrypoint, and states the ceiling of
what it can prove.

## Resolution status

**Resolved in this change:** the #94, #107 and #108 false-success mechanisms are
removed from their production authorities. The corresponding #99 rows now fail
under representative disconnecting mutations; this does not close the unrelated
historical rows still owned by #99.

| Issue | Closed mechanism | Recurrence lock |
| --- | --- | --- |
| #94 | Basename skips and substring needles were replaced by module-relative traversal, AST import/symbol policy and shipped-literal inspection. | The real named guard runs against temporary modules containing the old bypasses; clean controls remain green. |
| #108 | Basename exclusion, incomplete selection/history and backslash-only path detection were replaced by exact relative rules, strict decoding, validated history and separate drive/UNC families. | Temporary Git repositories invoke the real script; workflow source locks full checkout plus script invocation. |
| #107 | Field-local prefix redaction was replaced by one writer through which every printable report string passes. | All-string-field inventory, build-field bypass, slash/extended/UNC matrices and public CLI version tests cover the boundary. |

Observed verification on 2026-08-10: focused `host`, `internal/doctor` and
`cmd/mullion` suites passed; the uncached full suite and both diagnostic-tag
build/test pairs passed; `go build`, `go vet`, Linux stub build, bridge VM,
doctor/version CLI runs and the configured publication scan passed. Workspace
Go diagnostics were empty. The race lane was not covered on this workstation:
`CGO_ENABLED=0`, and the retry with CGo enabled could not find `gcc`.

## The shared failure pattern

The original defects looked unrelated: one was a Go test, one a PowerShell
script, and one a report formatter. The root mechanism was the same.

1. A helper or detector expressed part of the intended rule.
2. Some inputs bypassed the helper through selection, path authority, formatting
   routing, or incomplete history.
3. The final authority saw no finding because it had inspected less than it
   claimed.
4. The final authority still emitted its strongest success verdict.
5. Existing tests exercised the helper or one happy spelling, not the production
   connection that could be removed.

The repair therefore does not rely on detector tables alone. Selection, relative
path identity, parser/decoder errors, final exit status, formatter routing and
workflow invocation are all test-visible.

## Issue 94: no-listener source authority

### What was wrong

The old `TestNoNetworkListener` used case-sensitive substrings over `.go` files.
It skipped every file whose basename matched the guard file and granted the
loopback exception to every file whose basename matched either loopback fixture.
A nested spoofed basename could therefore disable the whole guard or one tier.
Improving the keyword list before removing that authority escape would have left
every new detector bypassable by filename.

The substring policy also confused syntax with spelling. A type reference could
match a function-name prefix and fail, while import aliases, dot imports,
receiver-variable server starts, inherited listeners and server type references
could pass. Non-Go text embedded into the shipped binary was outside the walk.
The decision record named a server type that the actual needle list did not even
contain.

### Current selection and path authority

`scanNetworkPolicy` walks from the located module root. Every file path is reduced
with `filepath.Rel`, rejected if it escapes the root, and normalized to slash form
before policy comparison. No basename is an authority. The only skipped directory
is `.git`; files are selected by the two explicit policy scopes below.

A walk error, read error or selected Go parse error returns an error to
`TestNoNetworkListener`, which calls `t.Fatalf`. An uninspected selected file can
never become a clean result.

The implementation is split by concern: `network_guard_test.go` owns traversal
and Go API identity, `network_dll_guard_test.go` owns loader/constant analysis,
`network_endpoint_guard_test.go` owns endpoint classification/exceptions, and
`network_guard_policy_test.go` owns adversarial and real-entrypoint fixtures.

### Current Go API tier

Every `.go` file is parsed with `go/parser`, regardless of build tags; comments are
deliberately excluded. Local import names map to import paths, while selector bases
resolved by `ast.Object` remain lexical shadows rather than package identities.
Renamed imports retain identity without classifying a local `net.Listen` method.
Selector references catch calls, function values and prohibited type references;
ambiguous dot imports of socket-capable packages fail closed.

The API table covers `net`, `net/http`, `net/http/httptest`, `crypto/tls`, `syscall`
and pinned `x/sys/windows`. DLL resolution follows direct/captured/reassigned
loaders, parenthesized targets, named/unnamed/generic conversions, package types
and `LazyDLL` aliases, plus literal/concatenated/converted explicit or extensionless
names. Constants, types, initialized loaders and assignments resolve per group.

Do not replace this with a global ban on method names such as `Serve`. Unrelated
application abstractions legitimately use those names. Package/type provenance is
what removes both the old false positive on `net.Listener` and the temptation to
keep adding receiver-name guesses.

When the pinned `x/sys` version changes, inspect its exported Winsock surface and
update the symbol table and fixtures together. A dependency update that changes
the capability inventory without revisiting this table is incomplete.

### Current endpoint tier

Go endpoint checks inspect unquoted AST string literal values, not comments.
Shipped text uses an explicit extension set covering frontend, script and source
forms that can enter the binary or published example. Documentation is not part
of this guard; otherwise the records explaining forbidden forms would disable the
build. Documentation remains inside the publication scanner's separate scope.

The endpoint detector case-folds before removing only the intercepted host.
Candidate-relative standalone/scheme/scheme-relative placement covers bracketed
IPv6 and browser decimal/octal/hex one-to-four-part IPv4, including short root
dots. Source boundaries admit quoted shipped forms after unrelated URLs while
excluding paths, credentials, lookalikes and version prose. Token-specific
exceptions cannot mask a second endpoint in one literal.

Exceptions are rule-specific. Listener/server APIs have no file exception.
Loopback validation data is allowed only in the exact repository-relative parser
fixture locations, and the default virtual host has a separate pin. Adding a new
fixture location requires a named negative test proving that the same basename
elsewhere remains visible.

### Actual-entrypoint proof

`TestNoNetworkListenerExercisesRealTraversalAndVerdict` obtains the current test
binary, runs the real `TestNoNetworkListener` from temporary module directories,
and asserts the child process result. Its fixtures cover:

- a genuine clean module and ordinary listener symbol;
- nested guard/loopback basename spoofs and shipped quoted endpoints;
- cross-file constants, initialized/reassigned loaders and generic types;
- explicit/extensionless and converted Winsock names;
- malformed selected source and deterministic selected-file read failure.

This is the #99 lock. A helper-only scanner test would not prove that
`moduleRoot`, traversal, detector results and the named test's exit status remain
connected.

### Do not regress this boundary

- Never restore a basename skip.
- Never exempt a whole file from the API tier.
- Never turn parser/read failure into `continue`.
- Never test only `CallExpr`; function values and type references matter.
- Never claim semantic whole-program proof from a syntactic source guard.
- Never add an endpoint fixture without a path-specific reason and a spoofed-path
  negative control.

## Issue 108: publication leak-scan authority

### What was wrong

The old authority selected mutable worktree names, rewrote path separators and
treated missing files as harmless. It could therefore inspect a clean decoy
instead of staged bytes, let a path alias inherit an allowance, or claim a
clean result after an incomplete read. This was a verdict-authority bug, not
just a regex omission.

### Current declared scope

The scanner consumes:

- stage-0 index blobs, except an indexed path proved to be an ordinary
  unstaged deletion before blob fetch;
- safe present worktree bytes for indexed paths, only as distinct revisions;
- safely missing paths only when Git reports deletion, flags are ordinary,
  and stage-0 mode/object ID equals the bound `HEAD` tree map;
- reachable commit messages from a valid, complete, non-shallow `HEAD`; and
- the configured detector families only.

Equal index/worktree bytes coalesce. Tracked symlinks are scanned as link-target
bytes without dereferencing. Full `HEAD` file content, staged-deleted
`HEAD`-only blobs, untracked files, binary payloads, unreachable commits,
foreign branches, encoded/obfuscated values and unknown secret classes are
outside this claim.

### Authority summary

Git manifests and blobs use checked byte-oriented plumbing. Exact Git path bytes
remain identity keys: no slash/backslash rewriting, case folding, Unicode
normalization or lossy decode may choose a file or allowance. Windows
case/separator collisions and reparse ambiguity fail closed. Strict decoding,
object lookup, history, and worktree errors cannot reach the clean branch.

Allowances bind exact path, detector, capture and expected count. Each active
path allowance is counted independently per distinct content revision: no
revision may exceed `Expected`, and at least one must equal it. Commit-message
allowances remain aggregate and require their exact stage-0 index anchor.

### Real-script proof

Temporary repositories invoke the real PowerShell script for detector
punctuation, path/history/count/action substitutions, staged index content,
ordinary deletion, skip-worktree ambiguity, raw path identity, strict
UTF-8/UTF-16, rejected UTF-32, invalid/shallow history, missing objects and a
clean repository. The workflow lock still requires full-depth checkout and the
real pinned scan step.

### Do not regress this boundary

- Never restore basename or whole-file exemptions.
- Never rewrite exact Git paths or fetch objects through text output.
- Never swallow Git, read, decode, reparse or collision errors.
- Never let a clean worktree replace an indexed blob.
- Never allow shallow history while claiming commit-message coverage.
- Never describe this configured known-shape scan as proof of no secret of any
  kind.

## Issue 107: doctor public-output authority

### What was wrong

The old formatter had two different output paths. `Folder` and `PinnedEnv` called
`redactHome`; the Mullion build string and every other printable string went
straight through control folding. A home path embedded inside replacement metadata
therefore bypassed redaction even if the whole build string was handed to the old
helper, because that helper compared only at byte offset zero.

The old comparison also treated slash variants as different paths. Its UNC helper
read the question mark in an extended drive prefix as a machine name, replaced the
wrong component, and left the user path behind while making it look like a network
share.

### Single public-output boundary

`Format` now creates one `publicReportWriter`. `formatWebView2` receives that writer,
not a raw `strings.Builder`. Every value originating in a printable report string
field or note routes through `sanitizePublicValue` before output. Direct builder
writes are limited to fixed Markdown scaffolding, blank lines, an all-numeric
monitor template and its fixed `primary` suffix, and the fixed measured-DPI
sentence; none accepts a string from `Report`.

The sanitizer performs operations in this order:

1. fold terminal control bytes;
2. replace every occurrence of a known home spelling;
3. collapse any remaining ordinary or extended UNC host.

Home replacement must precede general UNC collapse. A known home on a UNC profile
must become `%USERPROFILE%`; hiding only its server would preserve more identity
than the contract allows.

### Matching without corrupting diagnostics

Known homes compare canonically while display bytes remain exact. Slash/Unicode/
extended forms retain offsets. Exact-home prose redacts; space/dot siblings remain
distinct before separators and at end.

UNC recognition is a lazy linear forward pass over complete HTTP(S) spans,
including key-prefixed bracketed-IPv6 query/path text. Doubled local separators
remain local. Prefix, share, angle wrapper and punctuation survive while only the
host is replaced.

### End-to-end proof and future fields

The field inventory fails on an untested printable addition. Focused cases lock
long/8.3, Unicode/slash/extended homes, terminal/prose siblings, ordinary/extended
and wrapped UNC, local separators, key-prefixed URL spans and punctuation.

CLI tests drive `main`. The `windows && amd64` startup test drives actual
`Host.Run` with replaced DPI/discovery, observes the logger and stops before
COM/HWND. WOW64 retains its earlier architecture gate; portable packages do not
import Windows seams. Routing around sanitizer/source fails; `host.Version()` stays
exact.

### Do not regress this boundary

- Never pass a raw builder into a string-formatting subroutine.
- Never add a printable string field without extending the inventory and sentinel
  fixture.
- Never sanitize only values whose current source happens to be a path.
- Never run UNC collapse before known-home replacement.
- Never use `filepath.Clean` as a display transformation.
- Never sanitize `host.Version()` globally; keep exact data and sanitize sinks.

## Issue 99: production wiring and mutation proof

A test is authoritative only when removing or inverting the production connection
makes it fail. The fixes above use three appropriate forms:

- #94 invokes the actual named Go guard in a child test process;
- #108 invokes the actual PowerShell script in temporary Git repositories and
  source-locks the workflow artifact that calls it;
- #107 drives the release formatter and inventories every printable field.

Source/AST locks are used only where source is itself the production artifact or
where the guard reasons about source. None of the tests merely search for a helper
name and call that proof.

## Observed mutation ledger

Each mutation below was applied in a disposable repository copy on 2026-08-10.
The working tree was not modified. Every command exited non-zero for the named
contract failure.

| Property mutation | Command | Observed failure signal |
| --- | --- | --- |
| Restore basename skip or skip an injected selected-file read error | `go test -count=1 ./host -run ^TestNoNetworkListenerExercisesRealTraversalAndVerdict` | spoofed-basename child printed `PASS`; `read_failure` observed guard success |
| Remove API/loader/type binding, parenthesized/package assignment, generic string/DLL alias or extensionless module | focused network/child tests | the named form or actual child became clean |
| Drop candidate-relative mixed-radix/root-dot placement, special-scheme separator normalization, mapped-address unmapping or token scope | focused endpoint/child tests | endpoint vanished, false positive appeared or a sibling endpoint was masked |
| Route doctor/Run/CLI around sinks; drop terminal/sibling/backtick punctuation or array inventory | formatter/production tests | identity survived, useful output changed or a printable field vanished |
| Rescan URL prefixes or move the Run test out of amd64 | allocation/portable gates | linear/zero-allocation or WOW64 package gate failed |
| Decode UTF-32 as UTF-16 or authorize comment/scalar/unrelated/nested YAML | real-script/workflow tests | hidden text or inert pins obtained clean |
| Redirect Git, enable replace/graft view, scan tip or accept shallow/invalid selection | real-script tests | decoy/history input printed clean |
| Add/move/disable authority steps or override checkout | workflow lock | exact count/order/source failure named disconnect |

### Late closure mutations

The final #94/#107/#99 follow-up used six compiling disconnects: restore
built-in-only string conversion, bypass browser-special separator normalization,
skip mapped-address unmapping, remove the UNC backtick start, remove its host
terminator, and ignore reflected arrays. Each made its exact focused row fail;
the first three also exercised the named real-child guard where applicable.
The mapped mutation failed only the wildcard row: `IsLoopback` already recognizes
mapped loopback, while `IsUnspecified` needs the explicit unmapping locked here.

## Maintenance checklist

When changing these boundaries:

1. Identify the final authority that emits green, clean or paste-ready output.
2. List every input-selection step before changing detector logic.
3. Key network/doctor exceptions by normalized repository-relative URL/path and
   specific rule; key leak-scanner allowances by exact raw Git path identity,
   detector, capture and expected count.
4. Add one positive fixture, one genuine clean control and one inspection-failure
   fixture.
5. Drive the real named test, script, formatter or workflow artifact.
6. Apply the production-disconnect mutant once and record the named red signal.
7. Update the applicable decision Evidence section and this document.
8. Run the complete verification ladder; report automatic, live, uncovered and
   uncertain results separately.

## Proof boundaries

The no-listener guard is syntactic, not whole-program proof; leak-scan is a
configured known-shape scan, not a general secret scanner. Doctor cannot identify
foreign profiles, aliases, encoded paths or unrelated secrets; headless tests do not prove live Windows/WebView2 behavior.

These ceilings are not follow-up bugs; stronger claims require stronger proof first.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: bind publication scanning to exact raw Git paths, distinct index/worktree revisions, metadata-only HEAD classification and fail-closed deletion/allowance rules.
