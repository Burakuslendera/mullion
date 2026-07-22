package host

import (
	_ "embed"
	"html"
	"net/url"
	"strconv"
	"strings"
)

// This file builds the fallback surface shown when a navigation fails, so an end
// user is never stranded on Edge's chromeless "can't reach this page" network-error
// page (issue #3, found live while testing PR #4). It is a PURE function of Config
// and the failed URL and returns a self-contained data:text/html URL: mullion opens
// no socket and needs no server to show it, exactly as with the embedded fs.FS. It
// is kept portable (no build tag), next to loopback.go, so the builder is
// headless-testable - the live rendering is not, and is noted as such.
//
// The problem it solves: with Config.URL set, if the caller's loopback server is
// down (or not up yet at launch), WebView2 shows Edge's own error page. That page is
// not the frontend, so it draws no custom title bar and no caption buttons, and the
// frameless window (the native caption is removed in WM_NCCALCSIZE) looks broken.
// This page is mullion's own controllable surface instead.
//
// The injected bridge shim (host/js.go, registered with Init) runs on every
// document, including this data: page, so window.<ns> exists here and the caption
// buttons and the fallback drag path work exactly as on the real frontend.

// errorPageURL renders the fallback surface as a data:text/html URL for failedURL.
//
// It shows ONLY the redacted origin of failedURL - urlOrigin drops the path, query
// and fragment - so a token a caller placed in Config.URL never reaches the page.
// Retry re-navigates to that origin; for the common Config.URL (no path) the origin
// IS the original URL, and a path-bearing Config.URL retries the origin root, which
// keeps the promise that the page discloses nothing but the origin.
//
// The whole document is percent-encoded (url.PathEscape) into the data: payload, and
// every interpolated value is HTML-escaped first, so the two encodings compose: the
// browser percent-decodes the payload and the HTML parser then sees exactly the
// escaped document. Config.JSNamespace is validated to ^[a-z][a-z0-9]*$ by normalise,
// so it is safe to interpolate into the script and attribute names unescaped.
func errorPageURL(config Config, failedURL string) string {
	origin := html.EscapeString(urlOrigin(failedURL))
	replacer := strings.NewReplacer(
		"__NS__", config.JSNamespace,
		"__TITLEBAR_H__", strconv.Itoa(int(config.TitlebarHeight)),
		"__CONTROLS_W__", strconv.Itoa(int(config.CaptionControlsWidth)),
		"__BG__", cssColour(config.BackgroundColour),
		"__FG__", contrastColour(config.BackgroundColour),
		"__ORIGIN__", origin,
	)
	// errorpage.html ends with a newline, as a text file should - and an edit
	// could leave it with more than one - so trim them all: the data: payload is
	// exactly the document and nothing after it. errorpage_test.go locks this.
	document := replacer.Replace(strings.TrimRight(errorPageTemplate, "\n"))
	// An explicit charset: the document is ASCII (leak-scan holds errorpage.html to
	// the ASCII rule and every interpolated value is escaped or numeric), so this
	// fixes no concrete bug, but it states the encoding rather than leaving the
	// browser to assume one (issue #13). surfaceURIMatches tolerates the exact URL,
	// an empty URI and any data: prefix, so the added parameter does not disturb the
	// error-surface navigation-identity match (decisions/0021).
	return "data:text/html;charset=utf-8," + url.PathEscape(document)
}

// cssColour renders a Colour as CSS rgba(), with alpha in the 0..1 range CSS uses,
// so a translucent Config.BackgroundColour renders as configured rather than opaque.
func cssColour(colour Colour) string {
	alpha := strconv.FormatFloat(float64(colour.A)/255.0, 'f', -1, 64)
	return "rgba(" +
		strconv.Itoa(int(colour.R)) + "," +
		strconv.Itoa(int(colour.G)) + "," +
		strconv.Itoa(int(colour.B)) + "," +
		alpha + ")"
}

// contrastColour picks a legible text colour for a background of unknown brightness,
// so the message and the caption glyphs (which use currentColor) stay readable
// whatever BackgroundColour is. The weights are the Rec. 601 luma coefficients.
func contrastColour(colour Colour) string {
	luma := (int(colour.R)*299 + int(colour.G)*587 + int(colour.B)*114) / 1000
	if luma >= 140 {
		return "#1a1a1a"
	}
	return "#f2f2f2"
}

// errorPageTemplate is the fallback surface, rendered by errorPageURL. It lives in
// errorpage.html so it reads and edits as plain HTML, and is embedded at compile
// time - at run time it is still self-contained (inline CSS, inline SVG glyphs, no
// external asset) and needs no server. It stays ASCII-only: scripts/leak-scan.ps1
// holds .html source to the same ASCII rule as .go files.
//
// The title bar carries app-region: drag AND data-__NS__-drag: the first gives a
// real HTCAPTION on runtimes with non-client region support (native drag,
// double-click-maximise, snap), and the second lets the injected fallback drag
// script (host/js.go dragTemplateJS, which matches the default [data-<ns>-drag]
// selector) drag the window on older runtimes. The caption buttons and Retry carry
// app-region: no-drag and data-__NS__-no-drag so both paths treat them as clickable.
//
//go:embed errorpage.html
var errorPageTemplate string
