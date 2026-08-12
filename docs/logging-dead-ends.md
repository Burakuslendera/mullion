# Logging and identity dead ends

An archive of **things that did not work** in the two places where this host has
to say something true about a document: identifying its own error surface when
the runtime reports no source for it, and reducing a URL for a log line without
destroying the half that identifies it.

Split verbatim out of [lessons-and-dead-ends.md](./lessons-and-dead-ends.md) when
that file reached the 400-line reference-doc limit; the sections and their
numbering within this file are new, the bodies are not. The same working rule
applies: a claim is only "verified" if it was observed at runtime on a real
window. Passing tests, clean logs and plausible static analysis have each been
wrong here.

## Contents

- [1. A data: document has no reportable source](#1-a-data-document-has-no-reportable-source)
- [2. A sanitiser that mangles its input plausibly is worse than one that fails](#2-a-sanitiser-that-mangles-its-input-plausibly-is-worse-than-one-that-fails)
- [3. The short version](#3-the-short-version)

---

## 1. A data: document has no reportable source

**Symptom.** The fallback error surface loads, the window appears — and every
bridge message the page posts is rejected, `untrusted source, origin=:unknown`,
ten in a row: dead caption buttons on the one page whose whole job is having
working caption buttons (issue #56).

**Tried and dead.**

- **Recognise the surface by its message source.** The `WebMessageReceived`
  args' `GetSource` returns the **empty string** for a data: document — not the
  data: URI, not `null` (measured live, runtime 150.0.4078.65).
- **Ask the core at message time.** `ICoreWebView2.get_Source` — the current
  top-level document's URI — returns the empty string for the same document.
  The runtime erases the data: URI at both levels, so there is nothing to
  match; a `GetSource` binding written for this was deleted as dead code.

A diagnostic trap on the way: the rejection log's origin form collapses every
schemeless source — empty and `null` alike — to the same `:unknown`, so the raw
value can only be learned from a live probe. The rejection path now logs the
reduced raw source at debug for exactly this reason.

**Instead.** The host itself knows when it navigated to its own surface, so
identity comes from a UI-thread state machine (`noteNavigationOutcome`,
`errorSurfaceActive` in `host/errorsurface_windows.go`): the empty source is admitted
only while the surface is the current document, and only for the reserved
window controls — `Config.Bridge` stays origin-gated (decisions/0014). The
accepted costs of that identification, and what would retire it, are recorded
in decisions/0017, 0020 (the failed-Retry absorb window, issue #68), 0021
(navigation-id attribution, which retired 0020's ordering and keeps it only
as the id-less fallback) and 0024 (which failures deserve the surface at all).

**The tail of it: a failure status is not a diagnosis.** The machine armed on
any failed completion, and `ConnectionAborted` turned out to mean two unrelated
things — a dead endpoint when the caller serves the frontend over a socket, and
a navigation the runtime abandoned and restarted when mullion serves it in
process. Arming on the second replaced a live frontend with the fallback page,
whose Retry aborted the same way (issue #72). The fix is not a better reading of
the status, which carries no more information: it is to ask where *that*
navigation's bytes came from, which the host knows from the navigation id it
recorded at `NavigationStarting`. The first attempt keyed on the config mode
instead and two audit passes refuted it — the mode says where the frontend is
served from, not where the top frame went, and with the cancel gate off those
differ (decisions/0024).

**Lesson.** When the runtime's own identity channel reports nothing, parsing
harder is not the answer; the identity you need must come from state you
already own. The same holds one level up: when a status code is ambiguous, do
not tune the reading of it — find the state you already hold that disambiguates
it.

---

## 2. A sanitiser that mangles its input plausibly is worse than one that fails

`internal/logsafe.Message` was written to strip Windows paths out of error
strings. Its drive-letter rule is `<alpha> ':' <separator>`, which is inside
`http://` at the `p` and inside `https://` at the `s`; its UNC rule is `//`,
which every `scheme://host` URL contains. So every URL it ever reduced came out
as its last path segment with a clipped scheme welded to the front:

```
https://mullion.local/index.html?in=1  ->  httpindex.html?in=1
https://evil.example                   ->  httpevil.example
```

(Historical: `Message` no longer does this. Issue #80 found that the same rule
still ate the URLs sitting *inside* a message, which no call-site swap could
reach, and decisions/0028 taught `Message` about the two http schemes. The
lesson below is about how long it went unnoticed, which is unchanged.)

Nothing caught this for the life of three issues. The output still looked like a
diagnostic: it had the right shape, it started with `http`, and it named a real
file. The live verifications for #6, #68 and #72 were all read off `uri=` fields
that had already lost their host, and nobody reading them noticed, because a
mangled value that looks reduced is indistinguishable from a value that was
correctly reduced.

Two dead ends came out of fixing it.

**Bounding the input.** The first fix reduced with `net/url` but kept the
existing call-site clamp, which cut the value at 160 bytes *before* parsing.
A URL prefix is a valid URL. Pad a hostname so the cut lands on a label boundary
and `mullion.local.evil.example` logs as `mullion.local` - not garbled, not
marked, just a different and more trustworthy host. The same clamp deletes an
`@` past the limit, after which Go reads the credential as the host and prints
it. The first version was strictly worse than the bug: it converted visible
garbage into confident wrongness, and a reader acts on the second.
The rule that came out of it: **bound the reduction, never the input, and never
cut a host at all - print it whole or do not print it.**

**Folding control bytes to spaces.** `Message` neutralises a control byte by
turning it into a space. These log lines are `key=value, key=value`, so for a
host - where Go permits `,` and `=` unescaped - the neutraliser *manufactures*
the field separator it exists to defend against. A host of
`evil.example,<C1>user_initiated=false` becomes a second, forged field. Refusing
the host outright was the only version that held.

**Reducing a value is not encoding a field.** `Message`, `Diagnostic`, and
`DiagnosticFileName` deliberately preserve ordinary punctuation, including `,`
and `=`, because free text must remain readable and URLs must remain URLs. That
made a frontend diagnostic, asset path, or bridge method such as
`ready, forged=1` produce two apparent fields in the comma-separated
`key=value` grammar. The correction is not to make every message unreadable:
`Field` and `FieldFileName` compose the existing bounded/privacy reducers and
fold those two delimiters only for structured values. Free-text `message=`,
`reason=`, and `detail=` values retain their existing reducers and are rendered
after authoritative fields. A structured producer must select the field reducer;
its caller's input origin does not make the grammar safe.

**The first of those came back, twice.** Issue #80 taught `Message` to keep the
URLs inside a sentence (decisions/0028), and "never cut a host at all" had to be
re-derived in two shapes nobody was watching for. A value bounded *before* it was
scanned: `URL`'s non-http fallback cut its input at 160 bytes and handed the rest
to a `Message` that now keeps hosts, so the cut could land on a label boundary
inside one - reached through a function whose own comment justified the cut with
"Message deletes the identifying part of the value anyway", which that same
change had just made false. And a run ended by a TAB, LF or CR: a URL parser
deletes those three bytes before it resolves a value, so ending a run at one and
printing what precedes it prints a prefix of the real host. Neither is a
truncation, and both print a shortened host as a whole one. A rule of the form
"this is safe because X holds" is owed a re-check by whoever changes X.

**A finite frontend boundary is the deliberate exception, not a blind input
cut.** Issue #88 made the work and retained-state problem concrete: bridge
methods, phases and diagnostic details could be arbitrarily long, and one 64 KiB
punctuation suffix made the old reducer copy about 2 GiB. Frontend-controlled
diagnostics now select at most 1,936 bytes before reduction and emit at most
2,000. The selector accepts the first candidate the production URL reduction
can actually emit with its authority whole. An over-budget or parser-invalid
decoy is rejected and scanning continues. Valid userinfo is reduced to the
credential-free host.

A long path is validated in full, then streamed into fixed output storage by
whole `%XX`, percent-encoded-rune and UTF-8-rune units; query/fragment presence
is retained. The path scan is linear, a malformed escape beyond the visible
projection still rejects the candidate, and allocated bytes do not grow between
a 1 KiB and 1 MiB path. This is why the bound does not use a fixed prefix or
`boundForScan`'s safe-but-destructive rule of deleting the URL that a cut
interrupts. The resulting string is cloned before retention. Application bridge
payloads are outside this diagnostic limit and still pass to `Config.Bridge`
unchanged ([0035](decisions/0035-frontend-diagnostics-are-bounded.md)).

The decisions are [0025](decisions/0025-urls-are-logged-as-urls.md) and
[0028](decisions/0028-message-keeps-the-urls-inside-it.md).

## 3. The short version

1. **A data: document reports no source.** Identify your own surfaces from navigation state you already hold, not by parsing the source harder. (§1)
2. **An ambiguous status code is not a diagnosis.** The same failure status meant two different things; the state that told them apart was already recorded at the navigation's start. (§1)
3. **A sanitiser can remove the wrong half.** Reducing more than intended is not automatically safe: the URL reducer deleted the host and kept the query, which is the identifying half gone and the disclosing half kept. (§2)
4. **Never use a blind input prefix.** Bound the parsed output, or select bounded input only after proving a URL authority stays whole; a well-formed lie beats visible garbage past every reader. (§2)
5. **A reducer is not a field encoder.** Free text may retain punctuation, but a
   value in a comma-separated `key=value` record must fold the delimiters before
   it is logged. (§2)

> Last updated: 2026-08-12 | Editor: OpenAI (GPT-5.6) | Change: record the structured-field delimiter-injection dead end and the Field/FieldFileName boundary that preserves the existing free-text and URL reducers.
