# 0048. Successful caption normalization completes before exposure, not in the initial request

**Status:** Accepted; supersedes only the creation-time/lifetime boundary in [0003](./0003-keep-caption-bits.md)

## Context

Decision [0003](./0003-keep-caption-bits.md) records the reason the frameless
host must retain the captioned-window contract: DWM, USER32 and the shell use
`WS_CAPTION`, `WS_SYSMENU`, `WS_THICKFRAME`, `WS_MINIMIZEBOX` and
`WS_MAXIMIZEBOX` to classify the top-level window and provide its frame,
system-menu and Snap behaviour. Its sentence that the window is “created with”
all five bits and keeps them for its “whole life” correctly preserves the
steady-state invariant, but it also makes the initial `CreateWindowExW`
argument and the post-create lifetime boundary sound identical.

The host implementation may request an initial style without `WS_CAPTION` or
`WS_SYSMENU`. `CreateWindowExW` may synchronously run `WM_NCCREATE`,
`WM_NCCALCSIZE` and other creation callbacks before it returns an `HWND`, so
the host cannot normalize a returned handle before those callbacks. On the
successful normalization path, after the call returns and while the window
remains hidden, the host reads its effective style, applies the frame profile,
refreshes the non-client frame and completes DWM setup. A normalization is
successful only when the required style, frame-refresh and DWM native calls
succeed and the post-apply readback matches the active five-bit profile. After
that hidden steady state, the successful path may publish or use the `HWND`
externally, use it for WebView2, show it, or expose it through a host callback.

Current production style, frame-refresh and DWM failures, and a post-apply
profile mismatch, are logged; the existing flow continues and can still reach
publication. Those fail-open paths are not successful normalization. This
record neither accepts nor hides them, turns them into a supported success
guarantee, nor changes production behaviour. `Config.Logger` diagnostics may
run synchronously during the initial-request, effective pre-apply, post-apply
and DWM normalization phases. They are diagnostic exposure, not readiness or
`HWND` publication; their existing reentrancy risk and failure behaviour are
outside this timing decision.

Diagnostics must not collapse these phases. **Requested initial style** means
the style argument supplied to `CreateWindowExW`; **effective pre-apply style**
means the style observed on the returned `HWND` before normalization;
**post-apply style** means the style after the five bits are applied and the
frame is refreshed; and **steady state** means the hidden, DWM-configured state
that is ready for external use. A creation callback is not a steady-state
observation merely because it received a valid `HWND`.

## Decision

The five-bit caption contract is a hidden post-creation normalization, not a
requirement on the `CreateWindowExW` style argument. A successful normalization
means the required style, frame-refresh and DWM native calls succeed and the
post-apply readback matches the active five-bit profile. On that path, after
`CreateWindowExW` returns, the host implementation synchronously establishes
`WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX`,
refreshes the frame with `SWP_FRAMECHANGED`, and completes DWM setup while the
window is hidden. After reaching that hidden steady state, the successful path
may publish or use the `HWND` externally, create or navigate WebView2, call
`ShowWindow`, allow user interaction, or invoke a callback that exposes the
host. `Config.Logger` diagnostic callbacks are permitted during normalization;
they are not readiness, window publication or host-exposing callbacks, and
their existing reentrancy and failure behaviour is outside this decision.
Creation callbacks that run inside `CreateWindowExW` are explicitly outside the
five-bit guarantee. If a required style, frame-refresh or DWM native call fails,
or the post-apply readback does not match the active profile, current production
code logs and continues and can still reach publication. This ADR neither
accepts nor hides that fail-open path, treats it as supported success, nor
changes it. After successful normalization, the five bits remain set until the
`HWND` is destroyed. The frameless appearance still comes from the
client-extended `WM_NCCALCSIZE` path, not from clearing the styles.

## Invariants

1. Successful normalization requires the style, frame-refresh and DWM native
   calls to succeed and the post-apply readback to match the active five-bit
   profile.
2. After successful normalization, the style contains
   `WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX |
   WS_MAXIMIZEBOX`. No later frame, DWM, Snap, menu or DPI path may clear any
   of them.
3. The successful path order is **requested initial style → creation callbacks
   inside `CreateWindowExW` → effective pre-apply style → post-apply style and
   frame refresh → DWM setup → hidden steady state → publication/use**. The
   first phase may omit `WS_CAPTION` and `WS_SYSMENU`; the last phase may not.
4. On the successful path, external publication or use of the `HWND`, WebView2
   creation or navigation, `ShowWindow`, user interaction, and host-exposing
   callbacks follow hidden steady state. `Config.Logger` diagnostic callbacks
   may occur during every normalization phase; they do not establish readiness
   or publication, and their existing reentrancy and failure behaviour is
   outside this timing decision.
5. A diagnostic stage named `before` or **effective pre-apply** is not evidence
   of successful steady state. Audits must distinguish the requested initial,
   effective pre-apply, post-apply and steady-state observations.
6. `WM_NCCALCSIZE` continues to remove only the visual frame. It does not
   weaken the five-bit lifetime invariant after successful normalization.
7. Existing style, frame-refresh and DWM failures and post-apply profile
   mismatches are logged and may continue to publication. They are outside the
   supported successful-normalization guarantee; this record neither accepts
   nor hides that fail-open behaviour and makes no production change.

## Compatibility and risk

This preserves the observable steady-state contract recorded by 0003. The host
retains implementation latitude in the initial request. After successful
normalization, the captioned-window style exists before caller-visible or
WebView-visible work. No API, frame profile, menu rule, Snap route or production
behaviour changes by this documentation decision.

The accepted timing risk is a short hidden interval in which USER32 or DWM may
observe the effective pre-apply style and creation callbacks may receive an
`HWND` without all five bits. That interval is deliberately not described as a
captioned-window guarantee. On the successful path, publication, WebView work,
showing, interaction and host-exposing callbacks follow hidden steady state.
`Config.Logger` diagnostic callbacks remain permitted before it; they are not
readiness or window publication. Existing native-call failures and profile
mismatches may still log and continue to publication, but those fail-open paths
are outside this timing decision's supported successful-normalization guarantee
and are neither accepted nor hidden here.

## Alternatives rejected

**Require all five bits in the initial `CreateWindowExW` request.** This keeps
the literal reading of 0003, but it turns an implementation detail of the
initial request into a stronger callback-time guarantee without evidence that
the hidden interval changes native behaviour. It also prevents a host style
profile from using a deliberately minimal initial request while preserving the
same steady state.

**Treat creation callbacks as proof of the final style.** A callback inside
`CreateWindowExW` runs before the host can apply the returned-handle
normalization. Calling its observation “steady state” would hide a real phase
boundary and make the diagnostics misleading.

**Apply the five bits after publication, WebView setup or showing.** This would
expose USER32/DWM/shell classification and first-use behavior to a transient
style, and is rejected even if the initial request remains minimal.

**Leave the bits optional for the lifetime of the window.** This is the failure
0003 documents: the system menu, shell animation, resize/minimize/maximize
semantics and full Snap contract can degrade. The permanent five-bit invariant
is retained.

## Consequences

On the successful normalization path, the initial style argument and the
effective style on the returned `HWND` are separate contract points. A host
style profile may vary the request, but successful normalization must converge
synchronously to the same five-bit hidden steady state before exposure and
must preserve those bits through destruction. A diagnostic or evidence report
that quotes only the requested initial style, a creation callback, or the
pre-apply audit cannot claim successful normalization or the lifetime
invariant.

The cost is that code running from a creation callback cannot assume the
captioned-window contract. Such code must defer any operation that needs the
five bits until post-apply steady state. `Config.Logger` callbacks are the
explicit diagnostic exception: they may run during normalization but do not
establish readiness or publication, and their existing reentrancy and failure
behaviour remains outside this decision. Existing style/frame/DWM failures and
post-apply profile mismatches likewise remain outside the supported
successful-normalization guarantee and unchanged.

## Evidence ceiling

The source-level ordering is observable in `host/win32_call_windows.go`:
`CreateWindowExW` returns before `applyNativeWindowStyle` is called. The
normalization ordering and frame/DWM operations are in
`host/style_windows.go`, and `host/style_audit_log_windows.go` records the
style at named stages. Those facts establish terminology and the intended
successful hidden sequence; they do not prove that a transient pre-apply style
changes USER32 or DWM classification, initial `WM_NCCALCSIZE`, first paint,
menu state, Snap behaviour or DPI behaviour.

The current source also shows the failure boundary: style and frame-refresh
errors are warned by the create path, DWM errors are warned by the DWM helper,
and a post-apply profile mismatch is warned by the style path. The existing flow
continues and can still reach publication. Those paths are not evidence of a
successful steady state. This record neither accepts nor hides that fail-open
behaviour, turns it into a supported success guarantee, or changes production
behaviour.

A style readback, bounds log, headless seam, frame count, or successful build
is therefore not evidence that the transient interval is harmless. This record
claims no final live observation and changes no verification record; the
remaining native behaviour is bounded by the rollback condition below.

## What would change our mind

Move all five bits into the initial `CreateWindowExW` request if reproducible,
controlled evidence shows that the hidden interval changes any of the
following compared with an otherwise identical initial-five-bit control:

- USER32, DWM or shell window classification;
- the initial `WM_NCCALCSIZE` result or first painted frame;
- system-menu presence or item state;
- Win+Z, edge-drag, restore or other Snap behavior; or
- a DPI-aware size, frame or transition result.

The rollback evidence must identify the requested initial style, effective
pre-apply style, post-apply style and steady state, and must show the
consumer-visible difference on the same supported Windows build, display/DPI
setup and runtime. It must distinguish a successful-normalization comparison
from a log-and-continue failure or profile-mismatch path. A difference only in
a diagnostic line, a callback-time readback, or an uncontrolled timing
correlation does not fire
the trip-wire. A confirmed trip-wire makes the initial request part of the
five-bit contract; it does not permit removing the permanent lifetime
invariant.

## Evidence

- [0003](./0003-keep-caption-bits.md) remains the historical rationale for the
  five-bit shell, menu, frame and Snap invariant; this record supersedes only
  its literal creation-time/lifetime boundary.
- `host/win32_call_windows.go`, `host/style_windows.go` and
  `host/style_audit_log_windows.go` provide the current implementation
  vocabulary and ordering described above, including native-call failure and
  post-apply profile-mismatch paths that log and continue.
- The governing frame, window-acceptance and Snap references link this record
  for the hidden post-creation boundary. `Config.Logger` diagnostics remain
  permitted during normalization but are not readiness evidence. No
  production, test, workflow or verification-record file is changed by this
  decision.

> Last updated: 2026-09-05 | Editor: OpenAI (GPT-5.6) | Change: define successful normalization by native-call success and profile readback while recording the existing fail-open publication paths.
