# 0005. Capability detection is `QueryInterface`, never a version compare

**Status:** Accepted

## Context

This library talks to the runtime's own client DLL rather than to the SDK loader
([0001](./0001-own-webview2-com-layer.md)). The compatibility gate lives in the
loader. Bypass the loader and the gate goes with it, and that is not a deduction -
it was measured:

- Declaring `TargetCompatibleBrowserVersion = "999.0.0.0"` against a 150 runtime
  **succeeds**. Nothing refuses it.
- A null target is rejected with `E_INVALIDARG`, and an implausible one
  (`"1.0.0.0"`) with `ERROR_FILE_NOT_FOUND` - so the property must be supplied,
  and it still gates nothing.

A version number on this path proves that a string was accepted. It does not
prove that the object about to serve the call implements the interface.

## Decision

Every optional capability is detected by asking the object: `QueryInterface` for
the interface, use it if it answers, fall back if it does not.
`ICoreWebView2Settings9` (non-client region support), `ICoreWebView2Settings5`
(pinch zoom), `ICoreWebView2Controller3` and the rest are all reached this way. An
older runtime returns `E_NOINTERFACE` - a clean "no", handled as a recoverable
condition rather than an error.

## Alternatives rejected

**Compare the runtime version.** It reads well, it is one line, and the
documentation supports it: non-client region support is stable from runtime
131.0.2903.40 onward, and the package already has a `CompareVersions` that
implements the SDK's ordering. Two things kill it. Without the loader there is
nothing enforcing a floor, so the comparison is advisory at best; and even a
runtime new enough by number does not prove the interface is present on the
object in hand. `QueryInterface` answers the actual question, and its answer is
true by construction.

**Probe by calling the method and inspecting the HRESULT.** Tempting, and it
works for genuinely optional *methods*. It cannot work here: a missing interface
is not a failing call, it is a call through a vtable slot that does not exist.
The result is a crash inside the browser process, not an error code.

## Consequences

Every optional feature costs a `QueryInterface` and a matching `Release`, plus a
fallback branch of its own - and each degrades **independently**. Zoom control is
disabled on the base settings interface; pinch zoom needs `Settings5` and is
skipped with a warning when it is absent; non-client region support needs
`Settings9`, and when the query fails the frontend is left on the classic
JavaScript drag path rather than being told the title bar may live in the page.
That ordering is load-bearing: enable first, tell the frontend second, or an old
runtime gets a page that suppresses its own fallback bar and a window that cannot
be dragged at all.

Version numbers are still discovered - the newest installed runtime is chosen with
`CompareVersions`, and its version is reported back as the target - but a version
is never allowed to decide a capability.

## What would change our mind

The runtime gains a trustworthy capability declaration: either a manifest that
enumerates the interfaces it implements, or a version floor that is enforced
*inside* the runtime DLL rather than in the loader we do not use. Either would
make a declared version mean something at the ABI boundary. Until then, a version
compare here is a guess wearing a number.

## Evidence

- `internal/webview2/loader_windows.go`, `resolveTargetVersion`: the three live
  findings above, recorded next to the code they constrain.
- `internal/webview2/loader_windows_test.go`, `TestResolveTargetVersion`: locks
  the rule that the target version is never null and never invented.
- `webview_tab_strip_windows.go`: `QuerySettings9` gates non-client region
  support and logs `fallback=classic_titlebar` when it fails; `QuerySettings5`
  gates pinch zoom the same way.
- `docs/architecture.md` ("the version floor lives in the loader, not in the
  runtime") and `docs/snap-and-nonclient-region.md` section 4.
