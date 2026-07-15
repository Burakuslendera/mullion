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
