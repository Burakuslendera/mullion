# WebView2 hosting

## Contents

- [Talking to WebView2 without a third-party binding](#talking-to-webview2-without-a-third-party-binding)
  - [Finding the runtime, and skipping the loader DLL](#finding-the-runtime-and-skipping-the-loader-dll)
  - [Event handlers are COM objects we implement](#event-handlers-are-com-objects-we-implement)

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

1. **Discover the runtime.** Read the Evergreen registration Edge Update publishes under
   `Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}` —
   `pv` (product version) and `location` — from `HKCU`, then `HKLM` in the 32-bit
   registry view (Edge Update is a 32-bit installer and writes under `WOW6432Node`),
   then the 64-bit view. `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER` overrides all of it and is
   treated as a **pin**: if the folder holds no usable runtime the host fails rather
   than silently falling back to a different browser build. Every candidate is checked
   against the disk before it is accepted, because the registry outlives uninstalls,
   and a relative `location` value is dropped rather than resolved against the process
   working directory, so a malformed or planted registry entry cannot steer the load
   to a CWD-relative path (issue #69).
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

One handler carries a fifth constraint of its own, and it is an ordering.
`NavigationStarting` is the other half of the containment above: it asks the host
whether to cancel, calls `put_Cancel` itself, and only when *that* call succeeds
invokes a second host callback — `NavigationCancelledCallback` — which is where
everything following from a cancel happens. A `put_Cancel` that fails warns, naming
the navigation, and tells the host nothing: the navigation is going ahead, so it has
to reach the host as an ordinary one rather than as a cancel that never took
(decisions/0027). Both getters on this event report their own failure for the same
reason — an unreadable URI arrives at a host gate as the empty string, which is no
origin's, and a failed id read arrives as `0`, which no real navigation uses.


Asset serving moved verbatim to [Asset serving without a port](./assets.md).

> Last updated: 2026-08-06 | Editor: OpenAI (GPT-5.6) | Change: documented lazy COM callback allocation after the Windows/amd64 architecture gate, then split the asset-serving continuation verbatim when this file reached its hard limit.
