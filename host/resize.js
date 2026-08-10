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
    let maximised = true;
    let moveSizeActive = true;
    let resyncPending = true;
    let resyncTimer = 0;
    let zonesEnabled = false;
    const cursorForEdge = (edge) => {
      if (edge === "left" || edge === "right") return "ew-resize";
      if (edge === "top" || edge === "bottom") return "ns-resize";
      if (edge === "top-left" || edge === "bottom-right") return "nwse-resize";
      return "nesw-resize";
    };
    const viewport = () => {
      const visual = window.visualViewport || {};
      return {
        width: Math.floor(Math.max(Number(root.clientWidth) || 0, Number(window.innerWidth) || 0, Number(visual.width) || 0)),
        height: Math.floor(Math.max(Number(root.clientHeight) || 0, Number(window.innerHeight) || 0, Number(visual.height) || 0))
      };
    };
    const effectiveBorder = (size) => Math.min(border, Math.floor(size / 2));
    const zoneStyles = {
      left: "left:0;cursor:ew-resize",
      right: "right:0;cursor:ew-resize",
      top: "top:0;cursor:ns-resize",
      bottom: "bottom:0;cursor:ns-resize",
      "top-left": "left:0;top:0;cursor:nwse-resize",
      "top-right": "right:0;top:0;cursor:nesw-resize",
      "bottom-left": "left:0;bottom:0;cursor:nesw-resize",
      "bottom-right": "right:0;bottom:0;cursor:nwse-resize"
    };
    const zones = Object.entries(zoneStyles).map(([edge, style]) => {
      const zone = document.createElement("div");
      zone.setAttribute("__EDGE_ATTR__", edge);
      zone.style.cssText = "position:fixed;z-index:2147483647;box-sizing:border-box;background:transparent;pointer-events:auto;app-region:no-drag;-webkit-app-region:no-drag;" + style;
      root.appendChild(zone);
      return { edge, node: zone };
    });
    const refreshZoneDimensions = () => {
      const { width, height } = viewport();
      const resizeX = effectiveBorder(width);
      const resizeY = effectiveBorder(height);
      const resizeXpx = resizeX + "px";
      const resizeYpx = resizeY + "px";
      const middleWidth = Math.max(0, width - resizeX * 2) + "px";
      const middleHeight = Math.max(0, height - resizeY * 2) + "px";
      for (const { edge, node } of zones) {
        if (edge === "left" || edge === "right") {
          node.style.top = resizeYpx;
          node.style.width = resizeXpx;
          node.style.height = middleHeight;
        } else if (edge === "top" || edge === "bottom") {
          node.style.left = resizeXpx;
          node.style.width = middleWidth;
          node.style.height = resizeYpx;
        } else {
          node.style.width = resizeXpx;
          node.style.height = resizeYpx;
        }
      }
    };
    const clearCursor = () => { root.style.cursor = ""; };
    const setZonesEnabled = (enabled) => {
      zonesEnabled = enabled;
      for (const { node } of zones) node.style.display = enabled ? "block" : "none";
      if (!enabled) clearCursor();
      const state = enabled ? "enabled" : "disabled";
      if (lastCursorState === state) return;
      lastCursorState = state;
      api.diagnostic("resize-cursor", state);
    };
    const applyFrameState = (state) => {
      if (!state) return;
      maximised = state.maximised === true;
      moveSizeActive = state.moveSizeActive === true;
      setZonesEnabled(!resyncPending && !maximised && !moveSizeActive);
    };
    // A maximised window and a window already inside the native move/size loop
    // must not offer a second gesture through the frontend overlay.
    const syncZones = () => {
      refreshZoneDimensions();
      setZonesEnabled(false);
      resyncPending = false;
      api.__frame.refresh().catch(() => { setZonesEnabled(false); });
    };
    api.__frame.subscribe(applyFrameState);
    api.diagnostic("resize-cursor", "installed");
    syncZones();
    window.addEventListener("resize", () => {
      resyncPending = true;
      api.__frame.invalidate();
      refreshZoneDimensions();
      setZonesEnabled(false);
      clearTimeout(resyncTimer);
      resyncTimer = setTimeout(syncZones, 150);
    });
    window.addEventListener("focus", syncZones);
    const edgeForEvent = (event) => {
      const { width, height } = viewport();
      const x = Number(event.clientX);
      const y = Number(event.clientY);
      if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
      if (x < 0 || x >= width || y < 0 || y >= height) return "";
      const resizeX = effectiveBorder(width);
      const resizeY = effectiveBorder(height);
      const left = x < resizeX;
      const right = x >= width - resizeX;
      const top = y < resizeY;
      const bottom = y >= height - resizeY;
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
      if (!zonesEnabled || maximised || moveSizeActive || event.button !== 0 || event.isPrimary === false) return;
      if (event.target instanceof Element && event.target.closest("[data-__NS__-no-drag]")) return;
      const coordinateEdge = edgeForEvent(event);
      const edge = coordinateEdge === null ? edgeFromTarget(event.target) : coordinateEdge;
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
    window.addEventListener("blur", () => {
      resyncPending = true;
      api.__frame.invalidate();
      setZonesEnabled(false);
      clearCursor();
    });
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) return;
      resyncPending = true;
      api.__frame.invalidate();
      setZonesEnabled(false);
      clearCursor();
    });
  };
  install();
})();
