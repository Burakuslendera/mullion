// window.mullion is injected by the host before this script runs. There is no
// module to import and no generated binding file to keep in sync.
const mullion = window.mullion;

const $ = (id) => document.getElementById(id);

// Caption buttons. These call the host directly; the host does not route them
// through the application's Bridge.
$("minimise").addEventListener("click", () => mullion.window.minimise());
$("maximise").addEventListener("click", () => mullion.window.toggleMaximise());
$("close").addEventListener("click", () => mullion.window.close());

// An application-defined method. This one does reach Bridge in main.go.
$("now").addEventListener("click", async () => {
  try {
    $("now-result").textContent = await mullion.invoke("Now");
  } catch (err) {
    $("now-result").textContent = `error: ${err.message}`;
  }
});

async function refreshState() {
  const maximised = await mullion.window.isMaximised();
  $("state").textContent = maximised ? "maximised" : "restored";
}

window.addEventListener("resize", () => {
  refreshState().catch(() => {});
});

document.addEventListener("DOMContentLoaded", async () => {
  // Releases the startup show gate. Until this lands the host keeps the window
  // hidden, so the user never sees an empty white frame.
  mullion.shellReady();

  // Which drag path is live? With a WebView2 runtime new enough for non-client
  // regions the host sets this flag and CSS app-region does the work; otherwise
  // the injected JavaScript fallback handles the title bar.
  $("path").textContent = mullion.tabTitlebar
    ? "native non-client region (app-region: drag)"
    : "JavaScript fallback (old WebView2 runtime)";

  // Prove the bridge round-trip without needing anyone to click anything: this
  // is what the screenshot has to show.
  try {
    $("ping").textContent = await mullion.invoke("Ping");
  } catch (err) {
    $("ping").textContent = `error: ${err.message}`;
  }

  refreshState().catch(() => {});

  // Stops the render watchdog. Sent after the first paint, not on
  // DOMContentLoaded: the point of the signal is "the user can see something",
  // not "the DOM parsed".
  requestAnimationFrame(() => requestAnimationFrame(() => mullion.ready()));
});
