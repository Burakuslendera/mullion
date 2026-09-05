# 0029. The system-browser launch runs off the UI thread, bounded

**Status:** Accepted; [0043](./0043-external-routes-are-uri-only-os-activations.md) records the historical URI-only routing and concurrency rationale, including that the eight-worker bound is concurrency only. Current implementation: [Issue #116 bridge disposition](../bridge.md#issue-116-current-disposition).

## Context

Two routes hand a URL to the user's default browser, and both call
`ShellExecuteW` synchronously:

- `routeNewWindow`, from the `NewWindowRequested` handler (0022) — **on by
  default**, so every `window.open` and `target=_blank` click in every consumer
  goes through it;
- `noteNavigationCancelled`, from the `NavigationStarting` handler by way of the
  cancel confirmation (0023, 0027) — opt-in with `PinNavigationToOrigin`.

Both handlers run on the UI thread. That much is measured: the WebView2 event
handlers are invoked from the host's message loop on the thread that created the
WebView and holds `runtime.LockOSThread`, which
`internal/webview2/handlers_windows.go` states as the reason `Invoke` must not
touch the thread lock itself.

`ShellExecuteW` blocks until the scheme association is resolved and the target
application has started. While it does, the message loop is not pumping, so the
frameless window stops answering — no drag, no caption buttons, no resize — and
the runtime is still waiting on the handler to return.

**How long it blocks had not been measured when this record was first written**
(issue #74 says so in its own body, and the first version of this record did not
improve on it). It has been measured since, and the number is in the Evidence:
**230 ms** on the first launch after the browser process was killed, 18 ms on the
next one. The fix is worth making on the structure alone — a UI thread that can
be parked by an external process for an unknown interval is the defect, whatever
the interval turns out to be — but it is worth knowing that the interval is a
seventh of a second rather than a frame.

## Decision

**The launch runs on a goroutine of its own, and the event handler returns
immediately.** Nothing in either route needs the result: the return value was
only ever logged.

**The goroutine enters its own apartment.** `ShellExecuteW` can activate a COM
handler, and a fresh goroutine is in no apartment and may be migrated between OS
threads at any suspension point. So it locks its thread and calls
`CoInitializeEx(COINIT_APARTMENTTHREADED)`, the way `Run` does for the UI thread,
and balances it — S_FALSE included, which arrives as `ERROR_INVALID_FUNCTION` and
still owes a `CoUninitialize`, the distinction `initializeCOM` already draws. The
goroutine then returns and the Go runtime retires the locked thread with it, so
no apartment outlives its launch.

A refused apartment does not cancel the launch. `ShellExecuteW` resolves most
associations without activating a COM handler at all, so refusing to launch there
would lose a click over a condition that may not affect it. It warns instead.

**The number in flight is bounded at eight, and exceeding it is reported.** Every
launch is content-driven — a `window.open`, a link click — so the count is chosen
by the page rather than by the host, and an unbounded one is a goroutine and an
OS thread per event with a hostile document for a pump. That is the structure
0027 refused for the cancelled-navigation ledger, for the same reason. Eight is a
shape, not a measurement: the bound has to sit above what a person can produce,
because dropping a click is a real cost and a few impatient clicks during a cold
browser start are ordinary.

**The test seam stays synchronous.** `Config.openExternal` (the `host.openExternal`
field, issue #76) short-circuits ahead of the claim and the goroutine, so a
routing test still asserts on a decision rather than on a goroutine finishing.
The cost is that the bound is not on the path any routing test drives, so
`claimExternalOpenSlot` is exercised directly instead.

**`Config.Logger` must be safe for concurrent use, and now says so.** This is a
documentation change, not a behaviour change: the render watchdog and the startup
show gate have always written from timer goroutines, and `logSink` already counts
with `atomic.Int64` and states that it is invoked from goroutines with no recover
above them. The launch worker is one more caller, and the promise the embedder is
owed had never been written down.

## Alternatives rejected

**One long-lived single-worker queue shared by both routes** (issue #74's other
suggestion). One apartment for the process, launches strictly ordered. Rejected
because ordering is not a requirement here — two browser tabs have no sequence
between them — and the cost is a lifecycle the host does not otherwise have: when
the worker starts, who closes the channel, and what a teardown that never runs
(`Run` returning through a pre-loop failure) leaves behind. Per-launch goroutines
are stateless and need none of it, at the price of an apartment setup per click,
which is once per user action.

**An unbounded goroutine per launch.** Simpler still, and it is the structure
0027 rejected: a content-driven producer with nothing capping it.

**Keep the call inline and warn when it is slow.** It reports the symptom without
removing it, and the report cannot be written — the thread that would log is the
thread that is blocked.

**Move the test seam inside the goroutine, so production and test share one
path.** Tempting for symmetry, and measured to be wrong: it turns every routing
assertion into a race with a goroutine, and
`TestSafeTargetsAreHandedToTheSystemBrowser` fails under it.

## Consequences

**Launch failures are reported after the handler has returned.** The
`external open failed` and `external open skipped` warnings now land at whatever
point the worker reaches them, so a log read against the navigation lines around
them no longer shows a fixed order. They still carry the same text.

**A dropped launch is a new warning**, and it counts toward `SessionWarnCount` —
the "did this run come up clean" signal 0026 was careful about. Reaching the
bound is a genuine anomaly, so that is the right side of the trade.

**A launch can outlive the window that asked for it, or die with the process.**
The goroutine holds no reference to the window, so closing it mid-launch is
harmless — but a process that exits while a launch is in flight kills the
goroutine before `ShellExecuteW` returns, and the browser may not open. That was
true of the inline version only if the user managed to close a window whose
message loop was blocked, which they could not.

**The apartment is entered and left once per launch** rather than never. It is
the same work `Run` does once per process, on a thread that exists for the length
of one `ShellExecuteW` call.

## What would change our mind

- A timing run showing `ShellExecuteW` returns in well under a frame on a cold
  browser would make this machinery unnecessary, and the inline call — with its
  simpler ordering — would be the better code.
- An `external open dropped` line in a live report would mean eight is too low
  for real use, or that something is leaking slots; the bound should be
  understood before it is raised.
- A requirement that launches be ordered — a route that must open two URLs in
  sequence — would move this to the single-worker queue rejected above.

## Evidence

Commit on branch `audit-lock-the-ordering-rules`. Locked by
`host/externalopen_windows_test.go`: the bound admits exactly
`externalOpenLimit` launches and reports the one it drops, naming its target; a
released slot is reusable; a host that never reached `Run` already has its slots,
because a nil channel would take the `default` branch and drop every launch while
warning about each one.

The launch leaving the UI thread cannot be seen by running anything in this
suite — the seam returns ahead of the goroutine, and the goroutine's own work is
the `ShellExecute` a headless suite must never reach (issue #76) — so
`TestTheSystemBrowserLaunchLeavesTheUIThread` reads the source, the way the
navigation callbacks' guard does: `openInSystemBrowser` must not name
`procShellExecute`, the launch call must sit after the `go func()`, the worker
must pin its thread and enter and leave an apartment, and the syscall must have
exactly one call site so the check covers every path to it.

Nine mutants, each killed by the case that owns it: the launch run inline again;
`procShellExecute` made reachable from `openInSystemBrowser`; the worker's thread
lock removed; the apartment entered but never balanced; the bound removed; the
slot never released; the dropped launch silenced; `New` leaving the slots nil;
and the test seam made asynchronous.

**Verified live**, `examples/basic`, runtime 150.0.4078.83, 2026-07-25. A
temporary probe timed the two halves separately — the time the WebView2 event
handler spends inside `openInSystemBrowser`, and the time `ShellExecuteW` takes
on the worker:

```
new window routed to system browser, user_initiated=true, uri=https://example.com/
PROBE ui thread held for 0s
PROBE shellexecute worker took 230.5514ms      <- browser process killed beforehand
...
PROBE ui thread held for 0s
PROBE shellexecute worker took 18.3118ms       <- browser already running
```

The handler's own time is below the clock's resolution on both launches. The
230 ms is what the UI thread used to wait for, and it is the number the Context
above could previously only infer.

What the run did **not** establish is the symptom in the form a user would report
it. The window was dragged during a launch, but only by aiming a snap gesture
into the interval on purpose — which is an observation about the observer rather
than about the message loop, and is precisely why the probe was written instead.
The checklist item in
`docs/verification/acceptance-checklist.md#manual-acceptance-checklist` stands for anyone who wants the
user-visible version.
`go test -race` does not build on the development machine (no cgo toolchain). CI
ran it on this branch and it passed (run 30171063722) — the first race-detector
run over the new goroutine. What it touches is `host.log` (already atomic, and
now documented for concurrent use) and a buffered channel.

> Last updated: 2026-09-04 | Editor: OpenAI (GPT-5.6) | Change: restore the user-visible checklist sentence after repairing its canonical acceptance link.
