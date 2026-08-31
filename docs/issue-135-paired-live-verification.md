# Issue #135 paired exact-tree live verification

## Acceptance judgment

The 2026-08-31 Windows 11 paired run completes only the
[Issue #135 exact serialized first-document registration item](./verification.md#3-manual-acceptance-checklist).
The untagged artifact establishes ordinary first-document behavior; the tagged
artifact establishes the forced post-real-callback Show/Quit interleave and exits
successfully. The limits below remain limits: this is not a general graceful-close,
Win10, release-scheduling, or byte-equivalence claim.

## Evidence and source identity

The local, git-ignored evidence root is
`dist/issue-135-exact-tree-20260831/final-paired`. Its
`evidence-manifest.csv` has 36 entries; `evidence-manifest-meta.json` records the
manifest SHA-256 as
`B5E6DA1688FAEEB5EBCE4A2B2B7FF0FF8B6BC8C3050C9B0990D8B6DAFEC13C66`,
zero forbidden Go/module files, a clean eight-package `go list ./...`, and zero
`dist` package hits.

Both artifacts came from this frozen identity:

- HEAD: `f7860ae8804b27954bf33708d16a92797b4d66f0`.
- Tracked diff SHA-256:
  `409586B47D3D530C7A7FA816288E1851A828858E4F52E857BF4A223FEFE26332`.
- `source-identity-initial.json` records the thirteen-entry aggregate identity
  `4914E61763B02D073E7DB79771E0151B93BB6F2102C9F87C8A0CD91C130F76F9`.
  Its derivation recipe was not preserved, so this is not the literal CSV hash.
- The independently recomputed literal SHA-256 of
  `untracked-source-manifest-initial.csv` is
  `00FD27577869D8A26D94DA65A2C2FC2AFE6810EDA04A720CC1150B68F859BCFF`.
- The CSV's individual path, byte-count and SHA-256 rows, plus its entry in the
  parent `evidence-manifest.csv` identified by `B5E6DA1688FAEEB5EBCE4A2B2B7FF0FF8B6BC8C3050C9B0990D8B6DAFEC13C66`,
  preserve the source boundary despite the unavailable aggregate recipe.
- The initial, pre-live, pre-tagged and final identity records agree; final
  `git diff --check` exited zero with no missing, extra, or mismatched untracked
  source entry.

Those identities name the artifact source freeze, not the later documentation
tree. Current-versus-freeze divergence is documentation-only; no runtime artifact
build input changed.

The builds were:

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOAMD64=v1 go build -trimpath -buildvcs=true -o mullion-basic-windows-amd64v1-untagged.exe ./examples/basic
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOAMD64=v1 go build -tags mullion_script_completion_delay_diag -trimpath -buildvcs=true -o mullion-issue135-diag-windows-amd64v1-tagged.exe ./internal/cmd/mullion-issue135-diag
```

Both PE files are Windows/amd64, CGo-free, `GOAMD64=v1`, and built with Go
1.26.5. Their identities are:

- Untagged: `D7D79A86A64124349F28D833E6AB4AD653E612F407C8E837836B7C5871197754`.
- Tagged: `A0F5D1314A041DFAA5FAD2D01382F0323CBEC39EA8E71FEE27D037A3C80B4769`.

The host was Windows 11 Home Single Language 25H2 build 26200.9168, amd64,
with WebView2 Evergreen 151.0.4129.107. The doctor record reports two
1920x1080 displays at 125% and 100%.

## Untagged first-document evidence

The frozen manual stderr log hashes to
`0EBF09387CD75986FEDFF1985ED35E0BE4EEE65B24B85DD8A7846D1F4E47AF72`.
Its owned chronology is:

1. `asset serving ready` at 12:58:22.914;
2. `injected scripts registered` at 12:58:22.915;
3. `navigate requested` at 12:58:22.915;
4. first-document `document created`, DOM-loaded and resize-cursor-installed
   diagnostics;
5. frontend shell ready and visible;
6. bridge `Ping` received and completed;
7. window-loaded/navigation-completed diagnostics and frontend ready.

Registration therefore precedes Navigate, and the first document has the
diagnostic, bridge, drag, and resize scripts. The startup summary reported
`SessionWarnCount=0` and `SessionErrorCount=0`; the frozen log likewise has zero
Mullion-owned WARN and ERROR lines. The completed `Ping` is owned evidence that
the Ping/Pong bridge request/response path ran. The stderr evidence does not preserve the
visual `Pong from Go` payload, so no independent exact-artifact visual Pong claim
is made.

The manual run attributed title dragging through `titlebar drag requested`,
`titlebar drag applying`, and an `HTCAPTION` diagnostic. Its pointer resize
interactions produced two `bottom-right` / hit 17 operations. They did not
produce a pure-right event and are not described as one.

A prior raw log from the identical untagged artifact hashes to
`1E7C90B35D797AC4751ED3599B7AB2DFCDC23CBA88A9020BB1E564A08BF20E2A`.
At 12:55:11.936–12:55:12.171 it records `edge=right`, `resize requested`, hit
11 (`HTRIGHT`), and matching client/controller width growth from 784 to 904
while height remained 673. Those raw entries independently supply the right-edge
evidence. The live session record attributes them to one prepared, no-retry
Computer Use attempt, but no serialized action receipt was retained; that tool
and attempt-count attribution is not independently reconstructable offline.

The later manual X interaction made PID 51008 disappear, but the detached
launcher retained no exit code and the Mullion log retained no owned
destroy/message-loop-exit chronology. It is not strict graceful-close proof.
The terminal `Chrome_WidgetWin_0` unregister error 1411 is an external Chromium
line, not a Mullion logger WARN/ERROR and not part of the owned zero counts.

## Tagged adversarial evidence

The tagged stderr log hashes to
`E8EA9CA6732A4FA226EE176FF6A0CEFFD58B368C0BD5E80109966C516AD5E8D4`.
Exactly one run used PID 26156 from 13:00:26.8489857 to 13:00:31.0036718,
lasting 4.155 seconds without timeout and exiting zero. Its exact sequence was:

1. real required callback 1 held;
2. Show requested/applied exactly once and rejected exactly once as
   `webview embed already in flight`;
3. callback 1 explicitly released and its real completion published;
4. real required callback 2 held;
5. Quit requested exactly once;
6. callback 2 explicitly released and its real completion published;
7. Quit applied once, followed by destroy, WebView2 shutdown, run-exit teardown,
   the cancellation-owned terminal barrier error, the diagnostic assertion, and
   `diagnostic passed`.

Each required held/publication marker occurred once. Counts were zero for asset
serving ready, injected scripts registered, frontend render timeout/watchdog,
Navigate requested, frontend ready, and native host ready. The log has no literal
`markerFailures=0` line. Exit zero is code-gated by the exact command's
`MarkerDeliveryStats` requirement that `dropped=0` and `timedOut=0`; that is a
code-enforced condition, not a separately printed marker. Its two owned ERROR
lines are expected protocol evidence: rejected re-entrant Show and the
cancellation-owned pre-start Run failure.

## Proof ceilings

- The tagged artifact differs from the untagged artifact by design. It proves a
  supported Runtime survived the forced post-real-callback interleave; it does
  not prove the untagged Runtime callback schedule and is not byte-equivalent to
  the release artifact.
- The untagged X interaction is not strict graceful-close proof, and the manual
  run did not independently capture a visual Pong payload or repeat a pure-right
  gesture.
- Local `-race` remains unavailable because `gcc` is absent.
- The sole Win10 workflow attempt belongs to issue #129. `registervm` failed
  closed with `E_FAIL 0x80004005` because config UUID
  `42b3d126-9c0e-4a59-9431-7b1522c0684e` matched an existing VM. Per the
  one-attempt limit there was no retry, boot, guest hash, or artifact execution;
  no same-artifact Win10 claim exists.

> Last updated: 2026-08-31 | Editor: OpenAI (GPT-5.6) | Change: clarify final paired Issue #135 source identity and live-attribution proof boundaries.
