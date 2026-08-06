# 0035. Frontend-controlled diagnostics are bounded before reduction and retention

**Status:** Accepted

## Context

A script in the trusted document can post an arbitrary bridge method, phase,
diagnostic kind or detail. The injected bridge once shortened only diagnostic
detail to 240 code units, but direct `chrome.webview.postMessage` bypasses that
cut; method, phase and kind had no cut at all. The host reduced each value with
`logsafe.Message`, sometimes more than once, and retained the last phase, bridge
method and asset name until the Host closed.

That made input length both a UI-thread work multiplier and retained state. A
64 KiB punctuation suffix also exposed a separate quadratic implementation in
`sanitizeToken`: prepending each suffix byte copied the growing suffix again,
for about 2 GiB of copying from one 64 KiB token. A short file name could retain
the backing storage of a much larger asset path because `FileName` returned a
slice.

A fixed prefix cut is not safe. Decisions [0025](./0025-urls-are-logged-as-urls.md)
and [0028](./0028-message-keeps-the-urls-inside-it.md) record why: cutting inside
an authority can print a believable prefix as though it were the complete host.
Dropping every interrupted URL is safe but loses the first URL in the
`window.onerror` message, which is the identifying evidence that 0028 exists to
keep.

## Decision

`logsafe.Diagnostic` is the boundary for frontend-controlled text used by host
logging or diagnostics. It limits a value to 2,000 bytes. It first selects at
most 1,936 bytes for reduction, reserving the remaining input budget for
reduction markers and seams, then enforces the 2,000-byte output limit. If plain
prefix reduction expands — a lone `.` becomes `unknown.` — the final projection
reserves the selected URL's reduced bytes before trimming that prefix. Cuts stay
on UTF-8 rune boundaries.

The bounded input reserves the first http(s) run that the production URL
reduction can actually emit with its complete authority. A normal URL therefore
survives even after a long text prefix. ASCII control whitespace cannot end an
open authority. A long path may be cut only after its authority; the reducer
validates the entire path, projects it into fixed storage by complete `%XX`,
percent-encoded-rune or UTF-8-rune units, and carries query/fragment presence
without their values. A shaped authority that cannot fit, or a candidate with a
malformed userinfo or late path byte, is rejected and scanning continues. The
selector accepts every authority shape the production reducer does, including an
empty optional port. Valid userinfo is accepted, but credentials are validated
in place and never copied into the selected URL.

`DiagnosticFileName` takes the bounded tail of a frontend-controlled asset path,
then reduces and clones the final name. All retained diagnostic strings are
bounded and detached from large input storage. `sanitizeToken` finds the start
of trailing `:;,.` punctuation by index and slices the suffix once, making that
pass linear.

The decoded method is bounded only for host logging and retention. The
application's `Config.Bridge` still receives the original raw JSON string,
byte-for-byte. The JavaScript-side 240-code-unit slice is removed because a
pre-parse cut can shorten a URL authority and because the Go boundary is the one
direct WebView messages cannot bypass.

The fallback error surface is not admitted as a diagnostic source. Although
`diagnostics.js` is injected into its `data:` document, restricted dispatch
allows only the caption, drag and resize methods the surface needs. Its
`shellReady`, `ready`, `phase`, and `diagnostic` messages are dropped before
reserved-method handling, so the failed application's retained phase and render-
watchdog summary remain authoritative. Trusted-origin diagnostic messages still
arrive complete and are bounded only at the Go receipt boundary.

## Alternatives rejected

**Bound `Message` for every caller.** Most callers are host- or runtime-owned and
have different contracts. A global behavior change would repeat the broad audit
0025 avoided. The finite bound belongs to the frontend-controlled boundary.

**Keep the JavaScript slice and make it larger.** Direct calls bypass it, and a
code-unit prefix can manufacture a shortened host before Go sees the value.
Duplicated bounds also drift.

**Take the first 2,000 bytes.** This is simple and unsafe when the cut lands in a
URL authority. It also erases the first useful URL when a long browser-generated
error prefix precedes it.

**Drop every URL touched by a cut.** Safe for identity, but it breaks the live
`window.onerror` acceptance contract from 0028. Reserving the first reducible URL
keeps that evidence without retaining the whole message.

## Consequences

Frontend-controlled log fields and retained phase, bridge and asset diagnostics
are finite. Reduction after the initial selection has constant-sized input. The
selector scans linearly, rejects cheap lexical failures without allocating, and
accepts a candidate only after the production reducer has validated its whole
path and parsed its bounded credential-free origin. Fake-scheme count therefore
cannot drive allocation bytes. URL path projection and userinfo validation also
allocate a fixed amount: a 1 MiB path or credential costs the same bounded output
storage as a 1 KiB one. A large request still has to be received and decoded,
and `Config.Bridge` deliberately receives it whole.

Long context and URLs after the selected candidate are discarded. An authority
larger than the URL reducer's budget is rejected without being shortened, and
the scan continues to the next candidate; saying less is the permanent cost of
never printing a shortened host as a whole one. Asset diagnostics keep the final
component rather than the beginning of an overlong path.

The 2,000-byte value is now a compatibility limit for host diagnostics, not for
the application bridge protocol. Raising or lowering it changes observable log
and watchdog output and requires this decision to be revisited.

## What would change our mind

- A structured diagnostic protocol that separates bounded fields and URL values
  before they cross the WebView boundary could replace the free-text selector.
- Evidence that 2,000 bytes routinely omits information needed to diagnose a
  real failure would justify a different finite value, while preserving the
  complete-authority rule.
- A WebView2 API that enforces a host-side message size before allocation could
  add an earlier transport limit; it would not remove the need to keep
  `Config.Bridge` transparent unless that public contract changes.

## Evidence

`TestSanitizeTokenTrailingPunctuationIsLinear` compares allocation counts for
short and 8 KiB punctuation suffixes and locks exact output, so the former
per-byte prepend fails without a timing threshold. Bounded-reducer tests cover
adversarial punctuation, post-reduction prefix expansion, control-whitespace
authority cuts, over-budget and parser-invalid decoys followed by a real URL,
malformed userinfo continuation, credential removal, production-valid empty
ports, every `%XX` and encoded-rune boundary, control bytes and malformed escapes
beyond the retained projection, query/fragment markers, output size, and
fake-scheme allocation bytes. They prove every accepted candidate reduces as a
URL. URL benchmarks compare 1 KiB and 1 MiB paths and userinfo values and lock
input-size-independent allocated bytes.
Host tests cover large method, phase, kind and detail values, detached retained
state, a final resource name after a long URL path, the original large request
reaching `Config.Bridge`, and a trusted diagnostic whose first URL begins after
240 characters but survives bounded Go receipt. The production callback
constructor is driven with trusted and active-fallback sources: fallback
readiness/diagnostics and `Config.Bridge` admission stay closed, one fallback
window control executes, and the trusted origin remains admitted. Run
`node scripts/test-bridge.mjs` to render and evaluate the shipped bridge template
with Node's built-in `vm`; it fails if client-side diagnostic truncation returns.

On 2026-08-06 `go test -count=1 ./...` passed on Windows/amd64. A live
WebView2 151.0.4129.59 run delivered 100,000 punctuation bytes through
`window.mullion.diagnostic`; the host reached `frontend ready`, logged the
complete `https://mullion.localhost/app/main.js?` target without its query
value, shut down cleanly, and exited 0.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: retain complete bridge diagnostics until the bounded Go boundary, and prevent fallback diagnostics from replacing application watchdog evidence.

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: continue after invalid decoys, reserve the selected URL after expanding prefix reduction, match production authority shapes, validate complete paths/userinfo with fixed allocation, and drive trusted, restricted, and allowed-fallback cases through the production callback.
