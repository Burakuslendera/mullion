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
