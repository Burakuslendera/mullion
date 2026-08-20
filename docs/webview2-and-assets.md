# WebView2 hosting

## Contents

- [Talking to WebView2 without a third-party binding](#talking-to-webview2-without-a-third-party-binding)
  - [Finding the runtime, and skipping the loader DLL](#finding-the-runtime-and-skipping-the-loader-dll)
  - [The Go-owned ABI is explicit](#the-go-owned-abi-is-explicit)
  - [Event handlers are COM objects we implement](#event-handlers-are-com-objects-we-implement)
  - [Completion and embed lifetime](#completion-and-embed-lifetime)
  - [Reporting follows return ownership](#reporting-follows-return-ownership)

This document describes how the host talks to WebView2 without a third-party
binding. It moved verbatim out of [architecture.md](./architecture.md) — the
end-to-end map — when that file crossed the 400-line reference-doc limit.
Asset serving is documented separately in [assets.md](./assets.md).

## Talking to WebView2 without a third-party binding

`internal/webview2` is the library's own COM binding for the WebView2 Win32 API. The
only module dependency is `golang.org/x/sys` — runtime discovery, environment creation,
every interface, and every event handler is implemented here, against Microsoft's
published interface definitions. Nothing is delegated to a browser binding that has to
be kept in step with the runtime.

### Finding the runtime, and skipping the loader DLL

The SDK's usual entry point is `CreateCoreWebView2EnvironmentWithOptions`, exported by
`WebView2Loader.dll`, which an application is expected to ship beside its executable.
The library ships no such DLL. Instead:

1. **Gate the process architecture, then discover the runtime.** `findRuntime`
   rejects every target except `windows/amd64` before registry, pin, disk or DLL
   work ([decision 0034](./decisions/0034-webview2-hosting-is-windows-amd64-only.md)).
   On the supported target, read the Evergreen registration Edge Update publishes
   under
   `Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`:
   `pv` (product version) and `location`, from `HKCU`, then `HKLM` in the 32-bit
   registry view (Edge Update is a 32-bit installer and writes under
   `WOW6432Node`), then the 64-bit view.

   `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` overrides all registry candidates and is
   a **pin**, not a hint. It must be an absolute Windows path. Bare relative,
   `.\...`, drive-relative `C:...`, and rooted-but-drive-relative `\...` values
   are invalid. An invalid pin returns the existing pin diagnostic immediately:
   it cannot fall back to the registry, probe the disk, resolve the runtime DLL,
   or load another browser build. A valid absolute pin can proceed to ordinary
   candidate validation, but if it contains no usable runtime the host still
   fails rather than selecting a different build.

   Registry `location` has the same absolute-path invariant, but a malformed or
   stale registry candidate may fall through to later registry/default
   candidates. Every candidate is checked against disk before acceptance because
   registry entries outlive uninstalls. This distinction prevents either a
   planted registry value or an explicit pin from steering a load through the
   process working directory (issue #69). The headless
   `TestDiscoverCandidatesRejectsRelativePinnedFolders` locks the invalid-pin
   no-fallback/no-disk boundary; it does not load a real WebView2 DLL.

2. **Load the runtime's own COM server**, `<runtime>\EBWebView\<arch>\EmbeddedBrowserWebView.dll`,
   with `LOAD_WITH_ALTERED_SEARCH_PATH` so its siblings resolve out of the install
   folder and not out of ours (the wrong folder, and possibly a writable one).
3. **Call its export directly:**

   ```c
   HRESULT CreateWebViewEnvironmentWithOptionsInternal(
       bool                                                          checkRunningInstance,
       int                                                           runtimeType,
       PCWSTR                                                        userDataFolder,
       IUnknown                                                     *environmentOptions,
       ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler   *handler)
   ```

   This is where `WebView2Loader.dll` does its real work; the loader is a convenience
   wrapper around it. `ICoreWebView2EnvironmentOptions` is a COM object the *caller*
   implements, so it is implemented in Go like any other handler.

Bypassing the loader is not free, and two traps only exist on this path. Both were found
live, against a real runtime, and neither is documented — the official loader hides them
by never letting you make the mistake.

**`TargetCompatibleBrowserVersion` must not be null.** The runtime validates the
property and rejects a null with `E_INVALIDARG`. `WebView2Loader.dll` always supplies a
value, so an application on the official path cannot discover this. Supplying an
invented version instead is worse, not better: a plausible-looking `"1.0.0.0"` is
rejected with `ERROR_FILE_NOT_FOUND`, because the runtime maps the version onto a real
browser build and finds none. The only answer that is both truthful and cannot fail is
to report **the version of the runtime that was just discovered**.

**The version floor lives in the loader, not in the runtime.** Declaring a target of
`"999.0.0.0"` against a 150 runtime *succeeds*. The compatibility gate that would have
refused it is implemented in `WebView2Loader.dll` — bypass the loader and the gate goes
with it. The consequence is a rule, and it is the important one on this page:

> **Detect features with `QueryInterface`, never with a version comparison.** A version
> number buys no protection here. `QueryInterface` asks the object that will actually
> serve the call whether it implements the interface, and its answer is the only one
> that is true by construction. Every optional interface — `ICoreWebView2Settings9`,
> `ICoreWebView2Controller3`, and the rest — is reached this way, and a missing one is a
> recoverable condition, not an error.

### The Go-owned ABI is explicit

The binding has local, deterministic ABI manifests. Runtime-owned interface
tests pin flattened vtable slots and sizes, and canonical-string rows pin every
declared IID. For Go-owned event, completion, and environment-options objects,
the manifests pin vtable-at-offset-zero representation, full layout and
canonical IID. Local dispatch tests call runtime-facing entry points by literal
slot: event and completion `Invoke` at slot 3, plus every implemented options
getter and setter at its declared slot.

Those checks prove declarations and local dispatch only. They cannot prove that
a production consumer selects the right semantic interface, passes arguments in
native order, or transfers an owned COM reference correctly. Separate consumer
and handoff tests are required:

- `TestRegisterEventsPairsEveryNumericalConsumerWithItsSemanticHandler` maps all
  six production `add_*` slots to the expected handler IID, while
  `TestAddEventTransfersExactlyOneReferenceToTheRuntime` pins the package-to-
  runtime reference handoff on registration success and failure.
- `TestCreateEnvironmentWithOptionsNativeCallBoundary` and
  `TestEnvironmentCreateControllerNativeCallBoundary` drive the production
  loader delegates through their real callback/COM slots. They pin native
  argument order, semantic completion handlers, options-slot reads, and result
  ownership across synchronous failure, timeout with late completion, completed
  failure, and success.

The loader also has three local trip-wires that name-mirroring tests would miss.
`Environment.CreateController` dispatches through the complete
`ICoreWebView2EnvironmentVtbl`, never a private prefix that could drift from it.
Separate semantic constructors bind environment and controller completions to
their own IIDs. The numeric-call helper retains `//go:uintptrescapes`: it
forwards stack addresses through a `uintptr` wrapper, so removing that directive
can leave a callback holding the pre-growth stack address.

`TestCOMABIInventoryCompleteness` scans every production Go filename, including
architecture-specific suffixes, and inventories each vtable declaration, COM
object representation and GUID literal. It is only an alarm for unclassified
ABI: adding a manifest row does not prove correctness; the corresponding local
layout/identity/dispatch test and any affected consumer mapping or reference
handoff must also be pinned.
The authority is Microsoft.Web.WebView2 NuGet `1.0.4129.50`:
`build/native/include/WebView2.h` for flattened C vtables and root `WebView2.idl`
for UUIDs and declaration order. These checks are deterministic and need neither
a runtime nor a window
([decision 0001](./decisions/0001-own-webview2-com-layer.md)).

### Event handlers are COM objects we implement

`add_WebMessageReceived`, `add_WebResourceRequested`, `add_NavigationStarting`,
`add_NavigationCompleted`, `add_ProcessFailed` and `add_NewWindowRequested` each take a
COM object the runtime calls back into: a vtable, an IUnknown implementation and a
refcount, written in Go. Four constraints govern them, and three of the four are fatal
when violated. `NewWindowRequested` is where a single-window host takes `window.open` /
`target=_blank` over from the runtime: it suppresses the runtime's own detached new
window and routes an http/https target to the system browser (decisions/0022).

- **Build vtables once, lazily after the architecture gate.** `windows.NewCallback`
  allocates from a small, fixed table and never frees an entry. A callback allocated
  per handler *instance* exhausts the table; a vtable per interface wastes it. The
  shared vtables are therefore built by one `sync.Once`, only after amd64 validation,
  and every handler instance reuses them. Importing an unsupported Windows binary
  allocates no production callback merely to report its architecture sentinel.
- **Keep a GC root.** Once a Go object's address has been handed to COM, the Go garbage
  collector cannot see the reference: the runtime holds an integer, not a Go pointer. A
  package-level map keyed by the interface pointer is what keeps the object reachable, and
  the entry is deleted when the COM refcount reaches zero — so the map is a root, not a leak.
- **Release *after* registering, never before.** `add_*` takes its own reference on the
  handler. Dropping ours before that call is a use-after-free; never dropping it is a
  leak. The correct order is: create with refcount 1, register, then release our one
  reference and let the runtime's own reference keep the object alive.
- **No panic may escape `Invoke`.** The caller is Chromium, and a Go panic unwinding into
  a C++ stack takes the process with it. Every `Invoke` recovers, reports through a hook,
  and **returns `S_OK` regardless**. A failing HRESULT out of an event handler is not a
  no-op: for `WebResourceRequested` the runtime reads it as "the handler produced no
  response", cancels the request and blanks the asset — so one buggy Go callback would
  turn into a dead window. `S_OK` means "the event was delivered", which is true whatever
  the callback did with it.

For every value passed across an observation callback, the adapter carries the
getter's value and error independently. The host therefore distinguishes a
successfully observed zero/empty value from one produced after an HRESULT failure,
and reports policy only after its state transition. Getters that fail before a
safe callback exists — string-message decoding and resource-request acquisition —
are instead reported locally under the return-ownership rule below.
`NavigationStarting` asks the host whether to cancel, calls `put_Cancel` itself,
and invokes `NavigationCancelledCallback` only after that call succeeds. A failed
`put_Cancel` warns with the navigation and commits no cancel state. The warning
preserves getter provenance: a successful zero navigation id is `id=0`, while a
failed id getter is `id=unavailable`. URI, navigation id, initiation and redirect
getters retain separate provenance; the completed-navigation, new-window,
process-failure and message adapters do the same. See
[decisions/0027](./decisions/0027-cancel-is-committed-after-the-runtime-performs-it.md)
and [0037](./decisions/0037-event-values-preserve-getter-provenance.md).

### Completion and embed lifetime

Issue #98 closes one invariant across the asynchronous loader and `Browser.Embed`:
every COM reference that this package owns has exactly one release on every exit,
including timeout, a contract-breaking late callback, a panic in embedder code,
and a secondary cleanup failure. The tests are deliberately headless; fake COM
servers count `AddRef`/`Release` and exercise the production handoff seams
without creating an HWND, starting a message pump, or loading WebView2.

**Completion handlers.** The runtime's result argument is borrowed for the
duration of `Invoke`. The callback takes one reference before deciding where the
result goes. If the waiter is still present, that reference is transferred to
the waiter; if `abandon` has sealed the handler, or the callback fires twice, it
is released immediately. `abandon` sets its permanent flag under the same mutex
as the delivery decision and drains a result that reached the one-slot buffer
just before the flag. Therefore the two orderings
`Invoke → abandon` and `abandon → late Invoke` both release exactly once.

The loader registers `abandon` after `release`, so deferred calls run in the
required LIFO order: the handler is sealed and any buffered result is drained
before the package drops its own handler reference. The timeout and synchronous
HRESULT branches also call `abandon` immediately, closing the late-`Invoke`
window before returning; the deferred call remains the all-exits guard for
successful waits, completion-result errors and panics. This is the completion
handler extension of the timeout ownership fixed by #37, not a second owner.

**Embed failure and event registration.** `CreateEnvironment`,
`CreateController` and `GetCoreWebView2` each return an owned interface
reference. `Embed` keeps each local reference deferred until ownership is
transferred into `Browser`; before event registration succeeds, a deferred
`ShuttingDown` closes and releases the stored controller/core/environment if
embedder code panics or registration fails. `ShuttingDown` is idempotent and
retains the runtime-required close-before-release order. `addEvent` likewise
defers the constructor reference release until after `add_*` returns, so both a
registration error and a panic cannot strand it; successful registration leaves
exactly the runtime's reference.

This is distinct from reporting ownership. Under #86 and
[decision 0038](./decisions/0038-terminal-policy-owns-error-reporting.md),
`Embed` returns its primary failure unchanged and does not report it again.
Non-returnable adapter-policy failures, such as the bounds-policy `Put*` calls,
and secondary controller-close failures are reported locally. #97 owns the
outer window/browser teardown boundary; #98 prevents the in-progress embed from
orphaning these references before the host can take ownership. #99's
recurrence-lock rule requires tests at production handoff seams, but the focused
#98 tests do not directly execute `Embed` with fake COM objects; its guard wiring
therefore remains headless-unverified.

**COM out-parameters.** An options getter that cannot resolve its `this` still
nils a string pointer or writes `FALSE` before returning failure. A Win32
`BOOL` is four bytes (`int32`), not Go's one-byte `bool`; `writeBOOL` is the
single width-aware writer for that ABI. The rule prevents a caller from
freeing stale output or reading bytes left by a previous call, even though a
foreign or vacant `this` is not a normal runtime path.

The focused #98 tests prove these ownership, ordering, ABI-slot and out-
parameter contracts with fakes. They cannot prove WebView2's real callback
schedule, COM implementation, controller close behaviour, or rendering on a
live window. Those remain **unverified** until the runtime-backed Windows
verification path is exercised; a green headless suite is not that evidence.

### Reporting follows return ownership

Browser operations that return an error do not also invoke `ErrorCallback`; the
host wraps and reports that terminal failure once. The adapter reports locally
only when no error can be returned: an event cannot safely reach its callback,
an adapter-only policy operation fails, or secondary cleanup (including
controller close) cannot replace a primary returned failure. Optional-interface
misses remain warnings. This keeps one owner per terminal report without hiding
non-returnable failures
([decision 0038](./decisions/0038-terminal-policy-owns-error-reporting.md)).


Asset serving moved verbatim to [Asset serving without a port](./assets.md).

> Last updated: 2026-08-21 | Editor: OpenAI (GPT-5.6) | Change: define the absolute executable-folder pin as fail-closed before fallback, disk and DLL work, and link its architecture gate and headless test.
