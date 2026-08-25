package rush

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
  let collecting = false;
  let intentionalWait = 0;
  let baselineGlobals;

  function formatError(error) {
    if (!error) return String(error);
    const message = (error.name || "Error") + (error.message ? ": " + error.message : "");
    const stack = String(error.stack || "");
    return stack.startsWith(message) ? stack : message + (stack ? "\n" + stack : "");
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

  function clearState() {
    collecting = false;
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
    try { localStorage.clear(); } catch (_) {}
    try { sessionStorage.clear(); } catch (_) {}
    try { document.cookie.split(";").forEach(cookie => { document.cookie = cookie.split("=")[0].trim() + "=;expires=Thu, 01 Jan 1970 00:00:00 GMT;path=/"; }); } catch (_) {}
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
  }

  function bundleFactory(hash, source) {
    let factory = compiledBundles.get(hash);
    if (factory) return factory;
    if (!source) throw new Error("compiled bundle cache miss for " + hash);
    factory = new Function(source);
    if (compiledBundles.size >= bundleCacheLimit) {
      compiledBundles.delete(compiledBundles.keys().next().value);
    }
    compiledBundles.set(hash, factory);
    return factory;
  }

  async function executeSuite(bundle) {
    const resetStart = performance.now();
    clearState();
    const reset = performance.now() - resetStart;
    const suiteStart = performance.now();
    let results = [];
    let callbackWall = 0;
    let compile = 0;
    collecting = true;
    try {
      const compileStart = performance.now();
      const factory = bundleFactory(bundle.hash, bundle.source);
      compile = performance.now() - compileStart;
      factory();
      if (globalThis.__rushRegistration) await globalThis.__rushRegistration;
      const api = globalThis.__rushBrowserModule;
      if (!api || typeof api.run !== "function") {
        throw new Error("suite bundle did not expose the @rush/browser runtime");
      }
      api.configureRuntime({});
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
    const network = performance.getEntriesByType("resource").reduce((sum, entry) => sum + entry.duration, 0);
    const application = Math.max(0, callbackWall - intentionalWait - network);
    const runner = Math.max(0, total - callbackWall);
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
        wait_ms: intentionalWait,
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

  window.__rush = {execute, executeBatch};
  baselineGlobals = new Set(Reflect.ownKeys(globalThis));
  window.__rushReady();
})();
</script></body></html>`
