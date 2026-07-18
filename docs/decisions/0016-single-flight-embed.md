# 0016. The WebView2 embed is single-flight, and a destroyed window cancels it

**Status:** Accepted

## Context

`Browser.Embed` is synchronous from the caller's point of view but pumps the
message loop for up to a minute while the runtime's asynchronous environment
and controller creation complete (`internal/webview2/loader_pump_windows.go`).
Every message that arrives during that window is dispatched - on the same UI
thread, re-entrantly, while `host.browser` is still nil, because the commit
happens only after `Embed` returns.

Two defects followed (issue #23). A re-entrant `ensureWebView` - a `Show()`
posted from another goroutine, dispatched by the embed pump - saw the nil
`host.browser` and started a second embed: two browsers then raced for one
commit, and the loser leaked, controller, core, environment and browser child
process included. And a `Quit()` dispatched mid-embed destroyed the window with
`host.browser` still nil, so `WM_DESTROY`'s teardown skipped `ShuttingDown`;
the browser committed afterwards belonged to a window that no longer existed,
and nothing would ever release it.

## Decision

The embed is single-flight, and window destruction cancels it. Two
UI-thread-confined flags on `Host` enforce it:

- `webViewEmbedding` is set for the duration of `ensureWebView`'s create; a
  re-entrant call fails with "webview embed already in flight" instead of
  starting a second embed. The flag clears on success and failure alike, so a
  failed embed does not poison retries.
- `windowDestroyed` is set at the top of the `WM_DESTROY` case. `ensureWebView`
  refuses to start against a destroyed window, and `commitEmbeddedBrowser` -
  the one place a live browser is committed to `host.browser`
  (`navigateOrTearDown` only ever nils the field back on its teardown path) -
  tears the browser down instead of committing when the flag is up.

Both flags are read and written only on the UI thread, the same confinement
`host.browser` itself relies on.

## Alternatives rejected

**Commit `host.browser` before `Embed` returns (ownership-first).** It would
close both windows at once: the guard arms early and `WM_DESTROY` finds a
browser to tear down. But `host.browser` non-nil is, everywhere else, the
statement "this browser is embedded and usable" - bounds syncs, show paths and
the navigation callback all act on it. A half-created browser behind that field
would need a second "ready" flag anyway, and every reader would have to learn
it. Two narrow flags at the two decision points cost less than re-teaching the
field's meaning to every consumer.

**Queue the re-entrant request instead of refusing it.** Friendlier for the
caller - their `Show()` mid-embed would eventually apply. Rejected: the initial
show path already re-shows through its own gate once the embed completes, so
the queue would buy nothing there, and on the lazy path the caller gets an
error and can retry - which `ensureWebView` supports precisely because the
flag clears on failure. A queue is state with its own lifetime bugs; a refusal
is a fact.

**Tear down in `Run` with a defer instead of at commit.** A backstop, not a
fix: it would reclaim the leak at `Run`'s exit but leave the mid-embed window
where a destroyed window holds a live, usable-looking `host.browser`. The
commit-time check keeps the invariant local to the one assignment.

## Consequences

An application that calls `Show()` or drives `ensureWebView` while the first
embed is still pumping gets an error rather than a wait. That is a real,
permanent behaviour: the caller retries, or relies on the startup show gate,
which already re-shows once the embed lands. And every future path that commits
a browser to `host.browser` must go through `commitEmbeddedBrowser` - a direct
non-nil assignment in production code reopens defect 2, which is why the commit
now exists in exactly one place. (`navigateOrTearDown` nils the field on its
teardown path, and test fixtures set it directly to stage a state; neither
commits a live browser.)

## What would change our mind

- **The loader stops pumping messages during creation** - completions delivered
  without re-entering the window procedure (a dedicated pump window, or an
  async embed API). The re-entrancy this record defends against then cannot
  occur, and the flags become dead weight to remove.
- **A real application needs mid-embed `Show()` to succeed rather than fail.**
  That is the queue alternative above; a concrete consumer would reopen it with
  evidence.

## Evidence

- Issue #23: both defects, code-traced, with the pump call chain.
- `host/webview_windows_test.go`: `TestEnsureWebViewRefusesAReentrantEmbed`
  (the inner create must not run), `TestEnsureWebViewClearsTheInFlightFlag`,
  `TestEnsureWebViewRefusesAfterDestroy`,
  `TestCommitRefusedAfterMidEmbedDestroy` (the browser is torn down, not
  committed) and `TestCommitAssignsTheBrowserOnALiveWindow` - all headless,
  through the same injected-create seam `registerEventsOrTearDown` and
  `navigateOrTearDown` use.
- The live re-entrancy (a real `Show()` racing a real embed) needs a runtime
  and timing, and stays a live-only scenario; the flags' decision logic is what
  the suite pins.

> Last updated: 2026-07-18 | Editor: Claude (Fable 5) | Change: new record for the single-flight embed invariant (issue #23).
