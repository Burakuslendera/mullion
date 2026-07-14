# 0001. The WebView2 COM layer is written here, not taken from a third-party binding

**Status:** Accepted

## Context

The host began on a third-party Go WebView2 binding, and for the first weeks that
was the right call: it embeds the browser, wraps the settings object, and saves
you from vtables, IIDs, refcounts and callback trampolines. Three independent
pressures then arrived, and none of them went away:

- **A missing interface.** The binding's settings wrapper stops at
  `ICoreWebView2Settings6`. `ICoreWebView2Settings9`
  (`IsNonClientRegionSupportEnabled`) is what a custom title bar rests on, and it
  is absent on the upstream main branch too - so waiting for a release does not
  help. Raw COM had to be written anyway, reaching around the abstraction while
  it still owned the object.
- **A shipped DLL.** The binding's path runs through `WebView2Loader.dll`, which
  has to travel beside the binary - an odd thing for a library whose pitch is a
  single static binary.
- **A missing lifetime.** The binding owned the COM lifetime, so an asset
  response and its stream could only be released when the message loop exited.
  Memory grew monotonically with every request served, and it stood as a "known
  limitation" that was never about WebView2 at all.

Separately, the dependency's MIT attribution obligation lasts exactly as long as
the dependency does. Counting the escape hatches settles it: one fork and one
raw-COM reach-around means the abstraction is already gone.

## Decision

`internal/webview2` is this repository's own COM binding. The runtime is
discovered from the Edge Update registry entry, its own client DLL
(`EmbeddedBrowserWebView.dll`) is loaded with `LOAD_WITH_ALTERED_SEARCH_PATH`,
and its `CreateWebViewEnvironmentWithOptionsInternal` export is called directly.
Every interface, IID and event handler is derived from Microsoft's
MIDL-generated `WebView2.h`. `go.mod` requires `golang.org/x/sys` and nothing
else.

## Alternatives rejected

**Stay on the binding and patch the gaps with raw COM.** The fastest route, and
each patch was individually defensible. But three escape hatches into one
dependency is an answer, not a workaround - and the stream-lifetime leak is not
fixable from outside an object whose refcounts you cannot see.

**Fork the binding.** Keeps a familiar API, and only the delta has to be
maintained. It does not help: upstream's settings vtable ends at `Settings6` on
main, so the fork inherits the whole COM problem *plus* a divergence to rebase
forever, and neither the loader DLL nor the lifetime ownership moves.

**Ship the SDK's `WebView2Loader.dll`.** It is the supported path, and it is the
thing that enforces the compatibility floor. But it breaks the single-binary
claim, and the Evergreen runtime does not itself ship that DLL - it is an SDK
artefact, so shipping it is our redistribution problem, not the system's. The
stories behind all three are in
[lessons-and-dead-ends.md](../lessons-and-dead-ends.md) sections 8 and 13.

## Consequences

**We own the vtable ABI, and the compiler cannot see it.** A wrong slot offset is
not a bad HRESULT; it is a live call through a different function pointer, which
crashes or corrupts memory inside the browser process at first use. So every slot
of every interface is pinned with `unsafe.Offsetof` in a test, every IID is
re-parsed from its canonical text, and the settings chain's 39-slot budget is
asserted against the sum of its links. That test is not optional and never will
be. Any WebView2 API this library wants must be hand-bound, in declaration order,
from the SDK header - never from the reference pages, which list members
alphabetically. And the environment is created through an entry point Microsoft
documents as subject to change.

## What would change our mind

- **Microsoft removes or changes `CreateWebViewEnvironmentWithOptionsInternal`.**
  The failure is a clean error at startup rather than a crash, and
  `TestRuntimeExportsTheEntryPointWeCallDirectly` is the test that says so.
- **A Go binding - official or otherwise - covers the full settings chain *and*
  exposes COM lifetime control (release once the runtime holds its own
  references) *and* runs without a loader DLL.** All three, not two. Then this
  layer is cost with no purchase, and this record should be superseded.

## Evidence

- `go.mod`: a single `require`. `NOTICE`: no binding is bundled or derived from.
- `internal/webview2/loader_windows_test.go`:
  `TestRuntimeExportsTheEntryPointWeCallDirectly` loads the runtime's client DLL
  on a real machine and asserts the export exists.
- `internal/webview2/interfaces_windows_test.go`: every slot of every vtable
  pinned; `TestSettings9VtblLayout` anchors the 39-slot chain.
- Live, at the time the layer was written: a bare Win32 window created an
  environment and a controller through this path, navigated to `about:blank`, and
  the destination was **read back** from the browser through `get_Source` - a
  returned `S_OK` proves that a call was accepted, not that the browser went
  anywhere.

  The smoke program was not kept: it opens a window, and no test in this
  repository may do that (see [0006](./0006-tests-stay-headless.md)). So this line
  is an observation, not something a reader can re-run. What *is* re-runnable is
  the rest of this list, plus the live acceptance checklist in
  [verification.md](../verification.md), which exercises the same path through
  `examples/basic`.
