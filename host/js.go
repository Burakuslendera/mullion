package host

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

// Reserved bridge methods. The host answers these itself and never forwards
// them to Config.Bridge, so a frontend gets a working title bar without the
// application having to re-implement the window protocol.
const (
	methodStartDrag      = "WindowStartDrag"
	methodStartResize    = "WindowStartResize"
	methodMinimise       = "WindowMinimise"
	methodToggleMaximise = "WindowToggleMaximise"
	methodIsMaximised    = "WindowIsMaximised"
	methodShow           = "WindowShow"
	methodHide           = "WindowHide"
	methodClose          = "WindowClose"
	methodShellReady     = "WindowShellReady"
	methodReady          = "WindowReady"
	methodPhase          = "WindowPhase"
	methodDiagnostic     = "WindowDiagnostic"
)

// jsScripts holds the scripts injected into every document, rendered once for a
// given Config. They are injected in this order: bridge first (it installs the
// namespace the others use), then diagnostics, drag and resize.
type jsScripts struct {
	bridge         string
	diagnostics    string
	drag           string
	resize         string
	navigationEval string
	tabFlag        string
}

// jsScripts renders the injected JavaScript for this configuration. It is pure
// string work, deliberately kept off the Windows build tag so the rendering can
// be tested on any platform.
func (config Config) jsScripts() jsScripts {
	replace := strings.NewReplacer(
		"__NS__", config.JSNamespace,
		"__DATASET__", config.datasetKey("resizeEdge"),
		"__EDGE_ATTR__", "data-"+config.JSNamespace+"-resize-edge",
		"__DRAG_SELECTOR__", jsStringLiteral(config.DragSelector),
		"__TITLEBAR_H__", strconv.Itoa(int(config.TitlebarHeight)),
		"__CONTROLS_W__", strconv.Itoa(int(config.CaptionControlsWidth)),
		"__BORDER__", strconv.Itoa(int(config.ResizeBorder)),
		"__START_HIDDEN__", strconv.FormatBool(config.StartHidden),
		"__M_DRAG__", methodStartDrag,
		"__M_RESIZE__", methodStartResize,
		"__M_MIN__", methodMinimise,
		"__M_MAXTOGGLE__", methodToggleMaximise,
		"__M_ISMAX__", methodIsMaximised,
		"__M_SHOW__", methodShow,
		"__M_HIDE__", methodHide,
		"__M_CLOSE__", methodClose,
		"__M_SHELLREADY__", methodShellReady,
		"__M_READY__", methodReady,
		"__M_PHASE__", methodPhase,
		"__M_DIAG__", methodDiagnostic,
	)
	return jsScripts{
		bridge:         replace.Replace(bridgeTemplateJS),
		diagnostics:    replace.Replace(diagnosticsTemplateJS),
		drag:           replace.Replace(dragTemplateJS),
		resize:         replace.Replace(resizeTemplateJS),
		navigationEval: replace.Replace(navigationEvalTemplateJS),
		tabFlag:        "window." + config.JSNamespace + ".tabTitlebar = true;",
	}
}

// jsStringLiteral encodes s as a JavaScript string literal, quotes included, so
// a Config value substituted into a .js template cannot break out of the string
// context it lands in. Unlike JSNamespace (validated to ^[a-z][a-z0-9]*$),
// DragSelector is free-form, so it is encoded here rather than trusted raw:
// drag.js reads target.closest(__DRAG_SELECTOR__) with no surrounding quotes.
// json.Marshal of a string is always a valid JS string literal and never errors.
func jsStringLiteral(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// The templates below live next to this file as plain JavaScript and are
// compiled in with go:embed: source in another language does not sit inline in
// a Go string literal (CONTRIBUTING.md, Code style). The .js files stay
// ASCII-only - scripts/leak-scan.ps1 holds .js source to the same ASCII rule
// as .go files - and embedding is portable, so this file keeps no build tag.

// bridgeTemplateJS installs window.<ns>. Everything the frontend needs reaches
// it through this object: there is no module to import and no generated binding
// file to keep in sync, so the frontend can be plain HTML on disk.
//
//go:embed bridge.js
var bridgeTemplateJS string

// diagnosticsTemplateJS reports what the document actually did. Without it, the
// failure mode "WebView2 embedded, navigated, and painted nothing" is invisible
// from the Go side: the render watchdog would fire with no idea whether the
// scripts loaded, the stylesheets 404'd, or a script threw.
//
//go:embed diagnostics.js
var diagnosticsTemplateJS string

//go:embed navigation_eval.js
var navigationEvalTemplateJS string

// dragTemplateJS is the FALLBACK drag path, for runtimes without non-client
// region support. When the runtime is new enough the host enables non-client
// regions and CSS "app-region: drag" produces a real HTCAPTION, which is
// strictly better: the shell handles the drag, so double-click-to-maximise and
// snap-on-drag-to-edge come for free. This path exists so an old runtime
// degrades to a draggable window instead of a stuck one.
//
//go:embed drag.js
var dragTemplateJS string

// resizeTemplateJS overlays eight transparent resize zones on the document.
//
// The zones exist because the WebView2 child window covers the client area and
// swallows the mouse before WM_NCHITTEST on the parent ever sees it. The zones
// catch the pointer in the page and hand the gesture back to the window
// procedure, which then runs a real system resize loop.
//
// The geometry mirrors Config: the top zone stops short of the caption buttons
// so the buttons stay clickable, and the right zone starts below the title bar
// for the same reason.
//
//go:embed resize.js
var resizeTemplateJS string
