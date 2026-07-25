# 0028. A message keeps the http(s) URLs inside it

**Status:** Accepted

## Context

Decision 0025 fixed the log fields whose value **is** a URL. It did not reach
the fields whose value **contains** one, and it said so: "the ones this record
does not reach (a URL embedded in a JS error sentence, for example)". Those
fields are where a blank-window report is triaged from (issue #80):

- `frontend diagnostic error, message=` — `window.onerror`, at **ERROR**
- `frontend diagnostic unhandled rejection, message=` — at **ERROR**
- `webview2 handler recovered from panic, reason=` — the recovered value can
  name the navigation it was handling

Measured before this change:

```
in   Failed to fetch dynamically imported module: https://mullion.local/app/main.js
out  Failed to fetch dynamically imported module: httpmain.js

in   Refused to load the script 'https://cdn.evil.example/x.js' because it violates CSP
out  Refused to load the script 'httpx.js' because it violates CSP

in   navigate https://mullion.local/index.html: nil map
out  navigate httpindex.html: nil map
```

The cause is 0025's: `isPathStart` reads `<alpha> ':' <separator>` as a Windows
drive letter, which matches at the `s` of `https://`, and reads `//` as a UNC
start. 0025 routed *around* that rule for values that are URLs; the rule is
unchanged, so a sentence carrying one still loses the host to `FileName`.

**Swapping the call site to `logsafe.URL` fixes nothing.** `url.Parse` rejects
the whole sentence (`first path segment in URL cannot contain colon`) and `URL`
hands it straight back to `Message`, which mangles it exactly as before. So the
scheme has to be known one level down.

0025 rejected teaching `Message` about schemes, on the cost of auditing its ~90
callers for a reduction that would now be weaker, and set
`TestMessageStillManglesURLsWhichIsWhyURLExists` as the trip-wire for the day
that was revisited. This is that day, and what changed is the shape: the
widening below is **byte-identical for every message that carries no literal
`http://` or `https://`**, which is what removes the audit rather than arguing
about it.

## Decision

`Message` splits a message into http(s) runs and everything else. Each run goes
to `URL`; everything else gets the reduction it always got. Only those two
schemes are spared — a `file:` URL's path really is a local filesystem path and
still collapses to its file name, which is what made 0025's blanket rule safe.

**The scan runs before any control byte is folded, and a run ends only at ASCII
whitespace.** This is the load-bearing part, not an implementation detail.
`StripControl` turns a control byte into a space; scan afterwards and a host
with one inside it splits, and the part before the fold gets printed as though
it were the whole host:

```
in   https://mullion.local<U+0085>.evil.example/x
out  https://mullion.local
```

That is the forgery 0025 spent a round eliminating — "if a host is printed, it
is the WHOLE host, never a prefix of one" — and neutralising the control byte is
what would manufacture it, exactly as folding one to a space would have
manufactured a field separator there. Finding the run first keeps the byte
inside it, where `isHostnameShaped` refuses the host and the value falls back to
the old reduction with no host left at all.

**A part is separated from the one before it only where the source had
whitespace.** Separating unconditionally would put a space inside every quoted
URL — Chrome's CSP violation quotes them — and inside `blob:https://…`, turning
one token into two.

**The two functions may only call each other in one direction.** `Message` may
call `URL`. `URL`'s fallbacks that keep the scheme on the value they hand back
must call `messagePlain`, the reduction without the scan, or the two call each
other for ever. `URL`'s *non*-http fallback may call `Message`, and does, so a
`blob:` URL reveals the origin it wraps; that path terminates because whatever
`Message` hands on does begin with the scheme.

## Alternatives rejected

**Teach `isPathStart` that a drive letter is a single alpha.** The issue's own
option 2, and the narrowest-looking rule: `https:` has five letters before the
colon, `C:` has one. Rejected on three counts. It is incomplete — the `//` UNC
rule matches the same URL two bytes later, and `sanitizeToken` collapses any
token containing a separator regardless, so the host is still lost. It weakens
`Message` for *every* caller rather than only for messages carrying a URL: a
path glued to a preceding letter (`atC:\Users\alice\my file.txt`) stops being
recognised as a span and leaks a name fragment through the token pass. And it is
the change 0025 declined for a reason that has not gone away.

**Splice each URL substring through `URL` and re-run the old pipeline.** The
obvious in-place version. Rejected because the spliced result still begins
`https://`, so the pipeline mangles it a second time; protecting it means
excluding those spans anyway, which is this decision.

**Substitute placeholders for URL spans, reduce, then substitute back.** Works
until a message contains the placeholder.

**Fold control bytes first, as `Message` always did, and scan afterwards.**
Simpler by one ordering constraint, and it manufactures the host forgery above.

## Consequences

**A log field's message can now contain a URL.** `message=` and `reason=` are
free text that already carries spaces and punctuation, so nothing parsing them
changes — but a reader who learned that these fields never show origins will now
see them.

**`Message` and `URL` agree on a value that is nothing but a URL.** `URL` is
still the right call for a field whose value *is* one — it bounds the whole
value, and 0025's rule that every such field goes through it stands — but it is
no longer the only way to keep a host in a log line. The trip-wire test was
rewritten to lock the agreement rather than the old divergence.

**`blob:` and `filesystem:` now reveal the origin they wrap.** 0025 listed that
as a thing that would change its mind, and it has happened as a consequence
rather than as a decision: those values are not http(s), so they take `URL`'s
fallback into `Message`, which finds the inner origin. The wrapper stays glued
to it, so the value reads as one token.

**Recursion depth is a property to preserve, not an accident.** `Message` → `URL`
→ `Message` is reachable exactly once, through the non-http fallback, and the
next hop is guaranteed to be scheme-prefixed and therefore to end in
`messagePlain`. A future fallback that keeps the scheme *and* calls `Message`
hangs the process; the test that walks that path is what would catch it.

**The scan costs a pass over every message.** `hasHTTPPrefix` is checked at each
byte of every diagnostic string. These are log messages, not a hot loop, and the
cost is bounded by the message length.

## What would change our mind

- A caller that must never print a web origin — a value from a restricted
  source, say — which would want its own reducer rather than a weaker `Message`.
- Evidence that revealing an origin inside a `blob:` or `data:` wrapper is a
  disclosure rather than a diagnostic, which would move those back behind
  `messagePlain`.
- A third scheme worth sparing. The current answer is "http and https, because
  they carry a web origin and no local path"; anything else wants the same
  argument made explicitly rather than added to the list.

## Evidence

Commit on branch `fix-73-cancel-after-confirmation`. The three measured cases
above are locked verbatim by `TestMessageKeepsURLsEmbeddedInASentence`, together
with a query being dropped mid-sentence and a Windows path surviving reduction
alongside two URLs in the same message.

The property that removes 0025's audit is asserted as a property, not a fixture:
`TestMessageIsUnchangedWhereThereIsNoURL` compares `Message` against
`messagePlain` for paths, UNC paths, `file:`, `blob:null`, control bytes and
CR/LF, so it stays true for inputs nobody listed.

The forgery is locked from both directions by
`TestAControlByteInsideAHostCannotForgeAShorterOne` (C1, DEL and a C0 byte
inside a host: nothing hostname-shaped may be printed) and
`TestAURLRunEndsOnlyAtASCIIWhitespace` (a zero-width space in a path is
percent-encoded and the run continues; in a host it refuses the whole value).
`TestURLAndMessageDoNotRecurse` walks the one path where the two could loop —
a regression there does not fail it, it hangs the package.

Six mutants, each killed by the case that owns it: scanning after the control
fold; ending a run at any control byte; the host fallback returning to `Message`;
separating parts unconditionally; `Message` not scanning at all; and the
non-http fallback taking `messagePlain` instead of `Message`.

Not verified live. The change alters the text of three diagnostic lines that
only appear when a frontend throws, and no run has produced one; the next live
report that carries a JS error exercises it.

> Last updated: 2026-07-25 | Editor: Claude (Opus 5) | Change: new record - Message protects the http(s) URLs inside a message by delegating each run to URL (issue #80), which fires and answers the trip-wire decisions/0025 set; the scan runs before control bytes are folded, because folding first manufactures the host forgery 0025 eliminated.
