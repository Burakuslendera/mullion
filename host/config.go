package host

import (
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"time"
)

// ErrUnsupportedPlatform is returned by Run on every platform except Windows.
var ErrUnsupportedPlatform = errors.New("mullion: unsupported platform (windows only)")

// Logger receives the host's diagnostic output. Every message is already
// sanitised: file system paths are reduced to their base name before they reach
// the logger, so a Logger implementation may forward messages verbatim without
// leaking user paths.
//
// Messages are pre-formatted single strings on purpose. Some of them are emitted
// from hot paths (WM_SIZE, WM_MOVE), and a variadic signature would push
// formatting work into those paths for no benefit.
type Logger interface {
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// NopLogger discards every message. It is the default when Config.Logger is nil.
type NopLogger struct{}

func (NopLogger) Debug(string) {}
func (NopLogger) Info(string)  {}
func (NopLogger) Warn(string)  {}
func (NopLogger) Error(string) {}

type slogLogger struct{ logger *slog.Logger }

func (l slogLogger) Debug(msg string) { l.logger.Debug(msg) }
func (l slogLogger) Info(msg string)  { l.logger.Info(msg) }
func (l slogLogger) Warn(msg string)  { l.logger.Warn(msg) }
func (l slogLogger) Error(msg string) { l.logger.Error(msg) }

// SlogLogger adapts a *slog.Logger to the Logger interface.
func SlogLogger(logger *slog.Logger) Logger {
	if logger == nil {
		return NopLogger{}
	}
	return slogLogger{logger: logger}
}

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

	// Title is the window title. Default "Mullion".
	Title string
	// ClassName is the Win32 window class name. It must be unique per process.
	// Default "MullionWindow".
	ClassName string
	// VirtualHost is the synthetic host that serves Assets. It is the single
	// source for both the request filter and the origin allow-list.
	// Default "mullion.local", which yields the origin https://mullion.local.
	VirtualHost string
	// JSNamespace names the JavaScript global the host injects (window.<ns>) and
	// prefixes the DOM attributes it relies on (data-<ns>-resize-edge). It must
	// match ^[a-z][a-z0-9]*$, because it is also used as a camelCase dataset key;
	// an invalid value falls back to the default. Default "mullion".
	JSNamespace string

	// Width and Height are the initial client size in logical pixels.
	// Defaults 1024 x 768.
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
	defaultVirtualHost          = "mullion.local"
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
	if config.CaptionControlsWidth < 0 {
		config.CaptionControlsWidth = 0
	}
	if config.CaptionControlsWidth == 0 {
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

// origin is the scheme+host the WebView loads and the only origin the asset
// provider serves. Deriving it here keeps the request filter and the allow-list
// from drifting apart.
func (config Config) origin() string {
	return "https://" + config.VirtualHost
}

func (config Config) startURL() string {
	if config.URL != "" {
		return config.URL
	}
	return config.origin() + "/index.html"
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
