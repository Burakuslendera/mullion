# Publication evidence and leak-scan authorization

**Status:** Active

## Purpose

This document governs a narrow collision: an evidence record can legitimately
contain a build identity or artifact name that matches a publication leak-scan
detector. The detector must still fail closed until the exact documented value is
reviewed and authorized. This procedure preserves both properties: compatibility
evidence remains auditable, and an unrelated matching value cannot inherit its
authority.

It extends the [Issue 108 scanner authority](./guard-verification.md#issue-108-publication-leak-scan-authority);
that document remains the owner of scanner scope, decoding, traversal, history,
and the general allowance model.

## Observed incident

On 2026-08-28, GitHub Actions run
[`33167361118`](https://github.com/Burakuslendera/mullion/actions/runs/33167361118)
failed both Windows matrix jobs at the publication scan. The failure occurred
before the later Go, WebView2, diagnostic, or race steps. The record added for the
Windows compatibility work contained public artifact identity and artifact-name
evidence, but the scanner had no matching narrow allowances. The scan therefore
returned no clean verdict, as designed.

The result was a publication-policy failure, not evidence of a source-build,
runtime-discovery, WebView2-hosting, or GUI defect. The separate Windows x64 and
portable jobs from that run passed; they do not replace the failed Windows matrix
coverage.

## Required procedure

Before publishing evidence that matches a detector:

1. Run `pwsh scripts/leak-scan.ps1` against the intended tracked content. Treat a
   finding as a review request, not as a reason to remove provenance or weaken the
   detector.
2. Confirm that the value is intentionally public, necessary to the evidence
   claim, and bounded to a specific repository-relative document. Do not authorize
   a local path, credential, host identity, or an unexplained value merely to make
   CI green.
3. Add one allowance per detector family and documented capture. It must bind the
   exact normalized path, rule, exact detector capture, and expected occurrence count. Do
   not add a whole-file skip, a suffix or prefix match, a generic documentation
   exemption, or an allowance shared by unrelated records.
4. Add or extend the real-script test with the exact permitted record, a changed
   capture, and an under- or over-count case. The permitted record must be clean;
   every near miss must prevent a clean verdict. A helper-only matcher test is not
   sufficient.
5. Run the focused scanner test, the scanner itself, and the repository's relevant
   CGo-free Go verification commands. Then use the remote Windows matrix to verify
   the workflow consumes the new tracked source. Report the automatic result,
   live result, and remaining boundary separately.

## Evidence boundary

An allowance is publication authorization only. It does not validate the build
inputs, transfer, hash comparison, executable loading, Windows release support,
WebView2 runtime, rendering, or window interactions named by an evidence record.
Those claims keep the proof boundary stated in their canonical compatibility and
verification documents.

## Windows filesystem identity

The scanner compares its script root with Git's worktree before it selects any
tracked source. On Windows, that comparison uses the resolved filesystem identity
of each existing path rather than its displayed spelling: one API can return a
long Unicode component while another returns the same component in 8.3 form.
This is not a relaxed prefix or case-only comparison. Both paths must open, their
resolved identities must match exactly, and the Git index is resolved before the
existing containment check. A script placed below a different Git top level still
fails before any detector can print a clean verdict.

The real-script tests cover both outcomes where the filesystem exposes short and
long spellings, plus an intentionally nested scanner root that must be rejected.
Do not replace this identity check with string shortening, a common-prefix rule,
or a fallback that treats an identity-resolution failure as clean.

> Last updated: 2026-08-28 | Editor: OpenAI (GPT-5.6) | Change: define exact scanner authorization and Windows filesystem-identity boundaries for publication evidence.
