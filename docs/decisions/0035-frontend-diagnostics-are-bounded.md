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
most 1,936 bytes for reduction, reserving the remaining budget for reduction
markers and seams, and enforces the 2,000-byte limit again on the result. Cuts
stay on UTF-8 rune boundaries.

The bounded input reserves the first http(s) run that can be reduced with its
complete authority. A normal URL therefore survives even after a long text
prefix. ASCII control whitespace cannot end an open authority. A long path may
be cut only after its authority; a boundary escape or UTF-8 rune is completed,
and query/fragment presence is carried without their values. An authority that
cannot fit is omitted rather than shortened into a believable host.

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
selector scans linearly and rejects the URL parser's failure conditions without
allocating, then copies the first structurally valid fixed-size candidate; the
bounded reducer parses that candidate once. Fake scheme count therefore cannot
drive allocation count. A large request still has to be received and decoded,
and `Config.Bridge` deliberately receives it whole.

Long context and later URLs are discarded. A first URL with an authority larger
than the URL reducer's budget is discarded too; saying less is the permanent
cost of never printing a shortened host as a whole one. Asset diagnostics keep
the final component rather than the beginning of an overlong path.

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
adversarial punctuation, control-whitespace authority cuts, a boundary `%XX`,
query/fragment markers, too-long authorities, output size and fake-scheme
allocation counts. They also prove every accepted candidate shape reduces as a
URL and that ten malformed authorities cannot displace the next meaningful URL.
Host tests cover large method, phase, kind and detail values, restricted-source
diagnostics, detached retained state, a final resource name after a long URL
path, and the original large request reaching `Config.Bridge`.

On 2026-08-06 `go test -count=1 ./...` passed on Windows/amd64. A live
WebView2 151.0.4129.59 run delivered 100,000 punctuation bytes through
`window.mullion.diagnostic`; the host reached `frontend ready`, logged the
complete `https://mullion.localhost/app/main.js?` target without its query
value, shut down cleanly, and exited 0.

> Last updated: 2026-08-06 | Editor: GPT-5.6 | Change: accepted the 2,000-byte frontend diagnostic boundary, complete-first-URL rule, detached retained values and raw Config.Bridge transparency for issue #88; strengthened lexical selection against control-whitespace authority cuts, split escapes, malformed candidates and fake-scheme allocation growth.
