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
