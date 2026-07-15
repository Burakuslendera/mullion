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
