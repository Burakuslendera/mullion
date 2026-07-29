# 0031. The bytes never decide the content type, and the boundary decides the name

**Status:** Accepted

## Context

Asset responses carry `X-Content-Type-Options: nosniff`, and issue #13 records
why: it stops bytes an application serves as `text/plain` from being sniffed into
`text/html` and run in the bridge origin. No decision record covers that header;
this one is the first.

That header only protects a label mullion chose correctly, and mullion was
choosing it by sniffing. `contentTypeForAsset` fell back to
`http.DetectContentType` whenever `filepath.Ext` produced nothing the switch or
the registry MIME table recognised, and `DetectContentType` answers `text/html`
for anything opening with a tag. Measured over `os.DirFS`, on byte-identical
content:

```
uploads/note.txt    -> 200  text/plain; charset=utf-8
uploads/abc123      -> 200  text/html; charset=utf-8
uploads/x.foobar    -> 200  text/html; charset=utf-8
```

So an application serving an upload directory, a content-addressed blob store, or
anything else without a trusted extension got HTML execution in its own origin -
and `nosniff` then made the wrong label irreversible. `embed.FS` included.

A second route reached the same place through the name rather than the bytes.
Windows strips trailing dots and spaces, so `notes.txt.` opens `notes.txt`, while
`filepath.Ext("notes.txt.")` is `"."`, the switch misses, and the answer came
from the sniffer. The boundary let the name through because `hasTraversalSegment`
only refused a segment made *entirely* of dots and spaces. Measured, `os.DirFS`:

```
notes.txt   -> text/plain      notes.txt.  -> text/html
notes.txt%20 -> text/html      data.json.  -> text/html
```

Alias behaviour re-measured on go1.22.12, go1.23.12 and go1.26.5: all three open
`real.txt` for `real.txt.` and `real.txt `.

Separately, Windows device names reached the caller's `fs.FS`. Through a
passthrough `fs.FS` over `os.Open`, `ReadFile("nul")` returns zero bytes and a
nil error, so the request answered `200`. Reading `CON` is worse than an empty
body and narrower than it first looks: measured, it depends on how the
application was linked, not on mullion. Built for the console subsystem — which
is what plain `go build` produces — `os.Open("CON")` succeeds and the first
`Read` had not returned after three seconds. Built for the GUI subsystem
(`-ldflags "-H=windowsgui"`), the same code fails the open outright with "the
handle is invalid", so the request answers `500 read_error` and nothing hangs.
The hang is real for the default build and absent for the packaged one.

## Decision

The content type is decided from the name alone. `contentTypeForAsset` takes no
content, and a name it cannot classify is `application/octet-stream`.

The asset boundary refuses any segment ending in a dot or a space, because
Windows strips those and the name mullion classified would not be the file the OS
opens.

It does **not** filter Windows device names. `/nul`, `/con`, `/com1` and the
superscript `COM`/`LPT` forms are handed to the caller's `fs.FS` like any other
name. A check for them was written first and then removed; see the alternatives
below for why, and note that `os.DirFS` and `embed.FS` are both already safe
without it.

The common web extensions are named in the switch rather than left to the
machine's registry table.

## Alternatives rejected

**Keep sniffing, but refuse to return `text/html` from it.** Narrower and
tempting: it preserves `font/woff2` and `text/plain` for names the switch misses.
Rejected because it keeps mullion in the business of classifying bytes it did not
name, and the next content type that turns out to be executable in some future
engine re-opens the same hole. The rule is easier to keep than the exception.

**Pin the MIME table instead of the switch.** A frozen extension map, ignoring
the registry entirely, would make the answer machine-independent. Rejected as
larger than the problem: the registry is a decision about the *name*, which is
the model here, and pinning it means owning a table that drifts from the platform.

**Filter Windows device names at the boundary.** This was implemented first —
`CON`, `PRN`, `AUX`, `NUL`, `COM0`-`COM9`, `LPT0`-`LPT9`, `CONIN$`, `CONOUT$` and
the superscript `COM`/`LPT` forms, refused in every segment with a
`reserved_name` category — and then removed. It is written up in full because
"the asset boundary should reject `CON` and `NUL`" is an easy and
plausible-sounding thing to propose, and the work of measuring it has been done
once already.

What the filter would have covered, by `fs.FS` kind:

| backing | device name reaches the OS? | already refused? |
| --- | --- | --- |
| `embed.FS` | no — nothing touches the filesystem | not applicable |
| `os.DirFS` | no | yes, by the standard library |
| a caller's own passthrough | **yes** | no, at any Go version |

`os.DirFS` refuses the bare names on go1.22 already (`dirFS.join` →
`safefilepath.FromFS` → `IsReservedName`; renamed to `filepathlite.Localize` in
1.23 without a behaviour change), measured identically on go1.22.12, go1.23.12
and go1.26.5. So two of the three cases were never at risk, and the filter
existed for the third: a caller who wrote an `fs.FS` that passes names to
`os.Open`. Rejected because that is the caller's own code, and mullion should not
run a table lookup on every asset request to compensate for it.

Note what does *not* justify the filter, so neither is offered again: the
toolchain (measured above — `os.DirFS` was never the gap), and the extension form
(measured on Windows 11 26200, `nul.txt`, `con.log`, `aux.txt` and `aux.min.js`
all create ordinary files and `syscall.FullPath` answers an ordinary path for
each; only Go's `internal/safefilepath` comment about "some Windows versions"
argues otherwise, second-hand and unreproduced here).

**Ask Windows per request via `syscall.FullPath`.** This is what Go's own
`safefilepath` does, and the two reasons not to that come to mind first are both
wrong. Mullion *can* ask: `FullPath` is a name query that never touches the
filesystem, so it answers for an `embed.FS` name as readily as for a real file.
And it is cheap: measured at 488ns against `filepath.Ext`'s 5ns, roughly 25µs for
a fifty-asset page, beside a virtual host resolution this repository measured at
47-141ms.

Rejected because asking makes the answer a property of the running machine. The
same `embed.FS`, cross-compiled once, would serve a name on one Windows build and
refuse it on another, and the difference would surface in the field rather than
in development. Mullion is a library; a caller cannot test against every build
their users run. Neither cost nor capability is the reason, and neither should be
offered as one.

**Distinguish the `fs.FS` and only refuse for the filesystem-backed ones.** For an
`embed.FS` a device name is inert - no OS lookup happens, so the refusal is pure
cost - while for `os.DirFS` or a caller's passthrough it is the whole hazard. If
the boundary could tell them apart it could charge the cost only where it buys
something. Rejected because it cannot: `fs.FS` is an interface, and the concrete
type is routinely a wrapper - `examples/basic` passes `fs.Sub(embedded, ...)`,
not the `embed.FS` itself. A type switch would be wrong for every caller who
wraps, which is the documented way to use the API.

**Lean on `os.DirFS`, or move the floor to 1.24 and use `os.OpenRoot`.** Rejected
because the boundary's rule (issue #66, decision 0012's lineage) is that it
refuses a dangerous name itself. Note the version argument for this is *false*
and should not be repeated: `os.DirFS` refuses the bare device names on go1.22
already. The reason that holds is the caller's own `fs.FS`, which has no
protection at any version.

## Consequences

**An asset with no recognised extension is a download, not a document.** Names
outside the switch and outside the machine's registry table answer
`application/octet-stream`. An application that relied on the sniffer to serve
extensionless content must name its files.

**`mime.TypeByExtension` can answer `text/html`, and this is not a guarantee of
"never html".** An audit of this record measured the mechanism and it is not the
one written here first, which said "machine state". Go's `mime` package compiles
`builtinTypesLower` into the binary and installs it *before* the Windows registry
scan (`initMime` → `setMimeTypes(builtinTypesLower, ...)` → `osInitMime`), and the
registry path only ever calls `setExtensionType`, which stores and never deletes.
So the registry can override or extend an answer and can never remove one.
Measured: `.shtml` has a registry key carrying no `Content Type` value and
`.ehtml` has no key at all, yet both answer `text/html; charset=utf-8`.

That makes the exposure larger and more predictable than "machine state" implied:
`.htm`, `.shtml` and `.ehtml` are typed `text/html` on **every** machine, and
`.xhtml` and `.xht` are typed `application/xhtml+xml`, which a browser also
renders as a document and runs script in. The promise is that mullion decides
from the name; it is not that HTML can only come from `.html`, and it is not
contingent on the machine.

**A legitimate name ending in a dot or a space is refused.** Windows cannot
create one, but an `embed.FS` assembled on another platform can hold one, and a
cross-compiled application carrying such a name will get `403`.

**A caller who writes their own `fs.FS` over `os.Open` can serve a device.**
Measured: `ReadFile("nul")` through such an `fs.FS` returns zero bytes and a nil
error, so `/nul` answers `200` with an empty body. The request path is chosen by
the page, so no application has to *use* a device name for that to be reachable.
This is the price of not filtering, it is accepted, and it is the caller's own
`fs.FS` to fix. `embed.FS` and `os.DirFS` callers are unaffected — measured.

`host/assets_windows.go` and `TestAssetBoundaryDoesNotFilterDeviceNames` both
point here, because adding the filter back is the obvious-looking change and it
was already made, measured and reverted once.


**Reparse points are still not covered.** A directory junction inside the asset
root escapes it - measured: `mklink /J` inside the web root, then
`os.DirFS(root).ReadFile("escape/secret.txt")` returned the file outside. Neither
the boundary nor `os.DirFS` refuses to follow one. `os.OpenRoot` would, and needs
Go 1.24 against a supported floor of 1.22. Issue #103 stays open for it.

## What would change our mind

- **An application shipping a caller-written `fs.FS` that serves a device**, or a
  report of `/nul` answering `200` in the field, would make the boundary filter
  worth its cost after all. The measurement to bring is a real deployment, not a
  demonstration that the hazard exists — that much is already recorded above.
- A change in `os.DirFS` that stopped refusing reserved names would remove the
  main reason the filter is unnecessary, and should be treated as reopening this.
- Moving the supported Go floor to 1.24 would make `os.OpenRoot` available, close
  the reparse-point gap, and is the only thing that would.
- A `Config` option to supply an extension-to-type map would let an application
  serve extensionless content without mullion classifying bytes; if that is ever
  wanted, it belongs here rather than in a re-added sniffer.
- **This trip-wire was written already tripped, and is restated so it can do its
  job.** It used to read "if `mime.TypeByExtension` is ever measured returning
  `text/html` for an extension a real application ships, the table has to be
  pinned" — while the same record measured `.shtml` and `.htm` doing exactly that.
  The condition was never a future event; the mistake was believing the answer
  came from the registry rather than from Go's compiled-in table. Restated: the
  table is pinned if an application reports being served `text/html` (or
  `application/xhtml+xml`) for a name it did not intend as a document. What is
  accepted today is that `.htm`, `.shtml`, `.ehtml`, `.xhtml` and `.xht` are
  document types everywhere, which is defensible because each of them *is* a
  document extension. An extension that is not would not be.
- A change in Go's `builtinTypesLower` that maps some new extension to a document
  type would widen this without any change here, and is the reason the common
  types are named in the switch rather than left to that table.

## Evidence

- `host/asset_mime_windows.go`, `host/assets_windows.go` - the change.
- `host/asset_mime_windows_test.go`: `TestContentTypeForAsset` previously asserted
  `{"no extension, html content sniffs to html", "README", ..., "text/html"}`.
  The fix had to invert a passing test, not add one.
- `host/assets_windows_test.go`:
  `TestAssetResponseNeverTypesUnclassifiedBytesAsHTML` (issue #100's measured
  table, over `os.DirFS`), `TestAssetBoundaryDoesNotFilterDeviceNames`, and the
  new rows in `TestResolveAssetRequestDiagnostic`.
- Mutants run and killed: restoring `http.DetectContentType`; the fallback
  answering `text/html`; the old `strings.Trim(segment, ". ") == ""` rule;
  trailing-dot-only and trailing-space-only variants.
- The device-name filter, while it existed, was itself mutation-tested and its
  tests held (disabling the check, case-sensitive matching, last-segment-only
  scanning, dropping the extension strip were all caught). It was removed for the
  reason in *Alternatives rejected*, not because it was unsound. The measurements
  that justified it are kept there so the work is not repeated.
- Version claims measured with `GOTOOLCHAIN` on go1.22.12, go1.23.12 and
  go1.26.5, not inferred: `os.DirFS.ReadFile("nul")` fails on all three
  (`dirFS.join` -> `safefilepath.FromFS` -> `IsReservedName` on 1.22; renamed to
  `filepathlite.Localize` on 1.23, same behaviour). `os.OpenRoot` first appears
  in `api/go1.24.txt`.
- The superscript forms rest on Windows, not on Go's list. Asked directly, in
  three working directories: `syscall.FullPath("com¹")` answers `\\.\com¹`
  - a device path - while `syscall.FullPath("aux.min.js")` answers an ordinary
  path under the working directory. That is first-hand; the `IsReservedName`
  source it was first justified by is second-hand and no longer load-bearing.
- The `CON` hang was measured, not assumed, because the claim arrived from issue
  #103's text rather than from a run here. One source file, opened `CON` and read
  one byte behind a three-second watchdog, built twice: once with plain
  `go build` (console subsystem) and once with `-ldflags "-H=windowsgui"`. The
  console build blocked; the GUI build could not open the handle. `GetConsoleWindow`
  answered 0 in both and is not the discriminator - under a pseudoconsole it
  answers 0 while a console is present - so the subsystem the binary was linked
  for is what to read, not that call.
- `syscall.FullPath` benchmarked at 488ns/call against `filepath.Ext` at 5ns/call
  over 200,000 iterations. Recorded so the rejected alternative above is not
  reopened on a performance argument that does not hold.
- `TestNoNonASCIIInSource` rejected the superscript names written literally; they
  are built from runes.
- **Live run**, `examples/basic` at `devel`, WebView2 150.0.4078.105, Windows 11
  26200.8875, 2026-07-29. A temporary probe in the example fetched four paths from
  the real WebView and reported each one back through the bridge, so the run left
  a transcript instead of pixels:

  ```
  PROBE-style-css-is-200-text-css-charset-utf-8-OK    /style.css   (control)
  PROBE-blob-is-200-application-octet-stream-OK       /blob        (issue #100)
  PROBE-style-css-is-403-OK                           /style.css.  (issue #100)
  PROBE-nul-is-404-OK                                 /nul         (this record)
  PROBE-VERDICT-all-4-OK
  ```

  `/blob` is both the row that matters and the one the log cannot show by itself:
  `logAssetResponseDebug` skips the `other` bucket, so an
  `application/octet-stream` response is served without a line. That is why the
  probe reported through the bridge. The same bytes under the same name answered
  `200 text/html` before this change.

  The rest of the run was ordinary — `index.html` `text/html`, `style.css`
  `text/css`, `app.js` `text/javascript`, `LaunchToFrontendReadyMs=696` — and
  ended `SessionWarnCount=2, SessionErrorCount=0`. Both warnings are the probe's
  own: the `403` for `/style.css.` and the `404` for `/nul`. That a page can raise
  that count at all is issue #105's complaint; it is unchanged by this record and
  reachable from any `fetch` of a missing asset.

  **What the live run did not prove.** It read the type the WebView was served,
  not execution. `fetch` never parses a body as HTML, so no `pwned` flag could
  fire here whatever the header said. Showing execution needs an
  `<iframe src="/blob">` or a navigation to it, which under
  `application/octet-stream` becomes a download, so it was not run. The step from
  "labelled `application/octet-stream` with `nosniff`" to "not executed" is the
  engine's documented behaviour, not something this run measured.

  The probe was removed after the run; it is described here rather than kept.

- **Which switch rows are load-bearing, measured one at a time.** Deleting
  `case ".htm"` or `case ".txt"` kills no test, and the first explanation written
  here for that was wrong ("this machine's registry answers anyway"). The real
  reason is that Go's compiled-in table answers, on every machine. Asked directly,
  `mime.TypeByExtension` returns a value for `.htm`, `.txt`, `.mjs`, `.jpg`,
  `.jpeg`, `.gif`, `.webp` and `.wasm`, and returns `""` for `.woff` and
  `.woff2` — so only those last two are held up by the switch, and only their
  deletion is caught. The other eight are kept for readability, not insurance
  against machine state, and a future reader should not take a green suite as
  evidence that they are locked.
- **The name-to-file mapping is not one-to-one, and this record used to say it
  was.** "Rejecting the alias keeps exactly one name per asset" was wrong on two
  further Windows behaviours, both measured: names match case-insensitively, and
  where 8.3 generation is on a short name works too — `averylongname.html`,
  `AVERYLONGNAME.HTML` and `AVERYL~1.HTM` all opened the same file. Neither is
  closed, and neither can raise the type: the switch lower-cases, so case is
  harmless, and a truncated 8.3 extension (`.JSO`, `.WOF`, `.SHT`) falls out of
  the switch into `application/octet-stream` rather than into a document type.
  Measured on `configuration.json`, `webfontregular.woff2` and
  `servertemplate.shtml`, every short form typed `application/octet-stream`. The
  direction is always down, which is why the class is recorded rather than fixed.
- **Not measured.** That `application/octet-stream` plus `nosniff` means the bytes
  are *not executed* is the engine's documented behaviour, cited rather than
  observed here. Every claim in this record about execution rests on the label,
  not on a run that tried to execute something.

> Last updated: 2026-07-29 | Editor: Claude (Opus 5) | Change: new record - the content type is decided from the name and never from the bytes, and the boundary refuses a segment ending in a dot or a space. A Windows device-name filter was written first, mutation-tested, then removed and written up as a rejected alternative: os.DirFS refuses the bare names on go1.22 already (measured, not the 1.23 story the issue assumed) and an embed.FS never reaches the OS, so only a caller's hand-written fs.FS is exposed. The CON hang that justified the filter was measured rather than inherited - it blocks a console-subsystem build and cannot even open the handle in a GUI-subsystem one. Live run on WebView2 150.0.4078.105 confirmed /blob served application/octet-stream where it used to be text/html, with SessionWarnCount=2 accounted for.
