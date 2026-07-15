package host

import (
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
		"__DRAG_SELECTOR__", config.DragSelector,
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

// bridgeTemplateJS installs window.<ns>. Everything the frontend needs reaches
// it through this object: there is no module to import and no generated binding
// file to keep in sync, so the frontend can be plain HTML on disk.
const bridgeTemplateJS = `
(() => {
  if (window.__NS__ && window.__NS__.__bound) return;
  const pending = new Map();
  let seq = 0;
  const send = (payload) => {
    try {
      window.chrome.webview.postMessage(JSON.stringify(payload));
      return true;
    } catch {
      return false;
    }
  };
  // notify is fire-and-forget: window controls do not need a reply, and waiting
  // for one would add a round trip to every title bar click.
  const notify = (method, ...args) => { send({ id: "n" + (++seq), method, args }); };
  const invoke = (method, ...args) => new Promise((resolve, reject) => {
    const id = "c" + (++seq);
    pending.set(id, { resolve, reject });
    if (!send({ id, method, args })) {
      pending.delete(id);
      reject(new Error("mullion: bridge unavailable"));
    }
  });
  window.chrome.webview.addEventListener("message", (event) => {
    let payload = event.data;
    if (typeof payload === "string") {
      try { payload = JSON.parse(payload); } catch { return; }
    }
    if (!payload || !payload.id || typeof payload.ok !== "boolean") return;
    const entry = pending.get(payload.id);
    if (!entry) return;
    pending.delete(payload.id);
    if (payload.ok) entry.resolve(payload.result);
    else entry.reject(new Error(payload.error || "mullion: bridge call failed"));
  });
  window.__NS__ = {
    __bound: true,
    invoke,
    window: {
      minimise: () => notify("__M_MIN__"),
      toggleMaximise: () => notify("__M_MAXTOGGLE__"),
      isMaximised: () => invoke("__M_ISMAX__"),
      startDrag: () => notify("__M_DRAG__"),
      startResize: (edge) => notify("__M_RESIZE__", edge),
      show: () => notify("__M_SHOW__"),
      hide: () => notify("__M_HIDE__"),
      close: () => notify("__M_CLOSE__"),
    },
    shellReady: () => notify("__M_SHELLREADY__"),
    ready: () => notify("__M_READY__"),
    phase: (name) => notify("__M_PHASE__", String(name || "unknown")),
    diagnostic: (kind, detail) => notify("__M_DIAG__", String(kind || "unknown"), String(detail || "unknown").slice(0, 240)),
    startup: { startHidden: __START_HIDDEN__ },
    tabTitlebar: false,
  };
})();
`

// diagnosticsTemplateJS reports what the document actually did. Without it, the
// failure mode "WebView2 embedded, navigated, and painted nothing" is invisible
// from the Go side: the render watchdog would fire with no idea whether the
// scripts loaded, the stylesheets 404'd, or a script threw.
const diagnosticsTemplateJS = `
(() => {
  const api = window.__NS__;
  if (!api || api.__diagnosticsBound) return;
  api.__diagnosticsBound = true;
  const resourceName = (target) => {
    if (!target) return "unknown";
    return target.currentSrc || target.src || target.href || target.id || target.tagName || "unknown";
  };
  api.diagnostic("phase", "document created");
  const snapshot = (phase) => {
    const scripts = document.scripts ? document.scripts.length : 0;
    const links = document.querySelectorAll ? document.querySelectorAll("link[rel='stylesheet']").length : 0;
    api.diagnostic("phase", phase);
    api.diagnostic("dom", "scripts=" + scripts + ",stylesheets=" + links);
  };
  document.addEventListener("DOMContentLoaded", () => { snapshot("dom content loaded"); }, { once: true });
  window.addEventListener("load", () => { snapshot("window loaded"); }, { once: true });
  window.addEventListener("error", (event) => {
    if (event && event.target && event.target !== window) {
      const tag = event.target.tagName ? event.target.tagName.toLowerCase() : "resource";
      api.diagnostic("resource-" + tag, resourceName(event.target));
      return;
    }
    api.diagnostic("error", (event && event.message) || "window error");
  }, true);
  window.addEventListener("unhandledrejection", (event) => {
    const reason = event && event.reason;
    api.diagnostic("unhandledrejection", reason && reason.message ? reason.message : String(reason || "unknown"));
  });
})();
`

const navigationEvalTemplateJS = `
(() => {
  try {
    const api = window.__NS__;
    if (!api) return;
    const scripts = document.scripts ? document.scripts.length : 0;
    const links = document.querySelectorAll ? document.querySelectorAll("link[rel='stylesheet']").length : 0;
    const bodyChildren = document.body ? document.body.children.length : 0;
    api.diagnostic("dom", "after_navigation:scripts=" + scripts + ",stylesheets=" + links + ",body_children=" + bodyChildren);
  } catch {}
})();
`

// dragTemplateJS is the FALLBACK drag path, for runtimes without non-client
// region support. When the runtime is new enough the host enables non-client
// regions and CSS "app-region: drag" produces a real HTCAPTION, which is
// strictly better: the shell handles the drag, so double-click-to-maximise and
// snap-on-drag-to-edge come for free. This path exists so an old runtime
// degrades to a draggable window instead of a stuck one.
const dragTemplateJS = `
(() => {
  const api = window.__NS__;
  if (!api || api.__dragBound) return;
  api.__dragBound = true;
  const topResizeBorder = __BORDER__;
  let maximised = false;
  const syncMaximised = () => {
    api.window.isMaximised().then((value) => { maximised = value === true; }).catch(() => {});
  };
  syncMaximised();
  window.addEventListener("focus", syncMaximised);
  window.addEventListener("resize", () => window.setTimeout(syncMaximised, 120));
  document.addEventListener("mousedown", (event) => {
    if (event.button !== 0) return;
    const target = event.target;
    if (!(target instanceof Element)) return;
    const titlebar = target.closest("__DRAG_SELECTOR__");
    if (!titlebar) return;
    if (target.closest("[data-__NS__-no-drag]")) return;
    // Let a double click through: the second click is what maximises, and
    // starting a drag on it would swallow the gesture.
    if (event.detail > 1) return;
    // While restored, the top few pixels of the title bar belong to the resize
    // band, not to the drag region.
    if (!maximised && typeof titlebar.getBoundingClientRect === "function") {
      const localY = event.clientY - titlebar.getBoundingClientRect().top;
      if (localY >= 0 && localY < topResizeBorder) return;
    }
    api.window.startDrag();
  }, true);
})();
`

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
const resizeTemplateJS = `
(() => {
  const api = window.__NS__;
  if (!api || api.__resizeBound) return;
  api.__resizeBound = true;
  const install = () => {
    const root = document.documentElement;
    if (!root) {
      document.addEventListener("DOMContentLoaded", install, { once: true });
      return;
    }
    const border = __BORDER__;
    const titlebarHeight = __TITLEBAR_H__;
    const captionControlsWidth = __CONTROLS_W__;
    let lastResize = { edge: "", at: 0 };
    let lastCursorState = "";
    let maximised = false;
    let resyncTimer = 0;
    const cursorForEdge = (edge) => {
      if (edge === "left" || edge === "right") return "ew-resize";
      if (edge === "top" || edge === "bottom") return "ns-resize";
      if (edge === "top-left" || edge === "bottom-right") return "nwse-resize";
      return "nesw-resize";
    };
    const zoneStyles = {
      left: "left:0;top:0;width:" + border + "px;height:100%;cursor:ew-resize",
      right: "right:0;top:" + titlebarHeight + "px;width:" + border + "px;height:calc(100% - " + titlebarHeight + "px);cursor:ew-resize",
      top: "left:0;top:0;right:" + captionControlsWidth + "px;height:" + border + "px;cursor:ns-resize",
      bottom: "left:0;bottom:0;width:100%;height:" + border + "px;cursor:ns-resize",
      "top-left": "left:0;top:0;width:" + border + "px;height:" + border + "px;cursor:nwse-resize",
      "top-right": "right:" + captionControlsWidth + "px;top:0;width:" + border + "px;height:" + border + "px;cursor:nesw-resize",
      "bottom-left": "left:0;bottom:0;width:" + border + "px;height:" + border + "px;cursor:nesw-resize",
      "bottom-right": "right:0;bottom:0;width:" + border + "px;height:" + border + "px;cursor:nwse-resize"
    };
    const zones = Object.entries(zoneStyles).map(([edge, style]) => {
      const zone = document.createElement("div");
      zone.setAttribute("__EDGE_ATTR__", edge);
      zone.style.cssText = "position:fixed;z-index:2147483647;background:transparent;pointer-events:auto;" + style;
      root.appendChild(zone);
      return zone;
    });
    const clearCursor = () => { root.style.cursor = ""; };
    const setZonesEnabled = (enabled) => {
      for (const zone of zones) zone.style.display = enabled ? "block" : "none";
      if (!enabled) clearCursor();
      const state = enabled ? "enabled" : "disabled";
      if (lastCursorState === state) return;
      lastCursorState = state;
      api.diagnostic("resize-cursor", state);
    };
    // A maximised window must not offer resize zones: the edges belong to the
    // work area, and a resize there would silently un-maximise the window.
    const syncZones = () => {
      setZonesEnabled(false);
      api.window.isMaximised()
        .then((value) => { maximised = value === true; setZonesEnabled(!maximised); })
        .catch(() => { maximised = false; setZonesEnabled(true); });
    };
    api.diagnostic("resize-cursor", "installed");
    syncZones();
    window.addEventListener("resize", () => {
      setZonesEnabled(false);
      clearTimeout(resyncTimer);
      resyncTimer = setTimeout(syncZones, 150);
    });
    window.addEventListener("focus", syncZones);
    const viewport = () => {
      const visual = window.visualViewport || {};
      return {
        width: Math.max(Number(root.clientWidth) || 0, Number(window.innerWidth) || 0, Math.floor(Number(visual.width) || 0)),
        height: Math.max(Number(root.clientHeight) || 0, Number(window.innerHeight) || 0, Math.floor(Number(visual.height) || 0))
      };
    };
    const edgeForEvent = (event) => {
      const { width, height } = viewport();
      const left = event.clientX >= 0 && event.clientX < border;
      const right = event.clientX <= width && event.clientX >= width - border;
      const top = event.clientY >= 0 && event.clientY < border;
      const bottom = event.clientY <= height && event.clientY >= height - border;
      if (top && left) return "top-left";
      if (top && right) return "top-right";
      if (bottom && left) return "bottom-left";
      if (bottom && right) return "bottom-right";
      if (left) return "left";
      if (right) return "right";
      if (top) return "top";
      if (bottom) return "bottom";
      return "";
    };
    const edgeFromTarget = (target) => {
      if (!(target instanceof Element)) return "";
      const edge = target.dataset.__DATASET__ || "";
      return edge in zoneStyles ? edge : "";
    };
    const onPointerDown = (event) => {
      if (maximised || event.button !== 0 || event.isPrimary === false) return;
      if (event.target instanceof Element && event.target.closest("[data-__NS__-no-drag]")) return;
      const edge = edgeFromTarget(event.target) || edgeForEvent(event);
      if (!edge) return;
      event.preventDefault();
      event.stopPropagation();
      root.style.cursor = cursorForEdge(edge);
      // Both pointerdown and mousedown fire for one gesture; without this guard
      // the window procedure would start two resize loops for a single press.
      const now = Date.now();
      if (lastResize.edge === edge && now - lastResize.at < 250) return;
      lastResize = { edge, at: now };
      api.diagnostic("resize-edge", edge);
      api.window.startResize(edge);
    };
    document.addEventListener("pointerup", clearCursor, true);
    document.addEventListener("pointercancel", clearCursor, true);
    document.addEventListener("mouseup", clearCursor, true);
    document.addEventListener("mouseleave", clearCursor, true);
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("mousedown", onPointerDown, true);
    window.addEventListener("blur", clearCursor);
    document.addEventListener("visibilitychange", () => { if (document.hidden) clearCursor(); });
  };
  install();
})();
`
