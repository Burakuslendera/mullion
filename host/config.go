package host

import (
	"errors"
	"io/fs"
	"strings"
	"time"
)

// ErrUnsupportedPlatform is returned by Run on every platform except Windows.
var ErrUnsupportedPlatform = errors.New("mullion: unsupported platform (windows only)")

// ErrUnsupportedArchitecture is returned by Run from a Windows binary whose
// process architecture cannot safely host WebView2. Use errors.Is; the returned
// error also names runtime.GOARCH and the supported windows/amd64 target.
var ErrUnsupportedArchitecture = errors.New("mullion: unsupported Windows architecture")

// Colour is an 8-bit-per-channel RGBA colour.
type Colour struct{ R, G, B, A uint8 }

// Config describes a window host. The zero value is not useful on its own:
// Assets must be set (unless URL points the WebView at a caller-served origin).
// Every other field has a documented default that New applies, so
// Config{Assets: assets} is a complete configuration.
type Config struct {
	// Assets is the file system served to the WebView. It must contain
	// index.html at its root, unless URL is set. Assets are served from an
	// in-process virtual host; the library never opens a network port.
	//
	// The request path is chosen by the page, so every name the fs.FS will
	// answer is reachable. mullion validates the name - no traversal, no
	// backslash or colon, no segment ending in a dot or a space, and fs.ValidPath
	// last - but a name is all it validates. Two things follow, and they matter
	// only for an fs.FS backed by the real filesystem. An embed.FS is immune to
	// both: nothing in it reaches the OS.
	//
	// Serving from a directory, use os.OpenRoot rather than os.DirFS:
	//
	//	root, err := os.OpenRoot(dir)  // then Config{Assets: root.FS()}
	//
	// A reparse point inside the directory leaves it, and no name check can see
	// that: the name is ordinary and the redirection lives in the filesystem, so
	// mullion cannot refuse it for you. Measured - a directory junction made with
	// mklink /J inside the asset directory, then
	// os.DirFS(dir).ReadFile("junction/secret.txt"), returned a file from outside
	// it, while os.OpenRoot(dir).FS() answered "path escapes from parent" for the
	// same name. That difference is why this module's Go floor is 1.24 (issue
	// #103, decisions/0033) and it is pinned by
	// TestAssetRootRefusesAReparsePointAndOSDirFSDoesNot.
	//
	// Know the shape of what os.Root does, because it is narrower and blunter
	// than "it keeps you inside the directory", and both halves were measured:
	//
	//   - Narrower. It refuses reparse points whose tag is a name surrogate,
	//     which is junctions and symlinks. A hard link is not one. A hard link
	//     inside the directory to a file outside it reads normally through
	//     os.Root, exactly as it does through os.DirFS, and mklink /H needs no
	//     elevation. If arbitrary code can write into the asset directory, an
	//     os.Root does not make that safe.
	//   - Blunter. It refuses those tags wherever they point, including at a
	//     target inside the directory. A junction placed as a convenience by a
	//     build step - dir/alias -> dir/real - serves under os.DirFS and answers
	//     500 under os.Root. That is the price of the guarantee, not a bug.
	//
	// os.DirFS remains safe against every *name* mullion accepts; what it does
	// not cover is the filesystem redirection above.
	//
	// Windows device names are passed through, not filtered. os.DirFS refuses them
	// itself - measured on go1.22.12, go1.23.12, go1.24.6 and go1.26.5 - so an
	// os.DirFS caller is unaffected, and an embed.FS never reaches the OS. An
	// fs.FS written by hand over os.Open is the exposed case, and it is worth
	// knowing what it costs. Measured through such an fs.FS, ReadFile("nul")
	// returns zero bytes and a nil error, so /nul answers 200 with an empty body.
	// /con is worse and depends on how the application was linked: built for the
	// console subsystem, which plain `go build` produces, os.Open("CON") succeeds
	// and the first Read had not returned after three seconds - on the UI thread,
	// with the request path chosen by the page. Built for the GUI subsystem
	// (-ldflags "-H=windowsgui") the open fails outright and the request answers
	// 500. If you write your own fs.FS over os.Open, filter the device names in
	// it. decisions/0031 records why that filter is not here.
	Assets fs.FS

	// URL, when set, points the WebView at an origin the caller serves itself,
	// instead of serving Assets over the in-process virtual host. It must name a
	// loopback host - the local machine only - over http or https; any other URL
	// is rejected by Run (loopback.go and docs/decisions/0012 have the exact set).
	// mullion never opens a socket: the caller runs the server, mullion only
	// navigates there.
	//
	// Empty (the default) keeps the no-port guarantee and serves Assets as usual.
	// A non-empty URL is an opt-in for a caller who wants a real local HTTP origin
	// - a dev server with hot reload, or a runtime that already speaks HTTP. When
	// URL is set, Assets is optional and is not served, but window.<ns> (the bridge
	// and Config.Bridge) is still injected - which is why URL is pinned to loopback:
	// a remote origin could otherwise call into your Go. See docs/decisions/0012.
	URL string

	// PinNavigationToOrigin, when set, cancels any top-level navigation away from
	// the trusted origin (the virtual host, or the Config.URL origin) and opens the
	// foreign target in the system browser instead. It contains a frontend steered
	// off-origin - an external link with no target, an open-redirect, a server
	// redirect - so the injected bridge is never carried onto a foreign origin and
	// the custom frame is never replaced by a chromeless foreign page.
	//
	// Off by default: the message-dispatch origin gate (docs/decisions/0014)
	// already stops a foreign origin from acting through the bridge, so this is
	// defense-in-depth. Turn it on only if your frontend never navigates its top
	// frame off-origin on purpose - an app that runs an OAuth flow in the top frame
	// would have it cancelled. window.open / target=_blank route to the system
	// browser regardless of this setting (docs/decisions/0022 and 0023).
	PinNavigationToOrigin bool

	// Title is the window title. Default "Mullion".
	Title string
	// ClassName is the Win32 window class name. It must be unique per process.
	// Default "MullionWindow".
	ClassName string
	// VirtualHost is the synthetic host that serves Assets. It is the single
	// source for both the request filter and the origin allow-list.
	// Default "mullion.localhost", so the origin is https://mullion.localhost and
	// the request filter is registered for that origin exactly.
	// New ASCII-lowercases valid host names and rejects URL/authority syntax,
	// malformed labels, Unicode U-labels, escapes and IPv6 zones before native
	// startup. Underscores and canonical IP literals remain accepted for
	// compatibility. This field is ignored when URL selects an external source.
	//
	// Nothing mullion does resolves this name - WebResourceRequested intercepts
	// the request and Assets answers it in process - so it need not exist
	// anywhere. The runtime resolves it anyway, and the name decides what that
	// costs. The previous default, mullion.local, cost about two seconds on every
	// navigation: a NetLog capture on WebView2 150.0.4078.83 shows a
	// HOST_RESOLVER_MANAGER_JOB for "mullion.local:443" running 2.007 s, spanning
	// the gap between the document being served and the first subresource being
	// requested. Seven runs measured 2.012 to 2.041 s.
	//
	// The rule is not "pick something that will not resolve" - that is the
	// version that fails. mullion.test was measured and cost the same 2.027 s,
	// because .test (RFC 2606) only means nobody may register it, which says
	// nothing about what a resolver does when asked for one. The TLD RFC 6761
	// pins to the loopback address is different in kind: the RFC requires
	// resolvers to answer it without going to the network. The default moved
	// under it and measured 47-141 ms over five runs, with LaunchToWindowVisibleMs
	// falling from ~2500 to 448-630. decisions/0030 carries that change, including
	// how the no-port guard (decisions/0002, TestNoNetworkListener) was taught to
	// tell this name from an address.
	//
	// A caller that overrides this field takes the cost back on itself: a name
	// outside the TLD that RFC reserves is resolved like any other and the wait
	// returns, quietly. Nothing here checks for that.
	//
	// The measurement, its captures and the upstream report:
	//
	//	https://github.com/Burakuslendera/mullion/issues/85  the wait itself
	//	https://github.com/Burakuslendera/mullion/issues/77  the aborts it caused
	//	https://github.com/MicrosoftEdge/WebView2Feedback/issues/2381  upstream
	VirtualHost string
	// JSNamespace names the JavaScript global the host injects (window.<ns>) and
	// prefixes the DOM attributes it relies on (data-<ns>-resize-edge). It must
	// match ^[a-z][a-z0-9]*$, because it is also used as a camelCase dataset key;
	// an invalid value falls back to the default. Default "mullion".
	JSNamespace string

	// Width and Height are the initial client size in logical pixels. The
	// window opens centered in the primary monitor's work area, scaled to that
	// monitor's DPI (docs/decisions/0018). Defaults 1024 x 768.
	Width  int32
	Height int32
	// StartHidden creates the window without showing it and defers the WebView2
	// embed until the first Show. Note that WebView2 does not render while the
	// window is hidden, so the frontend cannot signal readiness until Show.
	StartHidden bool

	// TitlebarHeight is the height of the custom title bar in logical pixels.
	// The frontend's CSS title bar must be exactly this tall: the value drives
	// both the injected resize overlay and the native WM_NCHITTEST caption band.
	// Default 36.
	TitlebarHeight int32
	// CaptionControlsWidth is the width of the caption button cluster on the
	// right of the title bar, in logical pixels. The native hit test reports
	// this region as client area so the buttons stay clickable. Default 138.
	CaptionControlsWidth int32
	// ResizeBorder is the width of the resize band along the window edges, in
	// logical pixels. It is scaled by the window's DPI at hit-test time.
	// Default 8.
	ResizeBorder int32

	// HitTestTitlebarHeight and HitTestCaptionControlsWidth override the native
	// hit-test geometry when it must diverge from the CSS geometry above - for
	// example when a CSS transform scales the title bar. Zero means "same as the
	// CSS value". Most applications leave these unset.
	HitTestTitlebarHeight       int32
	HitTestCaptionControlsWidth int32

	// DragSelector is the CSS selector for the fallback drag region, used when
	// the WebView2 runtime is too old for non-client region support.
	// Default "[data-<JSNamespace>-drag]".
	DragSelector string

	// BackgroundColour is painted behind the WebView before the first frame and
	// during resize. Set it to the frontend's background to avoid a flash.
	// Default opaque white.
	BackgroundColour Colour

	// ShowTimeout bounds how long the host waits for the frontend to call
	// window.<ns>.shellReady() before showing the window anyway. A negative
	// value shows the window immediately. Default 7s.
	ShowTimeout time.Duration
	// RenderTimeout bounds how long the host waits for the frontend to call
	// window.<ns>.ready() before logging a render-watchdog error with the
	// collected diagnostics. A negative value disables the watchdog.
	// Default 16s.
	RenderTimeout time.Duration

	// UserDataFolder is where WebView2 keeps its profile: cache, local storage,
	// cookies. Empty means a folder under the user's local application data,
	// named after the executable.
	//
	// Leaving this to WebView2 itself is a trap worth knowing about: with no
	// folder given, the runtime writes next to the executable, which fails
	// outright for anything installed under Program Files. The default here
	// avoids that.
	UserDataFolder string

	// BrowserArguments is appended to the Chromium command line. It is the main
	// tuning surface the runtime exposes; most applications leave it empty.
	BrowserArguments string

	// DevTools keeps the developer surface enabled: DevTools (F12), the default
	// context menu and the browser accelerator keys. It is off by default,
	// because a shipped frameless window that reloads on Ctrl+R resets its
	// frontend while the native frame keeps running.
	DevTools bool

	// Logger receives diagnostic output. Default NopLogger.
	//
	// It must be safe to call from more than one goroutine. Most lines are
	// written from the UI thread, but not all, and never have been: the render
	// watchdog and the startup show gate write from timers, and a system-browser
	// launch writes from the worker it runs on (issue #74, decisions/0029). A
	// Logger that holds state - a buffer, a file handle, a counter - needs its
	// own lock; ColourLogger has one.
	Logger Logger

	// Bridge handles application-defined calls from the frontend. It receives
	// the raw JSON request ({"id":..,"method":..,"args":[..]}) and returns the
	// raw JSON response, or "" to stay silent. Window control methods never
	// reach Bridge - the host answers those itself - so Bridge may be nil.
	Bridge func(string) string

	// OnReady is called once the window exists and the message loop is about to
	// start.
	OnReady func()
	// OnClose is called when the user closes the window. Returning true cancels
	// the close.
	OnClose func() bool
}

const (
	defaultTitle                = "Mullion"
	defaultClassName            = "MullionWindow"
	defaultVirtualHost          = "mullion.localhost"
	defaultJSNamespace          = "mullion"
	defaultWidth                = 1024
	defaultHeight               = 768
	defaultTitlebarHeight       = 36
	defaultCaptionControlsWidth = 138
	defaultResizeBorder         = 8
	defaultShowTimeout          = 7 * time.Second
	defaultRenderTimeout        = 16 * time.Second
)

// normalise fills in defaults. It is pure, platform independent and total: any
// Config, including the zero value, maps to a usable one (except for Assets,
// which Run reports on because a nil file system is a programming error the
// library cannot paper over).
func (config Config) normalise() Config {
	if config.Title == "" {
		config.Title = defaultTitle
	}
	if config.ClassName == "" {
		config.ClassName = defaultClassName
	}
	if config.VirtualHost == "" {
		config.VirtualHost = defaultVirtualHost
	}
	if !validJSNamespace(config.JSNamespace) {
		config.JSNamespace = defaultJSNamespace
	}
	if config.Width <= 0 {
		config.Width = defaultWidth
	}
	if config.Height <= 0 {
		config.Height = defaultHeight
	}
	if config.TitlebarHeight <= 0 {
		config.TitlebarHeight = defaultTitlebarHeight
	}
	if config.CaptionControlsWidth <= 0 {
		config.CaptionControlsWidth = defaultCaptionControlsWidth
	}
	if config.ResizeBorder <= 0 {
		config.ResizeBorder = defaultResizeBorder
	}
	if config.HitTestTitlebarHeight <= 0 {
		config.HitTestTitlebarHeight = config.TitlebarHeight
	}
	if config.HitTestCaptionControlsWidth <= 0 {
		config.HitTestCaptionControlsWidth = config.CaptionControlsWidth
	}
	if config.DragSelector == "" {
		config.DragSelector = "[data-" + config.JSNamespace + "-drag]"
	}
	if config.BackgroundColour == (Colour{}) {
		config.BackgroundColour = Colour{R: 255, G: 255, B: 255, A: 255}
	}
	if config.ShowTimeout == 0 {
		config.ShowTimeout = defaultShowTimeout
	}
	if config.RenderTimeout == 0 {
		config.RenderTimeout = defaultRenderTimeout
	}
	if config.Logger == nil {
		config.Logger = NopLogger{}
	}
	return config
}

// hitTestMetrics is the frame geometry the window procedure hit-tests against,
// in logical pixels. The values are scaled by the window's DPI at hit-test time,
// never by CSS: a CSS scale would move the visible title bar without moving the
// band the shell drags by, and the two would drift apart on any non-100% monitor.
type hitTestMetrics struct {
	ResizeBorder   int32
	TitlebarHeight int32
	ControlsWidth  int32
}

func (config Config) hitTestMetrics() hitTestMetrics {
	return hitTestMetrics{
		ResizeBorder:   config.ResizeBorder,
		TitlebarHeight: config.HitTestTitlebarHeight,
		ControlsWidth:  config.HitTestCaptionControlsWidth,
	}
}

// validJSNamespace enforces ^[a-z][a-z0-9]*$. The constraint is not cosmetic:
// the namespace becomes both a DOM attribute segment (data-<ns>-resize-edge) and
// the camelCase dataset key that reads it back (dataset.<ns>ResizeEdge). A dash
// or an upper-case letter would break that mapping silently.
func validJSNamespace(namespace string) bool {
	if namespace == "" {
		return false
	}
	for index := 0; index < len(namespace); index++ {
		char := namespace[index]
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9' && index > 0:
		default:
			return false
		}
	}
	return true
}

// datasetKey maps the namespace to the camelCase dataset property that the
// injected resize overlay reads (data-mullion-resize-edge -> mullionResizeEdge).
func (config Config) datasetKey(suffix string) string {
	return config.JSNamespace + strings.ToUpper(suffix[:1]) + suffix[1:]
}
