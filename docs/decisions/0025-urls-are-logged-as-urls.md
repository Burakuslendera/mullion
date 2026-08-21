# 0025. A URL reaching a log line is reduced as a URL, not as a filesystem path

**Status:** Accepted. Its trip-wire fired: [0028](./0028-message-keeps-the-urls-inside-it.md) teaches `Message` about the http(s) schemes, which this record rejected on the cost of auditing ~90 callers — the widening there leaves a message with no such URL reduced as it was, bar one narrow case it records, so the audit is a smaller question. `URL` keeps its job. Two statements below no longer hold: "the non-http(s) reduction is now frozen" — a value *wrapping* an http(s) URL, `blob:` and `filesystem:` among them, now shows the origin it wraps (the three specific re-checks that sentence names, `file:`, `:unknown` and 0021's `data:` form, were verified and do still hold) — and this record's implication that `URL` is the only way a host survives a log line (issue #80).

## Context

`internal/logsafe` had one reducer, `Message`, written for error strings that
carry Windows paths. Its path sanitizer recognises a drive letter as
`<alpha> ':' <separator>` and a UNC start as `//`. Both patterns are inside every
http(s) URL - at the `p` of `http://`, the `s` of `https://`, and the `//` after
either - so the whole URL became a "path span" and `FileName` reduced it to its
last segment. Measured, on the branch that produced this record:

```
Message("https://mullion.local/index.html?in=1") == "httpindex.html?in=1"
Message("https://example.com/")                  == "httpexample.com"
Message("https://evil.example")                  == "httpevil.example"
```

The reduction removed more than intended, never less, so it was not a
disclosure. It removed the wrong half: the host, which says *where* a navigation
went, while keeping the query, which is where a token would sit. Six log lines
were affected - navigation starting, navigation cancelled off origin (both
branches), new window routed and dropped, and both halves of the web-message
rejection.

The pressure is that these are not incidental lines. The live verifications for
issues #6, #68 and #72 were read off them, and issue #77 is open and will be
chased with them. Three verification records rest on a field that had already
lost its identifying half.

A first attempt reduced with `net/url` and kept the existing call-site clamp,
which cut the value to 160 bytes *before* parsing. A review round measured what
that produces. Padding an attacker-controlled hostname so the cut lands on a
label boundary yields:

```
real   https://cdn.<pad>.mullion.local.evil.example/session/refresh
logged uri=https://cdn.<pad>.mullion.local
```

A prefix of a URL is a different URL, and this one names the trusted origin. The
same clamp deletes an `@` sitting past byte 160, after which Go reads the
credential as the host and logs it while the real host disappears. That attempt
turned a visibly-mangled field into a confidently wrong one, which is worse: a
reader distrusts `httpcdn...mullion.local` and acts on
`https://cdn...mullion.local`.

## Decision

`logsafe.URL` reduces a value that literally begins `http://` or `https://`,
and hands everything else to the plain reduction. Its fallback is bounded too:
an interrupted candidate is dropped rather than emitted as a host prefix, a cut
ends in `...`, and raw query/fragment presence remains a bare `?` or `#`.

For a validated HTTP(S) URL it emits the scheme, the whole credential-free
authority, and the path. Userinfo is validated in place but never copied into
the projection. It drops the query and fragment, keeping a bare `?` or `#` read
off the raw value so two navigations differing only there stay distinguishable.
It bounds the *result*, never the input, and only ever cuts the path: an
authority is printed whole or not at all, and a cut value ends in `...`.

If HTTP(S) reduction fails and a literal `@` occurs in the raw authority, the
authority cannot be separated safely into credentials and host. The diagnostic
therefore becomes exactly `unknown` plus the bare query/fragment markers. The
authority scan ends at the first `/`, `\`, `?`, or `#`, so an `@` in a
parse-invalid path does not suppress the established fallback. The scan uses
string indexes only; neither it nor the `unknown` result retains input-sized
state.

A credential-free authority that is not hostname-shaped (ASCII letters, digits
and `.-_:[]`) uses the bounded plain fallback rather than being printed as an
authority. Every URL that reaches a log line goes through `URL`. Rendering
controls and invalid UTF-8 are folded at the shared logsafe boundary without
expansion. For issue #115, the rendering-control acceptance set is deliberately
exactly U+200B–U+200F, U+202A–U+202E, U+2060–U+2064, U+2066–U+2069 and U+FEFF;
it is not an exhaustive copy of Unicode's `Bidi_Control` class, and U+061C is
not included without separate scope and evidence.

## Alternatives rejected

**Teach `Message` about schemes.** The obvious fix, and it would have repaired
every call site at once, including the ones this record does not reach (a URL
embedded in a JS error sentence, for example). Rejected because `Message`'s
aggression is load-bearing elsewhere: it is what deletes a home directory from an
error string, and the same call sites feed it `file:` URLs whose path really is a
local filesystem path. Widening it means auditing every one of its ~90 callers
for a reduction that is now weaker. `TestMessageStillManglesURLsWhichIsWhyURLExists`
is the trip-wire: it fails the day `Message` changes, which is the day this
split wants revisiting.

**Reduce the query instead of dropping it.** Keep parameter names, drop values;
or keep a count. Rejected as a worse trade in both directions: names leak
(`?reset_token=`, `?patient=`), and the marker already answers the only question
the open issues ask, which is whether a query was there at all.

**Gate on the parsed scheme rather than the literal prefix.** Simpler to read.
Rejected on measurement: `url.Parse` accepts `http:evil.example` (opaque, no
host) and `http:/C:/Users/alice/x` (empty host, drive-letter path) with
`Scheme == "http"`. Reassembling those from `Host` and `Path` erases the target
entirely in the first case and prints a home directory in the second.

**Fold control bytes in a host to spaces, as `Message` does.** Rejected because a
space is the field separator in these log lines, so the neutraliser would
manufacture the injection it exists to prevent: a host of
`evil.example,<C1>user_initiated=false` becomes a second, forged `key=value`
field. Refusing the host outright is the only version that holds.

**Keep clamping the input and accept the truncation.** Rejected; see Context. It
is the difference between a diagnostic that is broken and one that lies.

## Consequences

**A query string can never appear in a mullion log again.** A future bug whose
reproduction depends on query parameters cannot be diagnosed from the log alone;
the reporter has to instrument the frontend or move the discriminator into the
path. This is the permanent cost, and it is paid on purpose.

**The non-http(s) reduction is privacy-frozen, not byte-frozen.** `URL` still
delegates ordinary fallback values to `Message`, keeping a `file:` path
collapsing to its file name, the empty source reducing to `:unknown` through
`urlOrigin` (the value issue #56's live probe was read against), and
decisions/0021's `data:` observation standing. `blob:` and `filesystem:` retain
their complete inner HTTP origin with a bounded opaque suffix; oversized
`file:` values keep the `FileName` tail before the bound. Any future change to
`Message` must re-check those three original cases.

**What counts as a host is now `net/url`'s answer, filtered.** The reduction
inherits Go's parse semantics, which are not Chromium's. Every divergence found
in review was fail-closed at the security gates - `isExternalBrowserSafe` and
`sameHTTPOrigin` re-parse the raw value and reject what Go will not read - so the
divergence affects the log only. A URL Chromium accepts can still land in the
fallback and lose its host. When failed reduction also finds literal userinfo in
the raw HTTP(S) authority, losing the entire authority is mandatory: only
`unknown` and bare query/fragment markers survive.

**An IPv6 zone identifier takes the fallback**, because `%` is refused in a host.

**Two log lines gained a seam.** `logNavigationStarting` and
`logRejectedWebMessage` exist as methods so the suite can drive them; they were
previously inline in closures inside `createWebView` and therefore unlocked. Any
new URL-bearing log line should be reachable the same way.

## What would change our mind

- `Message` learning about schemes, which would make this split redundant. The
  trip-wire test above fires on exactly that.
- A live report that cannot be diagnosed without the query, which would mean the
  bare `?` marker is too little information rather than the right amount.
- A non-http(s) wrapper whose complete inner origin cannot fit without a
  misleading host projection would argue for refusing to print that wrapper at
  all. `blob:` and `filesystem:` now retain the origin they wrap when it fits,
  with the opaque suffix bounded, while `file:` keeps its privacy-preserving
  file-name tail.
- Evidence that a truncated path with a `...` marker is being misread as a whole
  one, which would argue for refusing to print a too-long path at all.

## Evidence

The reduction is locked by `internal/logsafe/url_test.go` and
`url_host_test.go`; the call sites by `host/webview_logging_windows_test.go` and
`host/systembrowser_windows_test.go`. The mangling above, the truncation attack,
the valid credential case, and malformed userinfo with an invalid percent escape
are covered. The malformed-userinfo production-path regression enters the real
new-window callback, proves that the parse-invalid target is not handed to the
opener, and requires a credential-free `unknown?#` diagnostic.

Not verified live. The malformed-userinfo correction changes only rejected
target diagnostics and has no live WebView producer claim. The earlier URL
reduction change likewise altered only log text; the `examples/basic` checklist
item in [verification.md](../verification.md) that reads one of those lines was
not run for it - `observed` for the headless behaviour, `unverified` for the live
log.

> Last updated: 2026-08-22 | Editor: OpenAI (GPT-5.6) | Change: make parse-invalid HTTP(S) authorities with literal userinfo fail to `unknown` plus bare query/fragment markers and record the rejected new-window diagnostic boundary.
