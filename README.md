# mullion

A Win32 window host for WebView2, in pure Go. No CGo. No local port.

`mullion` creates the window, embeds WebView2 in it, and hands the frame to your
frontend: your HTML draws the title bar, and the window procedure still does the
things a native caption does — drag, double-click to maximise, resize edges,
snap, the system menu, per-monitor DPI. Assets are served to the WebView straight
out of an `fs.FS` over an in-process virtual host, so nothing listens on a socket
and there is no HTTP server to firewall.

![The examples/basic demo: a frameless window with an HTML title bar, working caption buttons, and a bridge round-trip to Go](docs/images/demo.png)

```go
//go:embed all:frontend
var embedded embed.FS

func main() {
	assets, _ := fs.Sub(embedded, "frontend")

	host := host.New(host.Config{
		Assets: assets,
		Title:  "Demo",
		Width:  980,
		Height: 640,
	})

	if err := host.Run(); err != nil {
		log.Fatal(err)
	}
}
```

That is a complete application. `Config.Bridge` is optional: window controls are
answered by the host itself, so the title bar works before you have written a
single Go method.

```
go get github.com/Burakuslendera/mullion/host
```

Requires Windows and the [WebView2 Runtime][runtime] (shipped with Windows 11 and
current Windows 10). Non-Windows builds compile — `Run` returns
`ErrUnsupportedPlatform` — so a cross-platform program does not need build tags to
depend on this package.

[runtime]: https://developer.microsoft.com/microsoft-edge/webview2/

## Why

Embedding WebView2 is easy. Embedding it in a window whose title bar you drew
yourself, without losing what the shell gives a real window, is not. The failure
modes are quiet: the window compiles, opens, renders — and the drag band is four
pixels off, or the maximise animation is gone, or the content collapses to a
sliver after a style change, or everything is subtly blurry on a 150% monitor.

This package is the result of chasing each of those to its root cause. The code
is one half of it; [`docs/`](docs/) is the other.

## What it does

- **The frame is yours.** Custom title bar, caption buttons, eight resize zones,
  system menu, snap. `WM_NCCALCSIZE`, `WM_NCHITTEST`, `WM_GETMINMAXINFO` and
  `WM_DPICHANGED` are handled for you; you write the CSS.
- Where the runtime supports it (WebView2 131+), the non-client region is real:
  CSS `app-region: drag` becomes an actual `HTCAPTION` and the shell handles the
  dragging. Older runtimes fall back to an injected JavaScript drag path
  automatically.
- **No port.** Assets come from an `fs.FS`, served over `WebResourceRequested`
  and an `IStream`; scheme, host and path traversal are all rejected at the
  boundary. Or run your own dev server instead: point `Config.URL` at a loopback
  origin you serve yourself — mullion still opens no socket, and a failed load
  lands on a fallback you control rather than the browser's error page.
- The bridge is `window.mullion.invoke("Method", ...args)`, which returns a
  `Promise`. Window controls are reserved and never reach your code.
- A render watchdog fires if the frontend never paints, and reports what it saw:
  whether the document arrived, whether the stylesheets and scripts arrived, and
  what the last bridge call was.
- **Pure Go.** The runtime is located, loaded and driven from `internal/webview2`
  — the COM interfaces, the event handlers and the environment bootstrap are all
  written here, against Microsoft's published interface definitions. There is no C
  toolchain, no bundled loader DLL, and no third-party browser binding to keep in
  step with. The only dependency in `go.mod` is `golang.org/x/sys`.

## The frame contract

Three `Config` values must match your CSS. The native hit-test is computed from
them; the visible title bar is drawn from the CSS. If they disagree, the two
drift apart and part of your title bar stops dragging.

| `Config`               | Default | CSS                                     |
| ---------------------- | ------- | --------------------------------------- |
| `TitlebarHeight`       | 36      | height of your title bar element        |
| `CaptionControlsWidth` | 138     | total width of the caption button group |
| `ResizeBorder`         | 8       | nothing — handled natively              |

See [`examples/basic`](examples/basic) for a working frontend: a title bar with
`app-region: drag`, three caption buttons that opt out with `app-region: no-drag`,
and a bridge round-trip printed into the page.

## Documentation

| Document                                                             | What it covers                                                                   |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| [architecture.md](docs/architecture.md)                               | Bootstrap order, threading model, message routing, the bridge                     |
| [webview2-and-assets.md](docs/webview2-and-assets.md)                 | The in-house WebView2 COM binding, and asset serving without a port               |
| [frame-and-dpi.md](docs/frame-and-dpi.md)                             | `WM_NCCALCSIZE`, hit-testing, per-monitor DPI, restore flicker                    |
| [snap-and-nonclient-region.md](docs/snap-and-nonclient-region.md)     | What WebView2 can and cannot do with Windows 11 snap, and the COM binding for it  |
| [snap-sources.md](docs/snap-sources.md)                               | The 40 primary and secondary sources those findings rest on                       |
| [lessons-and-dead-ends.md](docs/lessons-and-dead-ends.md)             | Approaches that were tried and abandoned, and why                                 |
| [verification.md](docs/verification.md)                               | How to check a change actually works — "it compiles" is not acceptance            |

## Configuration

Everything below has a working default; `Config{Assets: assets}` is complete.

```go
type Config struct {
	Assets fs.FS            // required unless URL is set: must contain index.html
	URL    string          // opt-in loopback origin you serve yourself; no socket

	Title       string      // "Mullion"
	ClassName   string      // "MullionWindow"
	VirtualHost string      // "mullion.local" -> https://mullion.local
	JSNamespace string      // "mullion"       -> window.mullion, data-mullion-*

	Width, Height int32     // 1024 x 768
	StartHidden   bool

	TitlebarHeight       int32 // 36  } must match your CSS
	CaptionControlsWidth int32 // 138 }
	ResizeBorder         int32 // 8

	HitTestTitlebarHeight       int32 // escape hatch: native band != CSS band
	HitTestCaptionControlsWidth int32

	DragSelector     string  // "[data-mullion-drag]" (fallback drag path)
	BackgroundColour Colour  // painted before the first frame

	ShowTimeout   time.Duration // 7s;  wait for shellReady() before showing
	RenderTimeout time.Duration // 16s; watchdog if the frontend never paints

	UserDataFolder   string // WebView2 profile dir; default under %LocalAppData%
	BrowserArguments string // extra Chromium command-line flags
	DevTools         bool   // keep F12 / context menu / accelerators

	Logger Logger                 // default: discard
	Bridge func(string) string    // optional: your methods only
	OnReady func()
	OnClose func() bool           // return true to cancel the close
}
```

`URL` is the one opt-in worth reading twice. Set it and the WebView loads a loopback
origin you serve yourself — a dev server with hot reload, say — instead of `Assets`
over the in-process virtual host. mullion still opens no socket: you run the server,
it only navigates there, and `Assets` becomes optional. It must be loopback
(`127.0.0.1`, `localhost` or `::1`) over `http`/`https`, because `window.<ns>` — the
window controls and your `Config.Bridge` — is injected into whatever it loads, so a
remote origin could otherwise call into your Go. If that load fails, mullion shows its
own controllable fallback surface rather than the browser's error page. Full
reasoning: [decisions/0012](docs/decisions/0012-config-url-loopback.md).

`Logger` takes pre-sanitised single strings — file system paths are reduced to
their base name before they reach you, so messages can be forwarded verbatim
without leaking user paths. `SlogLogger(*slog.Logger)` is provided.

## Frontend API

`window.mullion` is injected before your scripts run. There is nothing to import
and no generated binding file to keep in sync.

```js
await mullion.invoke("Method", ...args);   // -> your Config.Bridge

mullion.window.minimise();
mullion.window.toggleMaximise();
mullion.window.close();
await mullion.window.isMaximised();
mullion.window.startDrag();                // only needed for a custom drag path
mullion.window.startResize("top-left");
mullion.window.show();                     // pairs with Config.StartHidden
mullion.window.hide();

mullion.shellReady();   // releases the startup gate; the window appears
mullion.ready();        // stops the render watchdog; call after the first paint

mullion.tabTitlebar;    // true when the native non-client region path is active
```

## Diagnostics

```
go run github.com/Burakuslendera/mullion/cmd/mullion@latest doctor
```

That is the whole command — nothing is installed, and `go run` fetches, builds
and runs it in one step. To keep it, install it instead:

```
go install github.com/Burakuslendera/mullion/cmd/mullion@latest
mullion doctor
```

`go install` puts the binary in `$(go env GOPATH)/bin`, and the bare
`mullion doctor` works only when that directory is on your `PATH`. On a stock
Windows install it already is: the Go installer adds the default
`%USERPROFILE%\go\bin` for you. What breaks it is a relocated `GOPATH` — the
binaries move with it, and the installer's `PATH` entry does not.

No checkout, no PowerShell. It prints the environment a window bug report needs
— Windows build, GPUs, every monitor with its **physical** resolution and
scaling — and then the question a registry lookup cannot answer: **which
WebView2 runtime this machine would actually load, and whether it still exports
the entry point mullion calls.** It starts no browser and opens no window. Exit
code `0` means mullion can start here; `1` means it cannot, and the block says
why.

Monitors are measured with per-monitor DPI awareness declared first. Windows
reports a *virtualised* resolution to a process that has not asked, so a
hand-written "1536x864" for a 1920x1080 monitor at 125% is the one number a DPI
report must not contain — which is why this is a command and not a checklist.

One more helper lives beside it: `mullion backdrop` covers every monitor with a
flat grey while you screenshot a window, so the margin around it — the shadow
and the corners are worth keeping in frame — carries nothing of your desktop.
Your mullion window is lifted in front of the grey as it opens; capture with
whatever tool you like, press Esc on the backdrop when done
([decisions/0013](docs/decisions/0013-backdrop-is-a-mullion-command.md)). From
a checkout, `scripts/screenshot.ps1` automates the whole capture; the image
above was taken that way, on the same flat ground.

## Known limitations

- **WebView2 does not render while the window is hidden.** With `StartHidden`, the
  frontend cannot signal readiness until the first `Show`. "Load it invisibly and
  reveal it when ready" is not achievable this way.
- **The environment is created through the runtime's own client DLL**, not
  through the SDK loader (which the Evergreen runtime does not even ship).
  Microsoft documents that entry point as subject to change. If it ever does, the
  failure is a clean error at startup, not a crash — a test asserts the export
  still exists, and `mullion doctor` answers the same question on any machine, in
  one command, without a checkout.
- **Windows only**, by construction.

## Status

Early. The API is small and the behaviour is covered by tests, but it has been
exercised by one application and one example. Issues and pull requests welcome —
please read [CONTRIBUTING.md](CONTRIBUTING.md) first, in particular the rule that
the test suite must keep running headless.

## Licence

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

The only dependency is `golang.org/x/sys` (BSD 3-Clause). Nothing else is
vendored, embedded or redistributed — including the WebView2 Runtime, which is a
system component that mullion locates and calls into.
