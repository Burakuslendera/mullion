# 0043. External routes are URI-only OS activations

**Status:** Accepted

**Current implementation (not part of this historical decision):** Historical uses
of “current” below describe state at decision time; the [Issue #116 current
disposition](../bridge.md#issue-116-current-disposition) wins now.

## Contents

- [Context](#context)
- [Decision](#decision)
- [Alternatives rejected](#alternatives-rejected)
- [Consequences](#consequences)
- [What would change our mind](#what-would-change-our-mind)
- [Evidence](#evidence)

## Context

Mullion has two ways to take an HTTP(S) destination out of its single WebView:

- `NewWindowRequested` attempts to route `window.open` and `target=_blank` by
  default, subject to successful runtime handling and required getters, under
  decision 0022.
- `PinNavigationToOrigin` optionally cancels an off-origin top-level navigation
  and routes it only after WebView2 accepts `put_Cancel`, under decisions 0023
  and 0027.

Decision 0029 moved both launches off the UI thread and bounded concurrent
workers, but it did not define whether that bound was also a rate limit. The
older records also used shorthand such as "the user's real browser" without
separating what Mullion controls from what Windows and the selected handler may
do. Issue #75 asks for those missing consequences and for the semantic loss when
a navigation carrying request state is reduced to a URI.

This record owns only issue #75 items 1, 2 and 4: routing frequency, the OS/browser
boundary, and URI-only request semantics. Item 3 is fallback authority, owned
separately by
[decision 0037](./0037-event-values-preserve-getter-provenance.md) and not
restated here. [Issue #87](https://github.com/Burakuslendera/mullion/issues/87)
is CLOSED/NOT_PLANNED: its stale-navigation-ID availability cost is an accepted
risk, not evidence for changing either boundary. It reopens only if the exact
`A-start/B-start/A-ConnectionAborted` ordering reaches fallback arming, as
specified by [decision 0024](./0024-benign-abort-in-process.md).

No current routing residual in this record is P0, P1, or P2. The accepted
residuals are low/P4 availability, privacy, and compatibility consequences.
Decision 0037 separately owns its conditional fallback-authority P2 tripwire.

Non-overlap is explicit. [Issue #116](https://github.com/Burakuslendera/mullion/issues/116)
owns separate origin-boundary documentation findings. The Windows 10/11 parity
umbrella [#126](https://github.com/Burakuslendera/mullion/issues/126) and its
content-boundary child
[#130](https://github.com/Burakuslendera/mullion/issues/130) own paired platform
evidence, not routing-policy selection or revision; #130 explicitly excludes
absorbing #75.

## Decision

Keep both routes. `NewWindowRequested` remains default-on as an attempted route.
Mullion first calls `PutHandled(true)`; only a successful call suppresses the
runtime's detached window. `GetUri` and `GetIsUserInitiated` must then both
succeed before Mullion routes a safe target. Getter failure produces no host
launch after successful suppression. `PutHandled` failure produces no host
launch and may leave the runtime's popup behavior in effect. The
`PinNavigationToOrigin` route remains opt-in; when enabled, it routes a safe
off-origin target only after the runtime successfully accepts cancellation.
Both routes drop every scheme except HTTP and HTTPS.

When the relevant WebView2 URI getter succeeds, Mullion passes that observed
HTTP(S) URI unchanged to the `ShellExecuteW` `open` activation. Log messages are
a separate boundary. A validated URL projects its whole credential-free
authority and bounded path, with only bare query/fragment markers. If HTTP(S)
reduction fails and the raw authority contains literal userinfo, no authority is
trusted: the diagnostic is `unknown` plus those bare markers. Diagnostics
therefore expose no userinfo, query values, or fragment values. [Decision
0044](./0044-malformed-http-userinfo-is-never-emitted-by-diagnostics.md) owns this
malformed-userinfo refinement. URI fidelity at the exact URI handoff does not
promise how the receiving browser interprets userinfo, query, or fragment.

Every handoff is a fresh OS URL activation. It is not replay of the WebView2
navigation or popup request. Mullion preserves no HTTP method, body, request
headers, referrer, opener relationship, WebView profile, selected system-browser
profile, WebView-held cookies or stored credentials, extension state, or other
session state. Userinfo remains part of the unchanged URI. Windows chooses the
registered handler, and that handler chooses its process, window, tab, profile,
session, and resulting network requests. Mullion neither selects nor observes
those choices.

At most eight production launch workers may be in flight. A ninth concurrent
handoff is dropped and warned. This is only a resource-concurrency bound around
workers that may block in `ShellExecuteW`; it is not a lifetime, per-document,
per-origin, or time-window rate limit. Once workers finish, the same admitted
document may continue creating fresh activations indefinitely.

WebView2's `IsUserInitiated` value remains a diagnostic classification on both
routes. Mullion logs it after redaction but does not gate routing on it and does
not treat it as proof of contemporaneous physical input. In particular, browser
user-activation state and WebView2's event classification can outlive or differ
from the JavaScript action that directly triggered an event.

## Alternatives rejected

**Gate on `IsUserInitiated`.** This is a reasonable popup-blocker policy and
WebView2 exposes the value so a host can adopt it. It would also silently change
the current default for scripted application flows. The value classifies an
event according to WebView2; it is not an authenticated physical-input token.
The live timer probe below demonstrates why the distinction matters. Keep the
signal visible for diagnosis rather than making it authority without a separate
product requirement and reliability evidence.

**Add a lifetime budget, time-window throttle, or URI de-duplication.** Any of
these would bound nuisance activations after the eight-worker resource bound has
cleared. They would also invent document lifetime, reset, retry, and equality
semantics, and could drop intentional repeated opens without an observed rate
problem in a maintained consumer. The route is reachable from renderer content
already executing in a document or frame, including foreign content; it grants
no new bridge authority. Retain the simple contract until the rate tripwire below
fires.

**Rewrite the URI or strip userinfo before activation.** Removing credentials or
normalising a destination can reduce surprising browser behavior, but it also
changes a caller-observed navigation and can break query-dependent links. The
host instead restricts the scheme, hands the successfully observed URI through
unchanged, and redacts only diagnostics. Receiving-browser behavior remains
outside the guarantee.

**Allow HTTPS only.** This narrows transport exposure, but rejects legitimate
HTTP destinations, especially caller-owned loopback development services. The
current security boundary prevents arbitrary protocol-handler activation; it
does not enforce transport policy for external web destinations.

**Infer POST, correlate `WebResourceRequested`, or replay a request.** A
`NewWindowRequested` event supplies a target URI, not an HTTP request to replay.
`NavigationStarting` can expose headers, but this host does not acquire request
method/body authority there, and correlating resource events would be
race-prone and ambiguous. Reconstructing or replaying credentials, headers, and
bodies into a different browser session would create a larger security boundary
while still failing to preserve opener and profile state. Applications that
require those semantics need an explicit application protocol, not a heuristic.

**Keep the external popup or navigation in WebView2.** This would retain more of
the embedded browser context, but either loads an off-origin document beside the
injected bridge or requires a real multi-window host with trusted chrome and
lifecycle ownership. It reverses the single-window containment chosen by 0014,
0022, and 0023. An application can instead render an in-page flow deliberately.

## Consequences

A successful handoff gives the OS the exact observed URI. Query values therefore
cross into an ambient browser environment even though Mullion's log contains
only a `?` marker. Userinfo and fragments cross the host boundary too, but the
browser may ignore, remove, reinterpret, or retain them; Mullion makes no browser
behavior guarantee. The selected handler may reuse an existing profile/session
or choose another one. Documentation must not call either outcome guaranteed.

A cancelled form navigation can lose its POST semantics. The fresh activation
commonly becomes a GET for the URI, with no body or original headers, so the page
shown may differ from the form submission the user intended. This is semantic
data loss, not a Mullion request-body leak: Mullion never obtains or transfers
the body. No POST detection, correlation, or replay heuristic is added.

The eight-worker ceiling limits simultaneous goroutines, pinned OS threads, and
blocking shell calls. It does not prevent renderer content from opening an
unbounded number over time, whether that content is the application, a foreign
top-level document while pinning is off, or a foreign frame. This route grants no
new bridge authority. Consumers that need a stronger renderer threat model need
containment above this API; route diagnostics are not authorization.

An OS/browser activation may cause network activity beyond the primary URL, such
as favicon, feed-discovery, extension, update, prefetch, or policy traffic. The
host cannot attribute such requests merely because they follow a handoff. Live
evidence records observed ancillary requests without assigning a cause.

Mullion still cannot promise a new process, window, or tab, a particular browser
profile, authentication state, credential forwarding, query/fragment treatment,
or the exact resulting HTTP request. It promises only scheme admission, exact
URI handoff, credential-free diagnostics (including `unknown` plus markers for
a rejected parse-invalid HTTP(S) authority carrying raw userinfo), bounded
in-flight workers, and route timing relative to successful WebView2
handling/cancellation.

## What would change our mind

- **Rate-policy tripwire:** a reproducible maintained-consumer incident in which
  renderer content causes sustained activations after workers clear and the
  embedder cannot contain it without Mullion support requires a separately
  specified lifetime and reset policy. Eight workers being briefly full is not
  that evidence.
- **Gesture-authority tripwire:** a product requirement to reject non-physical
  opens plus live evidence across both routes and supported runtimes that an
  available signal reliably represents the required physical gesture would
  justify a gate. `IsUserInitiated=true` alone does not fire it.
- **Request-semantics tripwire:** a supported flow that must preserve method,
  body, headers, opener, or WebView profile/session across an external route
  invalidates URI-only activation. The replacement must expose an explicit API
  and threat model; request-event heuristics do not satisfy it.
- **URI-fidelity tripwire:** any production path that mutates a successfully
  observed HTTP(S) URI before `ShellExecuteW`, or emits its userinfo/query/fragment
  values in diagnostics, violates this decision and must be fixed rather than
  documented as browser behavior.
- **Handler-policy tripwire:** a requirement to select or attest a particular
  browser profile/session cannot be met with `ShellExecuteW`; it requires a new
  integration decision rather than an inference from the default-browser label.
- **Scheme tripwire:** evidence that allowing HTTP creates an unacceptable
  maintained-consumer risk, with loopback and compatibility costs accounted for,
  would reopen HTTPS-only admission. Preference for HTTPS alone does not.

## Evidence

- Microsoft's
  [`ICoreWebView2NewWindowRequestedEventArgs`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2newwindowrequestedeventargs)
  reference exposes the target URI, `Handled`, and `IsUserInitiated`; it says the
  WebView popup blocker is disabled so the app may decide whether to block a
  non-user-initiated popup. It exposes no request method, body, headers, opener,
  or profile-transfer operation.
- Microsoft's
  [`ICoreWebView2NavigationStartingEventArgs`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2navigationstartingeventargs)
  reference defines URI, initiation, navigation identity, cancellation, and
  request-header observation. It does not provide a request body or a transfer
  operation, and header observation is not browser-session preservation.
- Microsoft's
  [`ShellExecuteW`](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew)
  reference defines the `open` verb as opening the item passed in `lpFile` through
  shell association. It does not promise browser process, window, tab, profile,
  session, or resulting HTTP semantics.
- `host/systembrowser_windows.go` owns the two host routes, HTTP(S) scheme gate,
  unchanged URI handoff, redacted route diagnostics, and eight in-flight slots.
  The WebView2 adapter calls the new-window host route only after
  `PutHandled(true)`, `GetUri`, and `GetIsUserInitiated` all succeed; failures are
  locked to no host callback/launch, with runtime popup behavior unclaimed after
  `PutHandled` failure.
  `TestProductionNewWindowRoutesNonGestureURIExactlyAndReducesDiagnostics`
  locks exact safe-target handoff and the credential-free validated projection.
  `TestProductionNewWindowRejectsMalformedUserinfoWithoutDiagnosticDisclosure`
  and
  `TestProductionCancelRouteRejectsMalformedUserinfoWithoutDiagnosticDisclosure`
  enter both production callbacks and lock no opener call plus `unknown?#` for a
  parse-invalid credential-bearing target. [Decision
  0044](./0044-malformed-http-userinfo-is-never-emitted-by-diagnostics.md) owns that
  refinement and its narrower backslash path boundary.
  `TestSafeTargetsAreHandedToTheSystemBrowser`,
  `TestExternalOpenSlotsAreBoundedAndSayWhenTheyRunOut`, and the focused routing
  contract tests lock the remaining observable host boundaries. `ShellExecuteW`
  itself remains a live-only side effect.
- The corrected live probe on 2026-08-21 used Windows build 26200.9168,
  Go 1.26.5, and WebView2 151.0.4129.93. A 750 ms timer recorded
  `direct_user_gesture=false` while `navigator.userActivation.isActive=true` and
  `hasBeenActive=true`; the one host route reported `user_initiated=true`. The
  primary server request was `GET /issue75?token=synthetic`, with no
  `Authorization`, `Cookie`, or `Referer`; the fragment was not server-visible.
  Fifteen unexplained feed-shaped GETs and one favicon GET followed and remain
  unattributed. The probe made one route, used no remote target, and closed and
  cleaned its owned resources. It did not test POST, a rate flood, or browser
  profile/session selection.
- [Verification records](../verification-records/2026-08.md#2026-08-records) preserve the
  malformed-profile first run as inconclusive and the corrected run as the sole
  behavioral evidence. [Issue #75](https://github.com/Burakuslendera/mullion/issues/75)
  owns the original consequence inventory and historical corrections; this
  decision, not the tracker text, owns the accepted current routing contract.

> Last updated: 2026-09-02 | Editor: ZCode (GLM-5.3-Flash) | Change: repoint the 2026-08 verification-records link at the verification-records/ folder.
