# 0004. The host answers the window control methods; `Config.Bridge` is optional

**Status:** Accepted

## Context

In the application this library was extracted from, the message callback handed
**every** incoming frontend message to the application - `WindowStartDrag`,
`WindowMinimise` and the rest included. The window-control protocol was therefore
implemented by the application, not by the window host.

The consequence is easy to miss until you are the second consumer: a library that
draws no title bar and answers no window messages hands every user the same
homework. Worse, a host built that way has a failure mode with no error in it - a
frontend with a custom title bar and no bridge wired up produces a window that
looks finished and cannot be moved.

## Decision

The `Host` answers a reserved set of bridge methods itself:

```
WindowStartDrag  WindowStartResize  WindowMinimise  WindowToggleMaximise
WindowIsMaximised  WindowShow  WindowHide  WindowClose
WindowShellReady  WindowReady  WindowPhase  WindowDiagnostic
```

Anything else is handed to `Config.Bridge` as the original raw request string, so
the application's own wire format stays opaque to the library. `Bridge` may be
nil: the title bar works before a single Go method has been written.

## Alternatives rejected

**Forward everything to the application.** It keeps the library thin, gives the
application total control of the protocol, and reserves no names - a real
advantage, and the design the extraction started from. But it makes each consumer
re-implement drag, resize, minimise, maximise and close against `WM_NCLBUTTONDOWN`
and hit-test codes, and it makes a nil `Bridge` mean a dead title bar. The default
behaviour of a window library should be a working window.

**A separate typed API instead of the message channel.** No collisions at all,
and better types. But the frontend already has exactly one channel into Go; a
second one doubles the protocol surface and the injected shim for the sake of
twelve calls that never change.

## Consequences

**The names are reserved, permanently.** An application cannot define a method
called `WindowMinimise` - it will be answered by the host and never reach
`Config.Bridge`. That is a real constraint on every consumer, and a test exists
specifically to keep it true.

The bridge envelope (`{id, method, args}` in, `{id, ok, result}` or
`{id, ok:false, error}` out) is now part of the library's contract rather than the
application's, so changing it is a breaking change for every frontend. And an
unknown method with no `Bridge` configured answers `ok:false` rather than staying
silent, because a frontend awaiting a promise that never settles is a worse bug
than a rejection.

## What would change our mind

A real application needs a method inside the reserved namespace - most plausibly a
frontend it did not write, already calling something named `Window...`. The answer
then is not to hand the protocol back, but to namespace the reserved set the way
the injected global already is: `Config.JSNamespace` prefixes `window.<ns>` and
`data-<ns>-*`, and the reserved method names would follow it. If that day comes,
this record is superseded by one that describes the prefixed namespace.

## Evidence

- `bridge_windows.go`: `handleReservedMethod` routes the reserved set before
  `Config.Bridge` is consulted; a nil `Bridge` is a supported configuration, not a
  panic.
- `js.go`: the twelve reserved method names, defined in one place.
- `bridge_windows_test.go`:
  - `TestBridgeHandlesWindowControlsWithoutAConfiguredBridge` - with `Bridge`
    nil, drag, minimise, toggle-maximise, hide, phase and diagnostic all answer
    `ok:true`, and `WindowIsMaximised` returns a result.
  - `TestBridgeReservedMethodsNeverReachTheApplication` - a configured `Bridge`
    never sees them.
  - `TestBridgeForwardsUnknownMethodsVerbatim` - an application method arrives as
    the original string, not a re-encoded one.
