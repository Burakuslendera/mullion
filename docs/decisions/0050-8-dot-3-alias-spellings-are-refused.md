# 0050. An asset request spelled as an NTFS 8.3 short name is refused

**Status:** Accepted

## Context

The asset boundary classifies a response from the request spelling:
`contentTypeForAsset` reads `filepath.Ext` of the name `resolveAssetRequest`
handed it, and that name is also what the caller's `fs.FS` opens. The model is
"mullion types the name it was handed" (0031), and it is only sound while the
name mullion was handed is the name the OS opens.

Windows matches a long name and the 8.3 short name it generated for it to the
same file, and the generated extension is the long one truncated to three
characters. Truncation can land inside the extension switch the long spelling
misses: `payload.htmlx` is unclassified — the switch misses it, and
`mime.TypeByExtension` answers nothing — while the generated short name ends
`.HTM` and answers `text/html; charset=utf-8`. The audit behind issue #139
measured the same opaque bytes answering `application/octet-stream` by long
spelling and `text/html` by short through both production adapters
(`os.DirFS`, `os.OpenRoot`): a renderer-chosen spelling, not the application's
own naming, decided that the bytes were executable markup on the origin the
bridge is injected into.

`hasTraversalSegment`'s comment had already measured that
`AVERYL~1.HTM` opens `averylongname.html`, and dismissed the class because "a
truncated 8.3 extension can only fall out of the switch into
application/octet-stream, never into html". The claim is false: truncation
removes characters from the end of the extension, and `.htm` is the prefix of
the very extensions the switch treats as most dangerous. Truncation demotes
`.json` to `.JSO`; it promotes `.htmlx` to `.HTM`.

## Decision

A request segment in the 8.3 short-name shape is refused, in the same place and
with the same verdict as the dot/space aliases: `is8Dot3AliasSegment` joins
`hasTraversalSegment`, the segment is rejected before any filesystem call, and
the answer is `403`. The shape is a base of at most eight characters ending in
`~` and a decimal serial, plus an optional extension of at most three
characters, matched case-insensitively and checked before `path.Clean` so a
percent-encoded `~` cannot launder it.

Classification keeps running on the name the boundary hands forward. The
refusal is what makes that sound: after it, the spellings that can reach the
classifier differ from the entry's own name only by case, and case cannot
change the class — the switch lower-cases and `mime.TypeByExtension` folds case
too. A non-ASCII short-shaped extension falls outside the grammar and reaches
the classifier, where neither the switch nor the MIME table knows it; the
answer there is the opaque default, which is the safe side.

## Alternatives rejected

**Canonicalise the spelling, then classify.** `fs.ReadDir` the parent and map
the segment back to the entry it resolves to, so the alias spelling serves and
is typed from the long name. Rejected: NTFS builds short names it cannot be
asked to reconstruct through `fs.FS` — the serial is allocation-order, and a
long name whose characters are not short-name-legal gets a hashed base rather
than a truncated one, so the mapping is not computable from a directory
listing; a match that is only probably right is exactly the promotion this
fixes, moved one step. It also costs a directory enumeration per alias-shaped
request.

**Canonicalise through Windows.** `GetShortPathName` or the handle's normalized
name would report true identity. Rejected: the boundary sees an `fs.FS`, and
`Config.Assets` may be an `embed.FS` or any caller-built one; there is no
Windows object behind it and no portable identity to ask for. Wiring
platform calls under a generic interface would tie the boundary to one
implementation of the interface it is defined over.

**Admit the alias spelling and clamp the type.** Serve it, but answer
`application/octet-stream` whenever the spelling might be an alias. Rejected:
the clamp is a classifier change keyed on the same unknowable identity, so it
must clamp every short-shaped name to the fallback — the alias spelling of a
genuinely typed long name (`AVERYL~1.HTM` for `averylongname.html`) serves
opaque bytes under a label the application's own naming contradicts, a worse
lie than a refusal that names the spelling as the problem.

## Consequences

A file literally named in the short-name shape — Windows will happily create
`report~1.txt` — is refused whether or not a long name generated it. That is a
real availability cost, unlike the dot/space refusals, which cannot reject a
name Windows itself created. It is paid on the fail-closed side: the spellings
most likely to be aliases are exactly the ones the grammar admits as aliases,
and a 403 names the request rather than mislabelling bytes. An application that
ships such a literal name renames the file.

The grammar is deliberately loose, and its cost is one-directional: it may
refuse names that are not aliases, and it will not admit a generated short
name it failed to recognise. A serial longer than the eight-character base
limit, a base before the `~`, or an extension above three bytes falls through
to the ordinary path — where a non-alias name is served correctly and an
unrecognised alias shape can only reach the classifier as an unknown
extension, which is the opaque default.

The boundary still cannot see what the filesystem does behind a spelling it
admits. A file symlink reached through `os.DirFS` renames the entry the OS
opens while the request spelling survives as the final segment; 0033 already
owns reparse points and recommends `os.OpenRoot`, which refuses them. What
this record buys is narrower: no admitted spelling may differ from the entry's
own name by more than case.

## What would change our mind

- A Windows build or filesystem that generates short names outside the shape
  (base above eight characters, serials on a different mark, extensions above
  three characters) — a measured alias spelling served as `200` would mean the
  grammar is behind the generator, and the check needs the wider shape.
- Evidence that applications routinely ship files literally named in the
  short-name shape; the availability cost is accepted today on the belief
  that they do not.
- A portable way to read canonical entry identity out of an arbitrary
  `fs.FS`; that would make the canonicalise-then-classify alternative viable
  and the refusal replaceable by an exact mapping.

## Evidence

- [Issue #139](https://github.com/Burakuslendera/mullion/issues/139) (P0
  blocker) reports the measurement: the long route `application/octet-stream`,
  the alias route `text/html`, through both production adapters on
  byte-identical inert content.
- `TestAssetProviderNeverTypesAnAliasSpellingOfAnOpaqueName` creates
  `payload.htmlx`, obtains the short name through `GetShortPathNameW`, proves
  both spellings one file with `os.SameFile`, and asserts over `os.DirFS` and
  `os.OpenRoot` that the long spelling stays opaque while the alias spelling
  is refused. Where the volume generates no short name the alias arm skips —
  the test records a volume property rather than asserting one.
- `TestIs8Dot3AliasSegment` locks the grammar; the alias rows in
  `TestResolveAssetRequestDiagnostic` and
  `TestAssetProviderResolveDiagnosticCategories` lock the refusal, and the
  `payload.htmlx` / `PAYLOA~1.HTM` pair in `TestContentTypeForAsset` locks why
  the classifier alone cannot be trusted with an alias spelling.

> Last updated: 2026-09-12 | Editor: ZCode (GLM-5.3-Flash) | Change: create the record — an asset request spelled as an NTFS 8.3 short name is refused, so classification never sees a spelling that resolves to another entry (issue #139).
