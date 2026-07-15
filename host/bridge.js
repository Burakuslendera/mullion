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
