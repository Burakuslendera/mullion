package host

import (
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
	document := replacer.Replace(errorPageTemplate)
	return "data:text/html," + url.PathEscape(document)
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

// errorPageTemplate is the fallback surface, rendered by errorPageURL. It is
// deliberately self-contained (inline CSS, inline SVG glyphs, no external asset) so
// it needs no server, and ASCII-only so it passes TestNoNonASCIIInSource.
//
// The title bar carries app-region: drag AND data-__NS__-drag: the first gives a
// real HTCAPTION on runtimes with non-client region support (native drag,
// double-click-maximise, snap), and the second lets the injected fallback drag
// script (host/js.go dragTemplateJS, which matches the default [data-<ns>-drag]
// selector) drag the window on older runtimes. The caption buttons and Retry carry
// app-region: no-drag and data-__NS__-no-drag so both paths treat them as clickable.
const errorPageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Couldn't load</title>
<style>
*{box-sizing:border-box}
html,body{margin:0;height:100%}
body{background:__BG__;color:__FG__;font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;display:flex;flex-direction:column;min-height:100vh}
.titlebar{height:__TITLEBAR_H__px;flex:0 0 auto;display:flex;justify-content:flex-end;align-items:stretch;user-select:none;app-region: drag;-webkit-app-region:drag}
.caption{width:__CONTROLS_W__px;display:flex;align-items:stretch;app-region:no-drag;-webkit-app-region:no-drag}
.caption button{flex:1 1 0;border:0;margin:0;padding:0;background:transparent;color:inherit;cursor:default;display:inline-flex;align-items:center;justify-content:center;app-region:no-drag;-webkit-app-region:no-drag}
.caption button:hover{background:rgba(127,127,127,0.2)}
.caption button.close:hover{background:#e81123;color:#fff}
.content{flex:1 1 auto;display:flex;flex-direction:column;align-items:center;justify-content:center;text-align:center;padding:24px;gap:14px}
.title{font-size:16px;font-weight:600}
.origin{opacity:0.75;word-break:break-all}
.retry{app-region:no-drag;-webkit-app-region:no-drag;display:inline-block;margin-top:6px;padding:8px 22px;font:inherit;color:inherit;text-decoration:none;cursor:default;border:1px solid currentColor;border-radius:6px;opacity:0.85}
.retry:hover{opacity:1;background:rgba(127,127,127,0.15)}
</style>
</head>
<body>
<div class="titlebar" data-__NS__-drag>
<div class="caption">
<button type="button" class="min" title="Minimise" aria-label="Minimise" data-__NS__-no-drag onclick="window.__NS__.window.minimise()"><svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M0 5 H10" stroke="currentColor" stroke-width="1" fill="none"/></svg></button>
<button type="button" class="max" title="Maximise" aria-label="Maximise" data-__NS__-no-drag onclick="window.__NS__.window.toggleMaximise()"><svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><rect x="0.5" y="0.5" width="9" height="9" stroke="currentColor" stroke-width="1" fill="none"/></svg></button>
<button type="button" class="close" title="Close" aria-label="Close" data-__NS__-no-drag onclick="window.__NS__.window.close()"><svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M0 0 L10 10 M10 0 L0 10" stroke="currentColor" stroke-width="1" fill="none"/></svg></button>
</div>
</div>
<main class="content">
<div class="title">Couldn't load</div>
<div class="origin">__ORIGIN__</div>
<a class="retry" href="__ORIGIN__" data-__NS__-no-drag>Retry</a>
</main>
</body>
</html>`
