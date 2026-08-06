import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const templateURL = new URL("../host/bridge.js", import.meta.url);
const template = readFileSync(templateURL, "utf8");
const replacements = new Map([
  ["__NS__", "mullion"],
  ["__START_HIDDEN__", "false"],
  ["__M_DRAG__", "WindowStartDrag"],
  ["__M_RESIZE__", "WindowStartResize"],
  ["__M_MIN__", "WindowMinimise"],
  ["__M_MAXTOGGLE__", "WindowToggleMaximise"],
  ["__M_ISMAX__", "WindowIsMaximised"],
  ["__M_SHOW__", "WindowShow"],
  ["__M_HIDE__", "WindowHide"],
  ["__M_CLOSE__", "WindowClose"],
  ["__M_SHELLREADY__", "WindowShellReady"],
  ["__M_READY__", "WindowReady"],
  ["__M_PHASE__", "WindowPhase"],
  ["__M_DIAG__", "WindowDiagnostic"],
]);
let injected = template;
for (const [placeholder, value] of replacements) {
  injected = injected.replaceAll(placeholder, value);
}
assert.doesNotMatch(injected, /__[A-Z0-9_]+__/, "generated bridge contains an unresolved placeholder");

const posted = [];
const listeners = new Map();
const webview = {
  postMessage(message) {
    posted.push(message);
  },
  addEventListener(type, listener) {
    listeners.set(type, listener);
  },
};
const context = vm.createContext({
  window: { chrome: { webview } },
});
vm.runInContext(injected, context, { filename: "host/bridge.js" });

const detail = "x".repeat(241) + " https://mullion.localhost/app/main.js?secret=value";
context.window.mullion.diagnostic("error", detail);

assert.equal(posted.length, 1, "diagnostic did not post exactly one bridge message");
const payload = JSON.parse(posted[0]);
assert.equal(payload.method, "WindowDiagnostic");
assert.deepEqual(payload.args, ["error", detail], "bridge truncated or rewrote diagnostic detail");
assert.ok(posted[0].includes("main.js?secret=value"), "raw posted message lost the URL after byte 240");
assert.ok(listeners.has("message"), "generated bridge did not install its reply listener");

console.log("bridge vm behavior: ok");
