# 0028. A message keeps the http(s) URLs inside it

**Status:** Accepted; malformed-userinfo and unsafe open-authority control output refined by [0044](./0044-malformed-http-userinfo-is-never-emitted-by-diagnostics.md) without superseding this decision

## Context

Decision 0025 fixed the log fields whose value **is** a URL. It did not reach
the fields whose value **contains** one, and it said so: "the ones this record
does not reach (a URL embedded in a JS error sentence, for example)". Those
fields are where a blank-window report is triaged from (issue #80):

- `frontend diagnostic error, message=` — `window.onerror`, at **ERROR**
- `frontend diagnostic unhandled rejection, message=` — at **ERROR**
- `webview2 handler recovered from panic, event=…, reason=` — the recovered
  value can name the navigation it was handling

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

**Swapping the call site to `logsafe.URL` fixes nothing.** `URL` gates on the
value *literally beginning* with the scheme, so a sentence never reaches its
parser at all — it is handed back to the plain reduction on the first line and
mangled exactly as before. (Issue #80's body attributes this to `url.Parse`
rejecting the sentence; that is the wrong mechanism for the right conclusion,
and the conclusion was re-measured against the pre-change code.) So the scheme
has to be known one level down.

0025 rejected teaching `Message` about schemes, on the cost of auditing its ~90
callers for a reduction that would now be weaker, and set
`TestMessageStillManglesURLsWhichIsWhyURLExists` as the trip-wire for the day
that was revisited (renamed `TestMessageAndURLAgreeOnABareURL` when it was, in
`internal/logsafe/url_test.go`). This is that day, and what changed is the shape: the
widening below leaves a message carrying no http(s) scheme reduced as it was,
with one measured exception, so the audit is a much smaller question than 0025
faced.

## Decision

`Message` splits a message into http(s) runs and everything else. Each run goes
to `URL`; everything else gets the reduction it always got. Only those two
schemes are spared — a `file:` URL's path really is a local filesystem path and
still collapses to its file name, which is what made 0025's blanket rule safe.

Three rules keep the split from manufacturing the forgery 0025 removed. Each was
found by measurement, two of them only after the first version of this record was
written and audited.

**The scan runs before any control byte is folded.** `StripControl` turns a
control byte into a space; scan afterwards and a host with one inside it splits,
and the part before the fold gets printed as though it were the whole host.
Finding the run first keeps the byte inside it, where `isHostnameShaped` refuses
the host and the value falls back to the old reduction with no host left at all.

**A run ends only at ASCII whitespace, and a run cut short by whitespace that is
a control byte keeps its host only if the authority was already complete.** TAB,
LF and CR are deleted outright by a URL parser before it resolves a value, so to
a browser `https://mullion.local<TAB>.evil.example/x` is one URL whose host is
`mullion.local.evil.example`. Ending the run at the tab and printing what
precedes it prints a prefix of that host as though it were all of it. A space is
different and is trusted: a space really does end a URL. Once a path, query or
fragment has started the host is complete and a later cut shortens the path, not
the host — which is what keeps a URL inside a multi-line stack trace readable.

**A value bounded before it is scanned drops any run the bound interrupted.**
`URL`'s non-http fallback normally cuts to `URLLimit` and then hands the result
to `Message`. Cutting the input was safe while the reduction deleted every host
— `boundInput`'s own comment said so, and that justification is exactly what
this change invalidated. Padding chosen by whoever wrote the value lands the cut
on a label boundary, so `blob:https://cdn.<pad>.mullion.local.evil.example/x`
would log as `blob:https://cdn.<pad>.mullion.local`; `boundForScan` takes the
whole interrupted run with the cut instead. The two path-shaped exceptions keep
their identifying part before the bound: a `blob:` or `filesystem:` wrapper
reduces its inner HTTP URL to a complete origin and bounded opaque suffix, while
a `file:` value keeps its final component for `Message`/`FileName` rather than
leaking a head-truncated directory.

**A part is separated from the one before it where the source had a separator,
and only there.** Separating unconditionally would put a space inside every
quoted URL and inside `blob:https://…`, turning one token into two. Not
separating where the source had a control byte would do the opposite and weld
two URLs into one, so the foreign one reads as a path of the trusted one.

**The two functions may only call each other in one direction.** `Message` may
call `URL`. `URL`'s fallbacks that keep the scheme on the value they hand back
must call `messagePlain`, the reduction without the scan, or the two call each
other until the stack runs out. `URL`'s *non*-http fallback may call `Message`,
and does, so a `blob:` URL reveals the origin it wraps; that path terminates
because whatever `Message` hands on does begin with the scheme.

## Alternatives rejected

**Teach `isPathStart` that a drive letter is a single alpha.** The issue's own
option 2, and the narrowest-looking rule: `https:` has five letters before the
colon, `C:` has one. Rejected on three counts, each measured. It is incomplete —
narrowing the drive rule alone still yields `https:main.js` because the `//` UNC
rule matches two bytes later, and removing that rule too still yields `main.js`
because `sanitizeToken` collapses any token containing a separator. It weakens
the reduction for *every* caller rather than only for messages carrying a URL:
`atC:/Users/alice/Team Reports/q3.xlsx` reduces to `atq3.xlsx` today and to
`Team q3.xlsx` under the narrowed rule, leaking a directory. And it is a
widening of `Message` in the same family 0025 declined, without the property
that makes this one cheap.

**Splice each URL substring through `URL` and re-run the old pipeline.** The
obvious in-place version. Rejected because the spliced result still begins
`https://`, so the pipeline mangles it a second time; protecting it means
excluding those spans anyway, which is this decision.

**Substitute placeholders for URL spans, reduce, then substitute back.** Works
until a message contains the placeholder.

**Fold control bytes first, as `Message` always did, and scan afterwards.**
Simpler by one ordering constraint, and it manufactures the host forgery.

## Consequences

**A log field's message can now contain a URL.** `message=` and `reason=` are
free text that already carries spaces and punctuation, so nothing parsing them
changes — but a reader who learned that these fields never show origins will now
see them. It reaches more than the three fields above: any of the ~90
`Message`/`Reason` call sites whose value happens to carry a URL, and
`raw source=` in the rejected-web-message line, which goes through `URL`'s
non-http fallback.

**`Message` and `URL` agree on a bare URL, when the value has no whitespace in
it.** Where it does, they diverge on purpose: `Message` splits at the space and
reduces each side, `URL` treats the whole thing as one value and refuses it. The
trip-wire test was rewritten to lock the agreement it *can* claim.

**`blob:` and `filesystem:` now reveal the origin they wrap**, and so does any
non-http value quoting one. 0025 listed that as a thing that would change its
mind, and it has happened as a consequence rather than as a decision. The
wrapper stays glued to it, so the value reads as one token.

**Not byte-identical: a reduction that used to collapse to nothing now says
"unknown".** Measured over 1.26M inputs, one divergence class and no other: a
message like `// /` reduces through `FileName` to a lone space, which
`strings.Fields` drops, and the old code returned the empty string. A log field
with nothing after the `=` is worse, so the new value stands and the claim is
narrowed rather than the behaviour reverted.

**A local path inside a URL's path is no longer collapsed.** `/@fs/C:/Users/alice/…`
is Vite's dev-server form, so `http://localhost:5173/@fs/C:/Users/alice/src/main.ts`
now logs whole where it used to log `httmain.ts`. 0025 valued the old
aggression for deleting home directories from error strings; that value is
partly given up here, for http(s) paths only. A secret in a *path* rather than a
query is likewise printed — 0025 only ever promised to drop the query.

**A path immediately before a URL exposes its last segment.** The run ends the
path span, so `watching C:/Users/alice/ https://mullion.local/app` reduces to
`watching alice https://mullion.local/app` where it used to reduce to
`watching app`. `FileName` has always returned the deepest directory of a path
ending in a separator; the split makes that reachable in a message that also
carries a URL.

**`Message` itself does not impose an aggregate bound.** A message carrying many
URLs still produces output proportional to its input. Frontend-controlled
messages no longer call it without a boundary: `Diagnostic` selects bounded
input and retains the first candidate that the production URL reduction can
actually emit. Other callers remain governed by their own trust boundary; this
decision does not silently change all of them.

**Recursion depth is a property to preserve, not an accident.** `Message` → `URL`
→ `Message` is reachable exactly once, and the next hop is guaranteed to be
scheme-prefixed and therefore to end in `messagePlain`. A future fallback that
keeps the scheme *and* calls `Message` does not hang: it dies with a stack
overflow in a couple of seconds, loudly, in whichever test runs first.

**Credentials never survive an accepted URL.** A syntactically valid userinfo
component may precede the host, but selection and reduction emit only the
credential-free scheme, whole host and bounded path. A shaped authority that
cannot fit is rejected and scanning continues, so an oversized decoy cannot
displace the next complete meaningful URL.

**The scan costs a pass over every message**, guarded by a search for the
scheme's first byte rather than a prefix test at every position — measured 14×
cheaper than the obvious form on a URL-free message. Under the navigation loop
issue #77 records, the whole change costs about 6 microseconds across 20
seconds; the cost that matters is per byte of frontend-supplied message.

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
with a query dropped mid-sentence, a Windows path surviving reduction alongside
two URLs, a URL carrying another in its query (one run, not two — stepping into
the run instead of over it panics on that shape), and trailing whitespace.

`TestMessageIsUnchangedWhereThereIsNoURL` compares `Message` against
`messagePlain` over a table of paths, UNC paths, `file:`, `blob:null`, control
bytes and CR/LF, and pins the one known divergence separately. It is a fixture
table, not a property test: an audit fuzzed the property against the actual
pre-change code over 1.26M inputs and found exactly the divergence named above,
which this table would not have found on its own.

The forgery is locked from every direction found:
`TestNothingPrintsAShortenedHostAsAWholeOne` covers a C1 byte, DEL, a C0 byte,
TAB, LF, CR, VT, FF, a double quote, an apostrophe and a zero-width space inside
a host; `TestABoundedValueCannotForgeAnOrigin` covers the bounded path;
`TestTwoURLsAreNeverWeldedIntoOne` covers the seam.
`TestURLAndMessageDoNotRecurse` walks the one path where the two could loop.

**This record's first version was audited by eight reviewers and did not survive
it.** They found the TAB/LF/CR forgery, the bounded-value forgery, the welded
seam, a dead predicate, and four claims here that were false — including a
mechanism copied out of the issue body without being checked, which is the third
time that has happened in as many records. Thirteen mutants are now recorded
killed, each by the case that owns it, including seven for the rules above that
did not exist when the first version was written.

**Verified live**, `examples/basic`, runtime 150.0.4078.83, 2026-07-25. A frontend
throw whose message carries a URL with a query reached the log intact:

```
level=ERROR msg="mullion: frontend diagnostic error, message=Uncaught Error: Failed to fetch dynamically imported module: https://mullion.local/app/main.js?"
```

The host is whole, the path is whole, the `?` records that a query was there, and
the query's value - `token=s3cr3t` in the thrown string - is gone. That is the
line issue #80 was opened about, which before this change read `httpmain.js`;
both halves of the trade this record makes are visible in one line.

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: add the forward link to 0044 for malformed-userinfo and unsafe open-authority control output; historical decision text is unchanged.
