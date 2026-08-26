package rush

import "strings"

var browserControllerHTML = strings.Replace(
	runtimeHTML,
	"<script>",
	"<script>"+string(browserRuntimeJS)+"</script><script>",
	1,
)

const runtimeHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Rush</title></head><body><script>
(() => {
  "use strict";
  const native = {
    setTimeout: window.setTimeout.bind(window),
    clearTimeout: window.clearTimeout.bind(window),
    setInterval: window.setInterval.bind(window),
    clearInterval: window.clearInterval.bind(window),
    requestAnimationFrame: window.requestAnimationFrame.bind(window),
    cancelAnimationFrame: window.cancelAnimationFrame.bind(window),
    addEventListener: EventTarget.prototype.addEventListener,
    removeEventListener: EventTarget.prototype.removeEventListener,
  };
  const timerHandles = new Map();
  const listeners = [];
  const compiledBundles = new Map();
  const bundleCacheLimit = 64;
  const appRealms = new Map();
  let appSequence = 0;
  let nativeNetwork = 0;
  let collecting = false;
  let intentionalWait = 0;
  let sessionTiming = {wall: 0, runner: 0, application: 0, network: 0, wait: 0};
  let sessionBatch = null;
  let baselineGlobals;

  function formatError(error) {
    if (!error) return String(error);
    const message = (error.name || "Error") + (error.message ? ": " + error.message : "");
    const stack = String(error.stack || "");
    return stack.startsWith(message) ? stack : message + (stack ? "\n" + stack : "");
  }

  function matchesRequest(pattern, request) {
    if (typeof pattern === "function") return Boolean(pattern(request));
    if (pattern instanceof RegExp) {
      pattern.lastIndex = 0;
      return pattern.test(request.url);
    }
    if (!pattern.includes("*")) return request.url === pattern;
    const escaped = pattern.split("**").map(part => part.split("*").map(segment =>
      segment.replace(/[.+?^${}()|[\]\\]/g, "\\$&")
    ).join("[^/]*")).join(".*");
    return new RegExp("^" + escaped + "$").test(request.url);
  }

  function makeNetwork(realm) {
    return {
      route(pattern, handler) {
        const registration = {pattern, handler};
        realm.routes.push(registration);
        return () => {
          const index = realm.routes.indexOf(registration);
          if (index >= 0) realm.routes.splice(index, 1);
        };
      },
      requests(pattern) {
        return Object.freeze(realm.requests.filter(request => pattern === undefined || matchesRequest(pattern, request)));
      },
      waitForRequest(pattern, options = {}) {
        const found = realm.requests.find(request => matchesRequest(pattern, request));
        if (found) return Promise.resolve(found);
        return new Promise((resolve, reject) => {
          const timeout = options.timeout ?? 5000;
          const waiter = {pattern, resolve, reject, timer: native.setTimeout(() => {
            const index = realm.waiters.indexOf(waiter);
            if (index >= 0) realm.waiters.splice(index, 1);
            reject(new Error("Timed out waiting for application request after " + timeout + "ms"));
          }, timeout)};
          realm.waiters.push(waiter);
        });
      },
    };
  }

  function recordRequest(realm, request) {
    const immutable = Object.freeze({...request, headers: Object.freeze({...request.headers})});
    realm.requests.push(immutable);
    for (const waiter of [...realm.waiters]) {
      if (!matchesRequest(waiter.pattern, immutable)) continue;
      native.clearTimeout(waiter.timer);
      realm.waiters.splice(realm.waiters.indexOf(waiter), 1);
      waiter.resolve(immutable);
    }
    return immutable;
  }

  async function handleRequest(request) {
    const realm = appRealms.get(request.realm);
    if (!realm) {
      await window.__rushAppRequestResult(request.id, JSON.stringify({action: "continue"}));
      return;
    }
    const inspected = recordRequest(realm, request);
    const registration = [...realm.routes].reverse().find(item => matchesRequest(item.pattern, inspected));
    if (!registration) {
      await window.__rushAppRequestResult(request.id, JSON.stringify({action: "continue"}));
      return;
    }
    let decision;
    const settle = value => {
      if (decision) throw new Error("Application route was already resolved");
      decision = value;
    };
    const route = Object.freeze({
      request: inspected,
      fulfill: (response = {}) => settle({action: "fulfill", ...response}),
      continue: (overrides = {}) => settle({action: "continue", ...overrides}),
      abort: (reason = "request aborted by Rush route") => settle({action: "abort", body: reason}),
    });
    try {
      await registration.handler(route);
      decision ||= {action: "continue"};
    } catch (error) {
      decision = {action: "abort", body: formatError(error)};
    }
    await window.__rushAppRequestResult(request.id, JSON.stringify(decision));
  }

  function nextLayout() {
    return new Promise(resolve => native.requestAnimationFrame(() => native.requestAnimationFrame(resolve)));
  }

  async function elementPoint(element) {
    if (!element.isConnected) throw new Error("Trusted native input target is detached");
    element.scrollIntoView({block: "center", inline: "center"});
    await nextLayout();
    const view = element.ownerDocument.defaultView;
    const candidate = [...element.getClientRects()].map(rect => ({
      left: Math.max(0, rect.left),
      top: Math.max(0, rect.top),
      right: Math.min(view.innerWidth, rect.right),
      bottom: Math.min(view.innerHeight, rect.bottom),
    })).find(rect => {
      if (rect.right <= rect.left || rect.bottom <= rect.top) return false;
      const hit = element.ownerDocument.elementFromPoint((rect.left + rect.right) / 2, (rect.top + rect.bottom) / 2);
      return hit === element || element.contains(hit);
    });
    if (!candidate) throw new Error("Trusted native input target has no unobscured rendered bounds");
    let x = (candidate.left + candidate.right) / 2;
    let y = (candidate.top + candidate.bottom) / 2;
    let current = element.ownerDocument.defaultView;
    while (current && current !== current.top) {
      const frame = current.frameElement;
      if (!frame) break;
      const frameRect = frame.getBoundingClientRect();
      const scaleX = frame.offsetWidth ? frameRect.width / frame.offsetWidth : 1;
      const scaleY = frame.offsetHeight ? frameRect.height / frame.offsetHeight : 1;
      x = frameRect.left + (frame.clientLeft + x) * scaleX;
      y = frameRect.top + (frame.clientTop + y) * scaleY;
      current = frame.ownerDocument.defaultView;
    }
    return {targeted: true, x, y};
  }

  function makeNativeAutomation() {
    let readiness;
    const prepare = () => readiness ||= window.__rushPrepareNativeInput();
    const send = async (action, element, extra = {}) => {
      await prepare();
      const point = element ? await elementPoint(element) : {};
      await window.__rushNativeInput(JSON.stringify({action, ...point, ...extra}));
    };
    const observe = async (element, type, count, action, message, timeout = 1000) => {
      if (count === 0) return action();
      let cleanup = () => {};
      const received = new Promise((resolve, reject) => {
        let remaining = count;
        const observed = event => {
          if (!event.isTrusted || type === "keyup" && ["Shift", "Control", "Alt", "Meta"].includes(event.key)) return;
          if (--remaining !== 0) return;
          cleanup();
          resolve();
        };
        const timer = native.setTimeout(() => {
          cleanup();
          reject(new Error(message));
        }, timeout);
        cleanup = () => {
          native.clearTimeout(timer);
          native.removeEventListener.call(element, type, observed);
        };
        native.addEventListener.call(element, type, observed);
      });
      try {
        await action();
        await received;
      } finally {
        cleanup();
      }
    };
    return {
      click: element => observe(element, "click", 1, () => send("click", element), "Trusted native click did not reach its target"),
      type: (element, text) => observe(element, "keyup", [...text].length, () => send("type", element, {text}), "Trusted native typing did not reach its target", Math.max(1000, [...text].length * 50)),
      press: (key, element) => send("press", element, {key}),
    };
  }

  async function clearOriginStorage() {
    try { localStorage.clear(); } catch (_) {}
    try { sessionStorage.clear(); } catch (_) {}
    try { document.cookie.split(";").forEach(cookie => { document.cookie = cookie.split("=")[0].trim() + "=;expires=Thu, 01 Jan 1970 00:00:00 GMT;path=/"; }); } catch (_) {}
    try {
      if (indexedDB.databases) {
        for (const database of await indexedDB.databases()) {
          if (!database.name) continue;
          await new Promise(resolve => {
            const deletion = indexedDB.deleteDatabase(database.name);
            deletion.onsuccess = deletion.onerror = deletion.onblocked = resolve;
          });
        }
      }
    } catch (_) {}
    try { await caches.keys().then(keys => Promise.all(keys.map(key => caches.delete(key)))); } catch (_) {}
    try {
      if (navigator.serviceWorker) {
        await navigator.serviceWorker.getRegistrations().then(registrations => Promise.all(registrations.map(registration => registration.unregister())));
      }
    } catch (_) {}
  }

  async function createAppRealm() {
    await clearOriginStorage();
    const id = "app-" + (++appSequence);
    const frame = document.createElement("iframe");
    frame.dataset.rushApp = id;
    frame.setAttribute("aria-label", "Rush application under test");
    frame.style.cssText = "border:0;width:100%;height:100%;position:fixed;inset:0";
    document.body.append(frame);
    const realm = {id, frame, currentURL: "about:blank", routes: [], requests: [], waiters: []};
    realm.network = makeNetwork(realm);
    appRealms.set(id, realm);
    return {
      window: () => frame.contentWindow,
      document: () => frame.contentDocument,
      url: () => realm.currentURL,
      async goto(url) {
        const proxied = await window.__rushAppNavigate(id, url);
        await new Promise((resolve, reject) => {
          const loaded = () => { cleanup(); realm.currentURL = url; resolve(); };
          const failed = () => { cleanup(); reject(new Error("Application navigation failed: " + url)); };
          const cleanup = () => {
            native.removeEventListener.call(frame, "load", loaded);
            native.removeEventListener.call(frame, "error", failed);
          };
          native.addEventListener.call(frame, "load", loaded, {once: true});
          native.addEventListener.call(frame, "error", failed, {once: true});
          frame.src = proxied;
        });
      },
      network: realm.network,
      native: makeNativeAutomation(),
      async dispose() {
        for (const waiter of realm.waiters.splice(0)) {
          native.clearTimeout(waiter.timer);
          waiter.reject(new Error("Application realm was disposed"));
        }
        appRealms.delete(id);
        await window.__rushAppReset(id);
        frame.remove();
        await clearOriginStorage();
      },
    };
  }

  EventTarget.prototype.addEventListener = function(type, listener, options) {
    if (collecting) listeners.push([this, type, listener, options]);
    return native.addEventListener.call(this, type, listener, options);
  };
  EventTarget.prototype.removeEventListener = function(type, listener, options) {
    return native.removeEventListener.call(this, type, listener, options);
  };
  window.setTimeout = (fn, delay = 0, ...args) => {
    const requested = Math.max(0, Number(delay) || 0);
    const handle = native.setTimeout((...callbackArgs) => {
      timerHandles.delete(handle);
      if (collecting) intentionalWait += requested;
      fn(...callbackArgs);
    }, requested, ...args);
    timerHandles.set(handle, "timeout");
    return handle;
  };
  window.clearTimeout = handle => { timerHandles.delete(handle); native.clearTimeout(handle); };
  window.setInterval = (fn, delay = 0, ...args) => {
    const requested = Math.max(0, Number(delay) || 0);
    const handle = native.setInterval((...callbackArgs) => {
      if (collecting) intentionalWait += requested;
      fn(...callbackArgs);
    }, requested, ...args);
    timerHandles.set(handle, "interval");
    return handle;
  };
  window.clearInterval = handle => { timerHandles.delete(handle); native.clearInterval(handle); };
  window.requestAnimationFrame = fn => {
    const handle = native.requestAnimationFrame(time => { timerHandles.delete(handle); fn(time); });
    timerHandles.set(handle, "animation");
    return handle;
  };
  window.cancelAnimationFrame = handle => { timerHandles.delete(handle); native.cancelAnimationFrame(handle); };

  async function clearState() {
    collecting = false;
    for (const realm of [...appRealms.values()]) {
      appRealms.delete(realm.id);
      try { await window.__rushAppReset(realm.id); } catch (_) {}
      realm.frame.remove();
    }
    for (const [handle, kind] of timerHandles) {
      if (kind === "interval") native.clearInterval(handle);
      else if (kind === "animation") native.cancelAnimationFrame(handle);
      else native.clearTimeout(handle);
    }
    timerHandles.clear();
    for (const [target, type, listener, options] of listeners.splice(0)) {
      native.removeEventListener.call(target, type, listener, options);
    }
    document.body.replaceChildren();
    document.querySelectorAll("head style, head link[rel=stylesheet]").forEach(node => node.remove());
    await clearOriginStorage();
    performance.clearMarks();
    performance.clearMeasures();
    performance.clearResourceTimings();
    if (baselineGlobals) {
      for (const key of Reflect.ownKeys(globalThis)) {
        if (!baselineGlobals.has(key)) {
          try { delete globalThis[key]; } catch (_) {}
        }
      }
    }
    intentionalWait = 0;
    sessionTiming = {wall: 0, runner: 0, application: 0, network: 0, wait: 0};
    sessionBatch = null;
    nativeNetwork = 0;
  }

  async function sessionOperation(operation) {
    if (!sessionBatch) {
      sessionBatch = {active: 0, started: performance.now(), runner: 0, application: 0, network: 0, wait: 0};
    }
    const batch = sessionBatch;
    batch.active++;
    try {
      const result = await operation();
      const timing = result.timing || {};
      // Concurrent clients contribute one wall-clock batch rather than
      // multiplying host overhead by the number of clients.
      batch.runner = Math.max(batch.runner, Number(timing.runner_ms) || 0);
      batch.application = Math.max(batch.application, Number(timing.application_ms) || 0);
      batch.network += Number(timing.network_ms) || 0;
      batch.wait = Math.max(batch.wait, Number(timing.wait_ms) || 0);
      return result;
    } finally {
      batch.active--;
      if (batch.active === 0) {
        sessionTiming.wall += performance.now() - batch.started;
        sessionTiming.runner += batch.runner;
        sessionTiming.application += batch.application;
        sessionTiming.network += batch.network;
        sessionTiming.wait += batch.wait;
        if (sessionBatch === batch) sessionBatch = null;
      }
    }
  }

  async function createSession(names) {
    const leases = await window.__rushCreateSession(names);
    return leases.map(lease => {
      let currentURL = "about:blank";
      return {
        name: lease.name,
        url: () => currentURL,
        async goto(url) {
          const result = await sessionOperation(() => window.__rushSessionGoto(lease.id, url));
          currentURL = result.url || url;
        },
        async evaluate(callback) {
          if (typeof callback !== "function") throw new TypeError("session client evaluate requires a callback");
          const result = await sessionOperation(() => window.__rushSessionEvaluate(lease.id, callback.toString()));
          currentURL = result.url || currentURL;
          return result.value;
        },
        async dispose() { await window.__rushDisposeSession(lease.id); },
      };
    });
  }

  function bundleFactory(hash, source, file) {
    let factory = compiledBundles.get(hash);
    if (factory) return factory;
    if (!source) throw new Error("compiled bundle cache miss for " + hash);
    const sourceURL = "rush-test://suite/" + encodeURIComponent(file).replaceAll("%2F", "/");
    factory = new Function("//# sourceURL=" + sourceURL + "\n" + source);
    if (compiledBundles.size >= bundleCacheLimit) {
      compiledBundles.delete(compiledBundles.keys().next().value);
    }
    compiledBundles.set(hash, factory);
    return factory;
  }

  async function executeSuite(bundle) {
    const resetStart = performance.now();
    await clearState();
    const reset = performance.now() - resetStart;
    const suiteStart = performance.now();
    let results = [];
    let callbackWall = 0;
    let compile = 0;
    collecting = true;
    try {
      const sharedAPI = globalThis.__rushBrowserRuntime;
      if (!sharedAPI || typeof sharedAPI.run !== "function") {
        throw new Error("Rush browser runtime did not load");
      }
      sharedAPI.resetRegistry();
      sharedAPI.resetMockRuntime();
      sharedAPI.configureSnapshots();
      const compileStart = performance.now();
      const factory = bundleFactory(bundle.hash, bundle.source, bundle.file);
      compile = performance.now() - compileStart;
      factory();
      if (globalThis.__rushRegistration) await globalThis.__rushRegistration;
      const api = globalThis.__rushBrowserModule || sharedAPI;
      api.configureRuntime({createApp: createAppRealm, createSession});
      const runResult = await api.run({emit: false});
      results = runResult.tests.map(item => ({
        name: item.fullName,
        status: item.state,
        duration_ms: item.durationMs,
        ...(item.error ? {error: formatError(item.error)} : {}),
      }));
      callbackWall = runResult.tests.reduce((sum, item) => sum + item.durationMs, 0);
    } catch (error) {
      results.push({name: "suite registration", status: "failed", duration_ms: 0, error: formatError(error)});
    }
    collecting = false;
    const total = performance.now() - suiteStart;
    const coordinatorNetwork = nativeNetwork + performance.getEntriesByType("resource").reduce((sum, entry) => sum + entry.duration, 0);
    const network = coordinatorNetwork + sessionTiming.network;
    const waits = intentionalWait + sessionTiming.wait;
    const coordinatorApplication = Math.max(0, callbackWall - sessionTiming.wall - intentionalWait - coordinatorNetwork);
    const application = coordinatorApplication + sessionTiming.application;
    const runner = Math.max(0, total - callbackWall) + sessionTiming.runner;
    return {
      file: bundle.file,
      tests: results,
      timing: {
        build_ms: 0,
        compile_ms: compile,
        reset_ms: reset,
        runner_ms: runner,
        application_ms: application,
        network_ms: network,
        wait_ms: waits,
        total_ms: total,
      },
    };
  }

  async function executeBatch(id, bundles) {
    const batchStart = performance.now();
    const suites = [];
    for (const bundle of bundles) suites.push(await executeSuite(bundle));
    const browserMS = performance.now() - batchStart;
    const compiledHashes = bundles.filter(bundle => compiledBundles.has(bundle.hash)).map(bundle => bundle.hash);
    const reportingStart = performance.now();
    const suitesJSON = JSON.stringify(suites);
    const reporting = performance.now() - reportingStart;
    await window.__rushReport('{"id":' + JSON.stringify(id) + ',"suites":' + suitesJSON + ',"compiled_hashes":' + JSON.stringify(compiledHashes) + ',"browser_ms":' + browserMS + ',"reporting_ms":' + reporting + '}');
  }

  async function execute(id, filename, source) {
    await executeBatch(id, [{file: filename, source, hash: "legacy-" + source.length}]);
  }

  function networkComplete(_realm, duration) {
    if (collecting) nativeNetwork += Math.max(0, Number(duration) || 0);
  }

  window.__rush = {execute, executeBatch, handleRequest, networkComplete};
  baselineGlobals = new Set(Reflect.ownKeys(globalThis));
  window.__rushReady();
})();
</script></body></html>`
