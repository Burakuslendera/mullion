# 0044. Malformed HTTP userinfo is never emitted by diagnostics

**Status:** Accepted; refines [0025](./0025-urls-are-logged-as-urls.md) direct URL fallback, [0028](./0028-message-keeps-the-urls-inside-it.md) `Message` unsafe open-authority control output, and [0035](./0035-frontend-diagnostics-are-bounded.md) frontend `Diagnostic` output without superseding them

## Context

Decision 0025 made a URL-valued diagnostic retain a complete HTTP(S) host while
dropping credentials and query/fragment values. Its accepted historical rule did
not settle what to print when `net/url` rejects a literal HTTP(S) value before a
credential-bearing authority can be separated safely.

Here, “diagnostics” includes both the URL-valued `uri` field on the two external
routes, which is reduced directly by `logsafe.URL`, and arbitrary prose carrying
embedded URL runs through `Message` or `Diagnostic`, including trusted
`WindowDiagnostic` bridge messages. Decisions
[0028](./0028-message-keeps-the-urls-inside-it.md) and
[0035](./0035-frontend-diagnostics-are-bounded.md) own the broader embedded-value
contracts. Inputs may contain malformed userinfo and may reach these reducers;
this record guarantees that diagnostic output never emits it.

The P2 reproducer was:

```
https://alice:bad%zz@evil.example?token=s3cr3t#private-fragment
```

The invalid percent escape makes URL reduction fail. Sending the whole value to
the established plain-message fallback can retain pieces of `alice`, `bad`, and
`evil.example`; it is no longer a trustworthy host projection even though query
and fragment values remain removed. Both production external routes can report
this shape after their HTTP(S) safety gate rejects it, so a diagnostic-only bug
would occur at caller-visible log lines even though no OS activation occurs.

The same unsafe authority can be embedded in frontend prose. A concrete trusted
bridge reproducer was:

```
error 'https://alice:bad%zz@evil.example'
stack
```

It reached `WindowDiagnostic`, `MarkFrontendDiagnostic`, `logsafe.Diagnostic`,
and the configured `Logger`. ASCII whitespace can also occur inside apparent
userinfo before a later `@`, so inspecting only a pre-control URL run does not
close the output boundary.

The boundary must also distinguish authority evidence from path data. For a
WHATWG special URL, WebView treats reverse solidus (`\`) as a path separator. An
`@` after that boundary is therefore path data, not evidence that the raw
authority carried credentials. No observed WebView producer contradicts that
boundary.

## Decision

For a diagnostic value containing a literal `http://` or `https://` run, normal
URL reduction runs first where the complete run is available. If it fails, the
raw authority is the bytes after the literal scheme prefix and before the first
`/`, `\`, `?`, or `#`. A literal `@` inside that authority makes every authority
component untrusted. The reducer emits only `unknown`, followed by bare `?` and
`#` presence markers read from the raw input. It emits no userinfo, host, path,
query value, or fragment value.

Reverse solidus deliberately closes this raw authority. An `@` after it remains
path data under the existing fallback policy; it does not trigger the
credential-authority refusal. Changing that would broaden the hardening beyond
the WebView/WHATWG boundary without producer evidence.

The raw-authority check is a byte scan that copies nothing and allocates nothing.
Its refusal output has fixed size, and the reducer must not retain or allocate in
proportion to malformed-userinfo input length. Existing valid HTTP(S) URLs,
including valid userinfo that normal parsing strips from diagnostics, keep their
0025 reduction.

Within an embedded URL run, TAB, LF, CR, VT, and FF stay inside the run while
the raw authority is open. This keeps any later `@` visible to the
raw-authority refusal rather than emitting its continuation as plain text.
WHATWG/WebView specifically deletes TAB, LF, and CR before parsing; all five
controls are unsafe diagnostic boundaries. A failed raw authority containing
any of them emits fixed `unknown`, even when no `@` is present. Literal space
remains the sole trustworthy open-authority terminator. After `/`, `\`, `?`, or
`#` completes the authority, any ASCII whitespace ends the URL run and preserves
the established multiline path and following-prose behavior.

Every production external-route caller keeps the same split: an accepted safe
target is handed off as the exact observed URI, while diagnostics use the
credential-free projection. A parse-invalid credential-bearing target is
rejected before the system-browser opener on both the new-window route and the
accepted navigation-cancel route; both log only `unknown` plus markers. Trusted
frontend diagnostics keep reaching `WindowDiagnostic`,
`MarkFrontendDiagnostic`, `logsafe.Diagnostic`, and the configured `Logger`, but
only the sanitized projection reaches the emitted logger line. Foreign and
fallback diagnostic mutation remains denied by its separate source gates.

## Alternatives rejected

**Use the plain-message fallback for every parse failure.** That preserves more
text, but the reproducer shows that it can preserve credential and authority
pieces after parsing has already said those roles cannot be separated safely.
The resulting field looks informative while carrying no trustworthy host.

**Recover the substring after the last `@` as a host.** Manual recovery after a
parser failure invents a second URL grammar and can turn malformed input into a
confidently wrong authority. Unknown is safer than a host the accepted parser did
not establish.

**Treat every later `@` as authority userinfo.** This would classify path data as
credentials after `/` or `\`, changing established fallback output and diverging
from WebView/WHATWG special-URL parsing. The narrower authority scan addresses
the observed disclosure without claiming a broader browser grammar.

**Reject every parse-invalid HTTP(S) value to `unknown`.** This is simpler, but it
needlessly removes bounded path evidence from authority-less and malformed-path
forms that carry no raw-authority userinfo. Those existing fallbacks remain
useful and are outside this P2 correction.

**Clamp before inspecting the authority.** An input cut can hide a later `@` or
manufacture a believable host prefix. Decision 0025 already rejected that class
of diagnostic lie; this refinement retains the full-input authority decision and
bounds only retained output.

## Consequences

Malformed credential-bearing HTTP(S) input loses all authority and path detail
in diagnostic output. Diagnosis must use the `unknown` value and any
query/fragment presence markers, or collect separate frontend evidence. This
information loss is the permanent privacy and correctness cost.

Valid userinfo remains accepted where the production safety gate accepts the URL,
and is still removed only from diagnostics. Exact URI handoff is owned by
[0043](./0043-external-routes-are-uri-only-os-activations.md); this decision does
not rewrite a caller-authorized target.

A parse-invalid value with `@` after a raw `/` or `\` boundary retains the
existing bounded plain fallback. That is intentional path treatment, not a
promise that every URL parser shares the same grammar. Encoded `%40`, Unicode
lookalikes, wrapper schemes, and other non-literal forms do not become raw
userinfo evidence under this rule.

Future caller diagnostics must preserve the split. Adding a new route that logs
the rejected raw value directly, allowing an embedded malformed-userinfo run to
fall through to plain diagnostic output, or moving the route check after an
opener call would violate this decision even if the current callers remain safe.

## What would change our mind

- A concrete WebView producer that treats a literal `@` after the first raw `\`
  as authority userinfo, or other producer evidence contradicting the documented
  `/`, `\`, `?`, `#` boundary, reopens the boundary. Parser speculation alone
  does not.
- A deliberate product decision to adopt an RFC-only authority grammar instead
  of WebView/WHATWG special-URL semantics requires a replacement decision; it is
  not a reason to silently broaden this scan.
- Either production route reaching its opener for the exact reproducer, or
  either route's diagnostic containing `alice`, `bad`, `evil.example`, `token`,
  `s3cr3t`, or `private-fragment`, is a correctness failure.
- `Diagnostic("error 'https://alice:bad%zz@evil.example'")` producing anything
  other than the exact `error 'unknown`, or a trusted multiline
  `WindowDiagnostic` logger line containing any raw credential or authority
  piece, is a correctness failure.
- TAB, LF, or CR splitting userinfo so its later `@` continuation reaches plain
  output, or VT/FF cutting an open authority to a printable prefix, reopens this
  P2.
- The malformed-userinfo allocation benchmark growing by more than 512 bytes
  between its 1 KiB and 1 MiB inputs fires the fixed-allocation tripwire.
- Evidence that the loss of all authority detail prevents diagnosis of a
  maintained-consumer incident would justify a separately specified safe
  projection; manually guessing a host remains unacceptable.

## Evidence

- `TestURLMalformedAuthorityUserinfoFailsClosed` locks the exact direct-value
  reproducer to `unknown?#` and forbids every credential, host, query, and
  fragment value.
- `TestDiagnosticMalformedUserinfoIsNeverEmitted` locks the exact embedded
  input to `error 'unknown` and forbids `alice`, `bad`, `evil.example`, and
  `%zz`.
- `TestDiagnosticControlTerminatedOpenAuthorityIsUnknown`,
  `TestDiagnosticControlInsideUserinfoDoesNotExposeItsContinuation`, and
  `TestDiagnosticRejectsAuthorityCutByASCIIWhitespace` cover TAB, LF, CR, VT,
  and FF across valid and malformed userinfo, including a control before a
  later `@`, and require fixed `unknown` output rather than an authority prefix.
- `TestProductionCallbackSanitizesTrustedWindowDiagnosticMalformedUserinfo`
  enters the production `MessageCallback` from the trusted origin, dispatches
  `WindowDiagnostic`, and requires the emitted logger line to contain the
  sanitized `error 'unknown` detail with no raw credential or authority piece.
  Existing foreign-source and fallback denial tests remain separate.
- `TestDiagnosticURLSelectionSkipsMalformedUserinfoDecoys` keeps malformed
  decoys from displacing a later meaningful URL during long-diagnostic
  selection.
- `TestURLBackslashEndsRawAuthorityBeforePathAtSign` locks the negative/control:
  an `@` after reverse solidus is path data and receives the exact established
  fallback rather than the authority-userinfo refusal.
- `TestURLMalformedUserinfoAllocationBytesAreInputSizeIndependent` compares 1
  KiB and 1 MiB malformed-userinfo inputs and enforces the 512-byte ceiling.
- `TestProductionNewWindowRejectsMalformedUserinfoWithoutDiagnosticDisclosure`
  and `TestProductionCancelRouteRejectsMalformedUserinfoWithoutDiagnosticDisclosure`
  enter both production caller paths, lock zero opener calls, and require the
  same credential-free `unknown?#` diagnostic.
- Independent combined security review found the control-split embedded
  userinfo P2 after the direct URL-valued route was hardened. It identified that
  a raw-`@` check on only the pre-control run was insufficient, drove the URL-run
  separator and raw-authority regressions, and retained the
  concrete-producer/replacement-policy tripwires. Adjacent reviewed cases
  include encoded separators and `%40`, multiple `@`, Unicode lookalikes, IPv6,
  `blob:`/`filesystem:` wrappers, exact URI handoff, allocation bounds, and the
  backslash-after-authority case.
- No live WebView producer emitted the malformed target, no `ShellExecuteW` call
  was attempted for it, and no receiving-browser behavior is claimed. This is
  deterministic headless and review evidence only.

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: name the malformed-userinfo diagnostic output guarantee and add exact embedded, unsafe-whitespace, and trusted WindowDiagnostic evidence.
