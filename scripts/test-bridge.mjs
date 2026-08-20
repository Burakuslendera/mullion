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
  ["__M_FRAMESTATE__", "WindowFrameState"],
  ["__E_FRAMESTATE__", "WindowFrameStateChanged"],
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

const observedFrameStates = [];
context.window.mullion.__frame.subscribe((state) => {
  observedFrameStates.push({
    maximised: state.maximised,
    moveSizeActive: state.moveSizeActive,
    generation: state.generation,
  });
});
listeners.get("message")({
  data: JSON.stringify({
    event: "WindowFrameStateChanged",
    state: { maximised: true, moveSizeActive: true, generation: 2 },
  }),
});
assert.deepEqual(
  observedFrameStates,
  [{ maximised: true, moveSizeActive: true, generation: 2 }],
  "bridge did not publish the native frame-state event",
);
listeners.get("message")({
  data: JSON.stringify({
    event: "WindowFrameStateChanged",
    state: { maximised: false, moveSizeActive: false, generation: 1 },
  }),
});
assert.equal(observedFrameStates.length, 1, "an older native frame-state event was accepted");

const frameRefresh = context.window.mullion.__frame.refresh();
const frameRequest = JSON.parse(posted.at(-1));
assert.equal(frameRequest.method, "WindowFrameState", "frame refresh called the wrong reserved method");
listeners.get("message")({
  data: JSON.stringify({
    id: frameRequest.id,
    ok: true,
    result: { maximised: false, moveSizeActive: false, generation: 2 },
  }),
});
await frameRefresh;
assert.deepEqual(
  observedFrameStates.at(-1),
  { maximised: false, moveSizeActive: false, generation: 2 },
  "newest frame snapshot did not reach subscribers",
);

const resizeTemplateURL = new URL("../host/resize.js", import.meta.url);
const resizeTemplate = readFileSync(resizeTemplateURL, "utf8");
const dragTemplateURL = new URL("../host/drag.js", import.meta.url);
const dragTemplate = readFileSync(dragTemplateURL, "utf8");

class FakeEventTarget {
  constructor() {
    this.listeners = new Map();
  }

  addEventListener(type, listener, options = {}) {
    const registered = this.listeners.get(type) || [];
    registered.push({ listener, once: options?.once === true });
    this.listeners.set(type, registered);
  }

  emit(type, event = {}) {
    const registered = this.listeners.get(type) || [];
    for (const entry of [...registered]) {
      entry.listener(event);
      if (entry.once) {
        this.listeners.set(type, this.listeners.get(type).filter((candidate) => candidate !== entry));
      }
    }
  }
}

class FakeStyle {
  set cssText(value) {
    this._cssText = value;
    for (const declaration of value.split(";")) {
      const separator = declaration.indexOf(":");
      if (separator < 0) continue;
      const property = declaration.slice(0, separator).trim();
      const propertyValue = declaration.slice(separator + 1).trim();
      if (property) this.setProperty(property, propertyValue);
    }
  }

  get cssText() {
    return this._cssText || "";
  }

  setProperty(property, value) {
    const key = property.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
    this[key] = String(value);
  }

  removeProperty(property) {
    const key = property.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
    const previous = this[key] || "";
    delete this[key];
    return previous;
  }
}

class FakeElement extends FakeEventTarget {
  constructor(tagName = "div") {
    super();
    this.tagName = tagName.toUpperCase();
    this.style = new FakeStyle();
    this.dataset = {};
    this.attributes = new Map();
    this.children = [];
    this.parentElement = null;
    this.clientWidth = 0;
    this.clientHeight = 0;
  }

  setAttribute(name, value) {
    const stringValue = String(value);
    this.attributes.set(name, stringValue);
    if (name.startsWith("data-")) {
      const datasetKey = name
        .slice(5)
        .replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
      this.dataset[datasetKey] = stringValue;
    }
  }

  appendChild(child) {
    child.parentElement = this;
    this.children.push(child);
    return child;
  }

  closest(selector) {
    const attributeMatch = /^\[([^\]]+)\]$/.exec(selector);
    if (!attributeMatch) return null;
    for (let element = this; element; element = element.parentElement) {
      if (element.attributes.has(attributeMatch[1])) return element;
    }
    return null;
  }
}

class FakeDocument extends FakeEventTarget {
  constructor(root) {
    super();
    this.documentElement = root;
    this.hidden = false;
  }

  createElement(tagName) {
    return new FakeElement(tagName);
  }
}

const resizeEdges = [
  "top-left",
  "top",
  "top-right",
  "right",
  "bottom-right",
  "bottom",
  "bottom-left",
  "left",
];
const cursorByEdge = {
  left: "ew-resize",
  right: "ew-resize",
  top: "ns-resize",
  bottom: "ns-resize",
  "top-left": "nwse-resize",
  "top-right": "nesw-resize",
  "bottom-left": "nesw-resize",
  "bottom-right": "nwse-resize",
};
const titlebarHeight = 32;
const captionControlsWidth = 138;

function injectResizeTemplate(border) {
  const resizeReplacements = new Map([
    ["__NS__", "mullion"],
    ["__BORDER__", String(border)],
    ["__TITLEBAR_H__", String(titlebarHeight)],
    ["__CONTROLS_W__", String(captionControlsWidth)],
    ["__EDGE_ATTR__", "data-mullion-resize-edge"],
    ["__DATASET__", "mullionResizeEdge"],
  ]);
  let resizeInjected = resizeTemplate;
  for (const [placeholder, value] of resizeReplacements) {
    resizeInjected = resizeInjected.replaceAll(placeholder, value);
  }
  assert.doesNotMatch(
    resizeInjected,
    /__[A-Z0-9_]+__/,
    "generated resize overlay contains an unresolved placeholder",
  );
  return resizeInjected;
}

function createResizeScenario(border, width = 1025, height = 769) {
  const root = new FakeElement("html");
  root.clientWidth = width;
  root.clientHeight = height;
  const document = new FakeDocument(root);
  const resizeCalls = [];
  const diagnostics = [];
  let frameState = { maximised: false, moveSizeActive: false, generation: 0 };
  let frameRequestGeneration = 0;
  let nextTimer = 1;
  const timers = new Map();
  const frameStateResponses = [];
  const frameStateSubscribers = [];
  const publishFrameState = (candidate) => {
    if (!candidate || candidate.generation < frameState.generation) return frameState;
    frameState = {
      maximised: candidate.maximised === true,
      moveSizeActive: candidate.moveSizeActive === true,
      generation: candidate.generation,
    };
    for (const subscriber of frameStateSubscribers) subscriber(frameState);
    return frameState;
  };
  const window = new FakeEventTarget();
  window.innerWidth = width;
  window.innerHeight = height;
  window.visualViewport = { width, height };
  window.Element = FakeElement;
  window.mullion = {
    diagnostic(kind, value) {
      diagnostics.push([kind, value]);
    },
    __frame: {
      subscribe(listener) {
        frameStateSubscribers.push(listener);
        listener(frameState);
        return () => {};
      },
      invalidate() {
        ++frameRequestGeneration;
      },
      refresh() {
        const requestGeneration = ++frameRequestGeneration;
        const response = frameStateResponses.shift() || Promise.resolve({ ...frameState });
        return response.then((candidate) => {
          if (requestGeneration !== frameRequestGeneration) return frameState;
          return publishFrameState(candidate);
        });
      },
    },
    window: {
      startResize(edge) {
        resizeCalls.push(edge);
      },
    },
  };
  const setTimeout = (callback) => {
    const id = nextTimer++;
    timers.set(id, callback);
    return id;
  };
  const clearTimeout = (id) => {
    timers.delete(id);
  };
  const runTimers = () => {
    while (timers.size > 0) {
      const pending = [...timers.entries()].sort(([left], [right]) => left - right);
      timers.clear();
      for (const [, callback] of pending) callback();
    }
  };
  const resizeContext = vm.createContext({
    window,
    document,
    Element: FakeElement,
    setTimeout,
    clearTimeout,
  });
  vm.runInContext(injectResizeTemplate(border), resizeContext, { filename: "host/resize.js" });
  return {
    root,
    document,
    window,
    resizeCalls,
    diagnostics,
    runTimers,
    deferFrameStateResult() {
      let resolve;
      let reject;
      const promise = new Promise((resolvePromise, rejectPromise) => {
        resolve = resolvePromise;
        reject = rejectPromise;
      });
      frameStateResponses.push(promise);
      return { resolve, reject };
    },
    setFrameState(next) {
      frameState = { ...frameState, ...next };
    },
    pushFrameState(next) {
      ++frameRequestGeneration;
      publishFrameState({ ...frameState, ...next });
    },
    setViewport(nextWidth, nextHeight) {
      root.clientWidth = nextWidth;
      root.clientHeight = nextHeight;
      window.innerWidth = nextWidth;
      window.innerHeight = nextHeight;
      window.visualViewport.width = nextWidth;
      window.visualViewport.height = nextHeight;
    },
  };
}

async function settlePromises() {
  await Promise.resolve();
  await Promise.resolve();
}

function zonesByEdge(scenario) {
  return new Map(
    scenario.root.children.map((zone) => [zone.dataset.mullionResizeEdge, zone]),
  );
}

function assertZoneGeometry(scenario, width, height, resizeX, resizeY, label) {
  const zones = zonesByEdge(scenario);
  const middleWidth = `${Math.max(0, width - resizeX * 2)}px`;
  const middleHeight = `${Math.max(0, height - resizeY * 2)}px`;
  assert.equal(zones.size, 8, `${label}: resize overlay did not install eight distinct zones`);
  for (const edge of resizeEdges) {
    const zone = zones.get(edge);
    assert.ok(zone, `${label}: missing ${edge} zone`);
    assert.equal(zone.style.position, "fixed", `${label}: ${edge} zone is not fixed`);
    assert.equal(zone.style.pointerEvents, "auto", `${label}: ${edge} zone does not accept pointers`);
    assert.equal(zone.style.cursor, cursorByEdge[edge], `${label}: ${edge} zone has the wrong cursor`);
    assert.equal(zone.style.display, "block", `${label}: ${edge} zone is unexpectedly disabled`);
    assert.equal(
      zone.style.appRegion,
      "no-drag",
      `${label}: ${edge} zone does not override standard app-region dragging`,
    );
    assert.equal(
      zone.style.WebkitAppRegion,
      "no-drag",
      `${label}: ${edge} zone does not override WebKit app-region dragging`,
    );
    if (edge === "left" || edge === "right") {
      assert.equal(zone.style[edge], "0", `${label}: ${edge} zone is not on its routed edge`);
      assert.equal(zone.style.top, `${resizeY}px`, `${label}: ${edge} zone overlaps a corner`);
      assert.equal(zone.style.width, `${resizeX}px`, `${label}: ${edge} zone has unbounded width`);
      assert.equal(zone.style.height, middleHeight, `${label}: ${edge} zone crosses corner routes`);
    } else if (edge === "top" || edge === "bottom") {
      assert.equal(zone.style[edge], "0", `${label}: ${edge} zone is not on its routed edge`);
      assert.equal(zone.style.left, `${resizeX}px`, `${label}: ${edge} zone overlaps a corner`);
      assert.equal(zone.style.width, middleWidth, `${label}: ${edge} zone crosses corner routes`);
      assert.equal(zone.style.height, `${resizeY}px`, `${label}: ${edge} zone has unbounded height`);
    } else {
      const [vertical, horizontal] = edge.split("-");
      assert.equal(zone.style[vertical], "0", `${label}: ${edge} zone is not on its vertical edge`);
      assert.equal(zone.style[horizontal], "0", `${label}: ${edge} zone is not on its horizontal edge`);
      assert.equal(zone.style.width, `${resizeX}px`, `${label}: ${edge} zone has unbounded width`);
      assert.equal(zone.style.height, `${resizeY}px`, `${label}: ${edge} zone has unbounded height`);
    }
  }
}
function dispatchPointerDown(scenario, target, clientX, clientY) {
  const event = {
    target,
    clientX,
    clientY,
    button: 0,
    isPrimary: true,
    defaultPrevented: false,
    propagationStopped: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
    stopPropagation() {
      this.propagationStopped = true;
    },
  };
  scenario.document.emit("pointerdown", event);
  return event;
}

async function verifyResizeScenario(border, label) {
  const scenario = createResizeScenario(border);
  await settlePromises();
  const resizeX = Math.min(border, Math.floor(1025 / 2));
  const resizeY = Math.min(border, Math.floor(769 / 2));
  assertZoneGeometry(scenario, 1025, 769, resizeX, resizeY, `${label} initial viewport`);

  const points = new Map([
    ["top-left", [0, 0]],
    ["top", [512, 0]],
    ["top-right", [1024, 0]],
    ["right", [1024, 384]],
    ["bottom-right", [1024, 768]],
    ["bottom", [512, 768]],
    ["bottom-left", [0, 768]],
    ["left", [0, 384]],
  ]);
  const zones = zonesByEdge(scenario);
  for (const [index, edge] of resizeEdges.entries()) {
    const conflictingTarget = zones.get(resizeEdges[(index + 1) % resizeEdges.length]);
    const [clientX, clientY] = points.get(edge);
    const event = dispatchPointerDown(scenario, conflictingTarget, clientX, clientY);
    assert.equal(
      scenario.resizeCalls.at(-1),
      edge,
      `${label}: ${edge} coordinates were overridden by the target dataset`,
    );
    assert.equal(event.defaultPrevented, true, `${label}: ${edge} resize was not consumed`);
    assert.equal(event.propagationStopped, true, `${label}: ${edge} resize propagated`);
  }
  assert.deepEqual(
    scenario.resizeCalls,
    resizeEdges,
    `${label}: representative points dispatched the wrong resize edges`,
  );

  const centerEvent = dispatchPointerDown(scenario, zones.get("left"), 512, 384);
  assert.equal(
    scenario.resizeCalls.length,
    resizeEdges.length,
    `${label}: exact center dispatched a resize from a stale target dataset`,
  );
  assert.equal(centerEvent.defaultPrevented, false, `${label}: exact center was consumed`);

  const noDrag = new FakeElement("button");
  noDrag.setAttribute("data-mullion-no-drag", "");
  const noDragEvent = dispatchPointerDown(scenario, noDrag, 512, 100);
  assert.equal(
    scenario.resizeCalls.length,
    resizeEdges.length,
    `${label}: data-mullion-no-drag did not suppress resize`,
  );
  assert.equal(noDragEvent.defaultPrevented, false, `${label}: no-drag pointer was consumed`);

  scenario.setViewport(801, 603);
  scenario.window.emit("resize");
  scenario.runTimers();
  await settlePromises();
  assertZoneGeometry(
    scenario,
    801,
    603,
    Math.min(border, Math.floor(801 / 2)),
    Math.min(border, Math.floor(603 / 2)),
    `${label} resized viewport`,
  );

  scenario.setFrameState({ maximised: true });
  scenario.window.emit("focus");
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", `${label}: maximised window left a resize zone enabled`);
  }
  const callsBeforeMaximisedPointer = scenario.resizeCalls.length;
  const maximisedEvent = dispatchPointerDown(scenario, zones.get("top-left"), 0, 0);
  assert.equal(
    scenario.resizeCalls.length,
    callsBeforeMaximisedPointer,
    `${label}: maximised window dispatched resize`,
  );
  assert.equal(maximisedEvent.defaultPrevented, false, `${label}: maximised pointer was consumed`);
}

async function verifyResizeEdgeAllowList() {
  const scenario = createResizeScenario(8);
  await settlePromises();

  for (const edge of [
    "toString",
    "valueOf",
    "constructor",
    "hasOwnProperty",
    "__proto__",
    "",
    "diagonal",
  ]) {
    const target = new FakeElement("div");
    target.setAttribute("data-mullion-resize-edge", edge);
    const event = dispatchPointerDown(scenario, target, Number.NaN, Number.NaN);
    assert.equal(
      scenario.resizeCalls.length,
      0,
      `invalid resize edge was dispatched: ${JSON.stringify(edge)}`,
    );
    assert.equal(
      event.defaultPrevented,
      false,
      `invalid resize edge was consumed: ${JSON.stringify(edge)}`,
    );
    assert.equal(
      event.propagationStopped,
      false,
      `invalid resize edge stopped propagation: ${JSON.stringify(edge)}`,
    );
  }

  for (const edge of resizeEdges) {
    const target = new FakeElement("div");
    target.setAttribute("data-mullion-resize-edge", edge);
    const event = dispatchPointerDown(scenario, target, Number.NaN, Number.NaN);
    assert.equal(
      scenario.resizeCalls.at(-1),
      edge,
      `valid own resize edge was rejected: ${edge}`,
    );
    assert.equal(event.defaultPrevented, true, `${edge} resize gesture was not consumed`);
    assert.equal(
      event.propagationStopped,
      true,
      `${edge} resize gesture did not stop propagation`,
    );
  }
  assert.deepEqual(scenario.resizeCalls, resizeEdges, "valid resize edges dispatched out of order");
}

async function verifyFrameStateOrderingAndMoveLoop() {
  const scenario = createResizeScenario(1_500_000_000);
  await settlePromises();

  const olderFocus = scenario.deferFrameStateResult();
  scenario.window.emit("focus");
  scenario.setFrameState({ maximised: true });
  scenario.window.emit("focus");
  await settlePromises();
  olderFocus.resolve({ maximised: false, moveSizeActive: false, generation: 0 });
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "an older focus query re-enabled a maximised zone");
  }

  const preResizeFocus = scenario.deferFrameStateResult();
  scenario.window.emit("focus");
  scenario.window.emit("resize");
  preResizeFocus.resolve({ maximised: false, moveSizeActive: false, generation: 0 });
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "a pre-resize query re-enabled a zone during debounce");
  }
  scenario.runTimers();
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "maximised zones were enabled after resize resync");
  }

  scenario.pushFrameState({ maximised: true, moveSizeActive: true, generation: 1 });
  scenario.setFrameState({ maximised: false, moveSizeActive: true, generation: 1 });
  scenario.window.emit("resize");
  scenario.runTimers();
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "restored drag-down enabled a zone before WM_EXITSIZEMOVE");
  }
  const callsDuringMoveLoop = scenario.resizeCalls.length;
  dispatchPointerDown(scenario, zonesByEdge(scenario).get("top"), 512, 0);
  assert.equal(
    scenario.resizeCalls.length,
    callsDuringMoveLoop,
    "an active native move loop dispatched a second resize gesture",
  );

  scenario.pushFrameState({ maximised: false, moveSizeActive: false, generation: 2 });
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "block", "restored zones did not re-enable after WM_EXITSIZEMOVE");
  }

  scenario.pushFrameState({ maximised: false, moveSizeActive: true, generation: 3 });
  scenario.window.emit("blur");
  scenario.pushFrameState({ maximised: false, moveSizeActive: false, generation: 4 });
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "a blurred document re-enabled resize zones");
  }
  scenario.window.emit("focus");
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "block", "focused restored document did not resync resize zones");
  }

  const rejectedRefresh = scenario.deferFrameStateResult();
  scenario.window.emit("focus");
  rejectedRefresh.reject(new Error("frame state unavailable"));
  await settlePromises();
  for (const zone of zonesByEdge(scenario).values()) {
    assert.equal(zone.style.display, "none", "failed frame-state query enabled a resize zone");
  }
}

function injectDragTemplate(border) {
  return dragTemplate
    .replaceAll("__NS__", "mullion")
    .replaceAll("__BORDER__", String(border))
    .replaceAll("__DRAG_SELECTOR__", JSON.stringify("[data-mullion-drag]"));
}

async function verifyDragFrameState() {
  const root = new FakeElement("html");
  const titlebar = new FakeElement("header");
  titlebar.setAttribute("data-mullion-drag", "");
  titlebar.getBoundingClientRect = () => ({ top: 0 });
  const target = new FakeElement("span");
  titlebar.appendChild(target);
  root.appendChild(titlebar);
  const document = new FakeDocument(root);
  const window = new FakeEventTarget();
  window.setTimeout = (callback) => { callback(); return 1; };
  let frameState = { maximised: false, moveSizeActive: false, generation: 0 };
  const subscribers = [];
  const dragCalls = [];
  const publish = (next) => {
    frameState = { ...frameState, ...next };
    for (const subscriber of subscribers) subscriber(frameState);
  };
  window.mullion = {
    __frame: {
      subscribe(listener) {
        subscribers.push(listener);
        listener(frameState);
        return () => {};
      },
      invalidate() {},
      refresh() {
        return Promise.resolve().then(() => {
          publish(frameState);
          return frameState;
        });
      },
    },
    window: {
      startDrag() {
        dragCalls.push("drag");
      },
    },
  };
  const dragContext = vm.createContext({ window, document, Element: FakeElement });
  const injectedDrag = injectDragTemplate(8);
  assert.doesNotMatch(injectedDrag, /__[A-Z0-9_]+__/, "generated drag contains a placeholder");
  vm.runInContext(injectedDrag, dragContext, { filename: "host/drag.js" });
  await settlePromises();
  const mouseDown = (clientY) => {
    document.emit("mousedown", { button: 0, target, detail: 1, clientY });
  };

  mouseDown(4);
  assert.equal(dragCalls.length, 0, "restored top resize band started fallback drag");
  mouseDown(20);
  assert.equal(dragCalls.length, 1, "restored title bar did not start fallback drag");

  publish({ maximised: false, moveSizeActive: true, generation: 1 });
  mouseDown(20);
  assert.equal(dragCalls.length, 1, "active native move loop started a second fallback drag");

  publish({ maximised: false, moveSizeActive: false, generation: 2 });
  mouseDown(20);
  assert.equal(dragCalls.length, 2, "fallback drag did not recover after native move-loop exit");

  publish({ maximised: true, moveSizeActive: false, generation: 3 });
  mouseDown(4);
  assert.equal(dragCalls.length, 3, "maximised title bar incorrectly reserved the restored resize band");
}

await verifyResizeScenario(8, "ordinary border");
await verifyResizeScenario(1_500_000_000, "extreme border");
await verifyResizeEdgeAllowList();
await verifyFrameStateOrderingAndMoveLoop();
await verifyDragFrameState();

console.log("bridge, frame-state resize and drag vm behavior: ok");
