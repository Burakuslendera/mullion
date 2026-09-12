# 0052. A browser-process exit fails closed instead of leaving a ready Host with a closed WebView

**Status:** Accepted

## Context

WebView2's `ProcessFailed` event carries a kind, and the kinds do not mean the
same thing. For `BROWSER_PROCESS_EXITED` Microsoft documents that the WebView
has already moved to the Closed state and that the application must recreate it
to recover. `RENDER_PROCESS_EXITED` creates a new renderer and an error page,
and `RENDER_PROCESS_UNRESPONSIVE` is advisory that may still recover. Mullion
registered the event and forwarded the kind to a Host callback that only logged
it (issue #155).

That log-only callback left a specific strand alive. The browser process was
gone and the control Closed, but the host's committed `host.browser` field made
every later `ensureWebView` return success - the closed control masqueraded as
embedded - while the ready path had already stopped the render watchdog, so no
timer remained to observe that nothing painted. The parent window, the message
loop and `frontendReady` all stayed live: a permanently unusable window that
every surface described as healthy. The ERROR line meant the failure was
observed; nothing owned a response to it.

## Decision

`ProcessFailed(BrowserProcessExited)` fails closed. The production callback
latches a per-Run terminal cause exactly once - guarded by a session flag and
by the browser's own `ShuttingDown` state, so duplicate and stale events
schedule nothing - and posts one tagged private command (`WM_APP+30`) through
the existing token-validated dispatch. The command's body is a plain window
destroy, so the audited `WM_DESTROY` teardown performs the one ownership
transition: stop the watchdog and show gate, shut the controller down, clear
`host.browser`, post `WM_QUIT`. The callback itself stays observation-only: no
destroy, no re-embed, no nested message loop inside the runtime's event
dispatch.

The message loop exits with `ErrBrowserProcessExited`, not success, whenever
that cause was latched - including when a queued `WM_QUIT` preempts the command
and the ordinary exit teardown performs the destroy. Recovery failure therefore
has no separate path: there is no recovery, and a Host that ran the terminal
transition is reusable for a later Run because `beginRun` resets the cause with
the rest of the session state.

Every other kind keeps the observation-only wait policy it had: the ERROR kind
line per event, no lifecycle action. The runtime recovers those kinds itself,
and the existing navigation-classification and error-surface machinery owns
whatever the user then sees.

## Alternatives rejected

**Recreate the WebView in a new generation.** Microsoft's own sample schedules
recreation off the event handler, and the host has the machinery: uncommit the
browser, clear the ready state, re-arm the watchdog, re-embed. Rejected for
this issue: an automatic re-embed re-runs the whole embed pump, script barrier
and first navigation on a thread that just reported its browser died, inside a
session whose readiness, error-surface and navigation state all describe a
frontend that no longer exists. Getting that reset complete and provable is a
feature, and the P0 requires the unusable state to end now; recreation remains
available as a later policy layered on the same terminal seam.

**Fail closed synchronously inside the callback.** Calling `DestroyWindow`
from the `ProcessFailed` handler would dispatch `WM_DESTROY` - and with it
controller `Close` - while WebView2 is still dispatching the event that
reported its own process died, exactly the re-entry the vendor guidance warns
against. Rejected: the destroy is scheduled through a posted command instead,
and the callback provably touches no COM object.

**Treat renderer exit or unresponsiveness as terminal too.** Both kinds leave
the control alive, and the runtime already supplies the recovery artifacts (a
new renderer, an error page). Terminating the window on an advisory would turn
a recoverable slow page into a dead app. Rejected: the wait policy stays, and
the kind matrix is pinned so a future kind must be classified explicitly
rather than inherited into either behavior.

**Return success and let the caller discover the closed window.** The loop's
exit code cannot distinguish a user close from the teardown, so without a
recorded cause `Run` would report a normal close after a browser death.
Rejected: the latched cause makes the terminal outcome observable through
`errors.Is` while leaving the ordinary close path untouched.

## Consequences

After a browser-process exit the window closes and `Run` returns
`ErrBrowserProcessExited`; an application that wants a visible error surface or
an automatic restart owns that decision at its call site. The availability cost
is real and accepted: one bad browser-process death ends the session instead of
repairing it.

The policy is kind-exact. A future `COREWEBVIEW2_PROCESS_FAILED_KIND` value
that closes the control the way a browser exit does must be added to the
terminal branch explicitly; kinds Mullion has never classified stay on the
observation-only policy, which is safe only because none of them ends the
control.

The live presentation of the strand - the blank window, its input behavior -
was never reproduced; the vendor state transition and the missing owner are the
evidence. Live confirmation on supported runtimes stays with the issue's
reproduction contract and the Windows 10/11 parity row in issue #129.

## What would change our mind

- A WebView2 capability that reports browser-process exit without closing the
  control, or that recreates it in place, would remove the contract this
  fail-closed decision rests on; the policy should then be revisited.
- A hosted-application requirement for unattended recovery that cannot tolerate
  a closed window would justify the recreate policy; it must then also specify
  the readiness/error-surface reset the terminal seam now ends with, and the
  decision supersedes this record.
- Evidence that renderer-exit events can close the top-level control would
  make the observation-only policy unsafe and require the matrix to be split
  per kind again.

## Evidence

- [Issue #155](https://github.com/Burakuslendera/mullion/issues/155) (P0
  blocker) documents the runtime transition and the log-only owner at
  commit 43a719f, with Microsoft's `ProcessFailedKind` contract and the
  vendor sample's no-nested-loop guidance as the source boundary.
- `TestProcessFailedBrowserExitPostsOneTaggedTerminalCommand` pins the
  exactly-once tagged post, the observation-only callback and the terminal
  outcome; `TestProcessFailedOtherKindsKeepTheObservationOnlyPolicy` and
  `TestProcessFailedKindGetterFailureStaysObservationOnly` pin the kind matrix
  and getter provenance.
- `TestProcessFailedBrowserExitTerminalLeavesNoLiveHostWithAClosedWebView`
  pins the full transition: the committed guard masquerades before it, and
  after it the ownership fields are cleared, the watchdog stopped with a bumped
  generation, re-embed refused, `Run`'s outcome terminal, and the next session
  clean.
- `TestProcessFailedBrowserExitAfterDestroyRecordsOutcomeWithoutPost` and
  `TestProcessFailedAfterBrowserShutdownIsIgnored` pin the teardown-window and
  stale-browser deliveries; `TestProcessExitCommandAppliesOnlyForTheOriginatingRun`
  pins the tagged dispatch behind the latched browser-exit cause.
- `TestDualTerminalApplicationNamesTheOutcomeOwningCause` pins the shared
  command's log priority: with both terminal causes latched, the applying log
  names the browser-process-exit cause that owns `Run`'s error, mirroring
  `terminalOutcome`.

> Last updated: 2026-09-13 | Editor: ZCode (GLM-5.3-Flash) | Change: create the record — ProcessFailed(BrowserProcessExited) fails closed through one tagged terminal command, other kinds stay observation-only (issue #155) — and pin the shared command's log priority, which names the terminal cause that owns Run's error when both causes latch.
