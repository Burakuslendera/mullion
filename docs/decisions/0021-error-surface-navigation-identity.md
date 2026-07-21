# 0021. Error-surface completions are attributed by navigation id

**Status:** Accepted

## Context

0017 identifies the fallback error surface by navigation state, because the
runtime erases a `data:` document's source. Without navigation identity the
machine classified completions by order, and 0020 patched the failure side —
absorb everything while the surface loads — after a failed Retry's second
completion dead-sealed the visible surface (issue #68). Both records carried
the same class of residual, each explicitly trip-wired on identity arriving: a
foreign success inside the loading window was mis-taken for the surface's own
load, a genuinely failing surface load could not be told from a straggler at
all, and a superseded surface `Navigate` re-armed noisily. The runtime has
carried the missing identity on the base event args since the first SDK:
`get_NavigationId` on both `NavigationStarting` and `NavigationCompleted`
(WebView2.h base interfaces) — unused here because `NavigationStarting` was
unbound (issue #6's list).

## Decision

`NavigationStarting` is bound (the fifth event handler), and the host claims
the surface's own start. The claim is double-gated: `errorSurfacePending`
scopes it to the window between the host issuing the surface `Navigate` and
that navigation starting, and the reported URI must be the surface's exact
`data:` URL or a tolerated reporting variant — the empty string (issue #56's
erasure; `unverified` whether this channel shares it) or another `data:` form
(content cannot navigate the top frame to `data:`; `likely`, Chromium blocks
renderer-initiated top-level `data:` navigations). The claimed id then
attributes completions positively (`noteNavigationOutcome`):

- the surface's own **success** re-admits it, whatever landed before it;
- the surface's own **OperationCanceled** is supersede cleanup — the
  machine's classification logs at debug; the completion callback's generic
  `navigation failed` WARN still precedes it, as for every failed
  completion;
- any other failure of the surface's own navigation is the genuine seal —
  admission drops, nothing re-navigates;
- a foreign **success** commits a foreign document, so the admission drops
  (an unresolved surface navigation stays claimable and its late commit
  re-admits);
- a foreign **failure** inside the surface window is absorbed, outside it
  arms.

Two rules keep the attribution honest across generations and races. A
completion cannot precede its own navigation's start, so an identified
completion arriving while the surface's start is still unclaimed is classified
foreign - the claim window stays open for the surface's own start. And arming
starts a new surface generation: it resets the claimed id, so a superseded
generation's late cancel cannot be mis-attributed to the next arming and
unwind its claim window (a defect the pre-merge audit found and this rule
closed; the id-less fallback likewise must not clobber a claimed id).

When either id is unavailable, the machine falls back to 0020's order-based
rules verbatim, with 0020's accepted costs. A synchronous surface-`Navigate`
failure unwinds the arming (`noteSurfaceNavigateFailed`), closing the
completion-less residual 0020 recorded.

## Alternatives rejected

- **Keeping the absorb window as the only mechanism** (0020). It could not
  express "the surface's load really failed", and its success handling kept
  both mis-admission tails. Superseded by this record; its machine survives
  as the id-less fallback.
- **Claiming the next start after `Navigate`, without a URI check.** A
  foreign navigation already queued when the host navigates would steal the
  claim, and the surface's real completion would then read as foreign — the
  dead surface again, now with identity's confidence behind it.
- **Accepting only the exact URI.** If `NavigationStarting` shares the
  `GetSource` erasure, identity would never be learned and every run would
  live on the fallback. The tolerances trade that for a bounded mis-claim
  residual (below).

## Consequences

- The identity path retires 0020's ordering residuals; the seal —
  `fallback error surface load failed, not retrying` — is reachable again,
  and only for the surface's own genuinely failed load.
- **Mis-claim residual.** While the surface `Navigate` is pending, a start
  reporting an empty or foreign-`data:` URI is claimed (a failed `GetUri`
  read maps to the empty form too). Such a start can only come from the
  departing-document class the 0017 pre-commit window already admits, and the
  exposure bound is the same: the reserved methods, never `Config.Bridge`,
  until the next resolving completion. The same mis-claim also has an
  availability tail: the surface's own start then passes unclaimed, so its
  commit reads foreign and the visible surface is left unadmitted until the
  next arming. Both halves ride on the claim tolerances, which the live
  probe's tolerance-narrowing addresses.
- **The fallback inherits 0020 wholesale.** Wherever an id is unavailable,
  0020's machine and its recorded costs apply; the nine id-less test locks
  pin that the fallback stays byte-equivalent in behaviour.
- The exact shape a `data:` navigation's URI takes at `NavigationStarting` on
  a real runtime is `unverified` until the live probe; the claim tolerates
  every candidate shape, and the probe's Debug lines (`navigation starting,
  id=…, uri=…`) settle it.
- One more COM object per WebView (the fifth handler). `put_Cancel` stays
  unwrapped: the navigation-cancel gate is issue #6's work, and this record
  gates nothing.

## What would change our mind

- A live observation of the claim tolerances mis-claiming in practice — the
  tolerance set narrows to what the probe showed the runtime actually
  reports.
- A runtime reporting different ids for a navigation's start and completion —
  the correlation contract broken; the fallback would become permanent and
  this record would need superseding.
- Issue #6's `NavigationStarting` cancel gate landing: with foreign top-level
  navigations cancelled at start, most foreign-completion classes disappear
  and the machine can shrink.

## Evidence

- WebView2.h (Microsoft.Web.WebView2 SDK 1.0.2903.40,
  `build/native/include/WebView2.h`): the `NavigationStarting` args 10-slot
  order and IID `{5b495469-e119-438a-9b18-7604f25f2e49}`, the handler IID
  `{9adbe429-f36d-432b-9ddc-f8881fbd76e3}`, and
  `COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED` = 14. The vtable order
  is locked by the layout tests and the IIDs by `TestInterfaceIDs`; the enum
  values are transcription-only — no independent form exists for a test to
  compare them against.
- `host/webview_windows_test.go`: the identity locks proved fails-before
  against an identity-blind mutant of the machine —
  `TestErrorSurfaceSurvivesAForeignSuccessDuringItsLoad`,
  `TestErrorSurfaceSupersededNavigateCleansUpQuietly` and
  `TestErrorSurfaceSealsWhenItsOwnLoadFails` fail on it;
  `TestErrorSurfaceIdentityAttributesTheRetryStraggler` passes on both by
  design (the orderings coincide there — an equivalence lock). The nine
  id-less locks drive the fallback and hold unchanged.
- The pre-merge audit round (three finders plus adversarial refuters): the
  COM/ABI, ownership and plumbing lenses came back clean; the state-machine
  lens found the stale-generation id carry, confirmed by its refuter and
  closed by the generation rules above, with three more fails-before locks —
  `TestErrorSurfaceLateCancelDoesNotDisturbANewArming`,
  `TestErrorSurfaceIdentifiedCompletionsBeforeTheClaimAreForeign` and
  `TestErrorSurfaceIdlessCompletionDoesNotDestroyTheClaimedIdentity`, each
  observed failing against the pre-rule machine.

> Last updated: 2026-07-22 | Editor: Claude (Fable 5) | Change: new record — error-surface completions attributed by navigation id (issue #68 follow-up, the identity trip-wire of 0017/0020, under issue #6's event-binding work).
