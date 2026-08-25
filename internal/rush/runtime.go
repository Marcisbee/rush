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
  let collecting = false;
  let intentionalWait = 0;
  let prefix = [];
  let tests = [];
  let beforeEachHooks = [];
  let afterEachHooks = [];
  let baselineGlobals;

  function fullName(name) { return [...prefix, name].join(" > "); }
  function register(name, fn, mode = "run") { tests.push({name: fullName(name), fn, mode}); }
  function test(name, fn) { register(name, fn); }
  test.skip = (name, fn) => register(name, fn, "skip");
  test.todo = name => register(name, null, "todo");
  test.only = (name, fn) => register(name, fn, "only");
  test.each = values => (name, fn) => values.forEach((value, index) => {
    const args = Array.isArray(value) ? [...value] : [value];
    register(name.replace(/%[sdifjo]/g, () => String(args.shift())), () => fn(...(Array.isArray(value) ? value : [value])));
  });
  function describe(name, fn) { prefix.push(name); try { fn(); } finally { prefix.pop(); } }
  describe.skip = (name, fn) => { prefix.push(name); const start = tests.length; try { fn(); } finally { prefix.pop(); } for (let i = start; i < tests.length; i++) tests[i].mode = "skip"; };
  describe.only = (name, fn) => { prefix.push(name); const start = tests.length; try { fn(); } finally { prefix.pop(); } for (let i = start; i < tests.length; i++) tests[i].mode = "only"; };

  function serialize(value) {
    if (typeof value === "string") return JSON.stringify(value);
    try { return JSON.stringify(value); } catch (_) { return String(value); }
  }
  function formatError(error) {
    if (!error) return String(error);
    const message = (error.name || "Error") + (error.message ? ": " + error.message : "");
    const stack = String(error.stack || "");
    return stack.startsWith(message) ? stack : message + (stack ? "\n" + stack : "");
  }
  function deepEqual(a, b) {
    if (Object.is(a, b)) return true;
    if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
    const ak = Reflect.ownKeys(a), bk = Reflect.ownKeys(b);
    return ak.length === bk.length && ak.every(k => bk.includes(k) && deepEqual(a[k], b[k]));
  }
  function expect(actual) {
    const match = (pass, message, inverted = false) => {
      if (inverted ? pass : !pass) throw new Error(message);
    };
    const make = inverted => ({
      toBe(expected) { match(Object.is(actual, expected), "expected " + serialize(actual) + " to be " + serialize(expected), inverted); },
      toEqual(expected) { match(deepEqual(actual, expected), "expected " + serialize(actual) + " to equal " + serialize(expected), inverted); },
      toBeTruthy() { match(Boolean(actual), "expected " + serialize(actual) + " to be truthy", inverted); },
      toBeFalsy() { match(!actual, "expected " + serialize(actual) + " to be falsy", inverted); },
      toContain(expected) { match(actual != null && typeof actual.includes === "function" && actual.includes(expected), "expected value to contain " + serialize(expected), inverted); },
      toBeNull() { match(actual === null, "expected " + serialize(actual) + " to be null", inverted); },
      toBeDefined() { match(actual !== undefined, "expected value to be defined", inverted); },
      toThrow(expected) {
        let thrown = null;
        try { actual(); } catch (error) { thrown = error; }
        let pass = Boolean(thrown);
        if (pass && expected instanceof RegExp) pass = expected.test(String(thrown && thrown.message || thrown));
        if (pass && typeof expected === "string") pass = String(thrown && thrown.message || thrown).includes(expected);
        match(pass, "expected function to throw", inverted);
      },
      get not() { return make(!inverted); },
    });
    return make(false);
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
    tests = [];
    beforeEachHooks = [];
    afterEachHooks = [];
    prefix = [];
    intentionalWait = 0;
  }

  Object.assign(globalThis, {
    test,
    it: test,
    describe,
    expect,
    beforeEach: fn => beforeEachHooks.push(fn),
    afterEach: fn => afterEachHooks.push(fn),
  });

  async function execute(id, filename, source) {
    clearState();
    const suiteStart = performance.now();
    const results = [];
    let callbackWall = 0;
    try {
      (0, eval)(source + "\n//# sourceURL=" + filename);
    } catch (error) {
      results.push({name: "suite registration", status: "failed", duration_ms: 0, error: formatError(error)});
    }
    const hasOnly = tests.some(item => item.mode === "only");
    collecting = true;
    for (const item of tests) {
      if (item.mode === "todo" || item.mode === "skip" || (hasOnly && item.mode !== "only")) {
        results.push({name: item.name, status: item.mode === "todo" ? "todo" : "skipped", duration_ms: 0});
        continue;
      }
      const started = performance.now();
      let failure = "";
      try {
        for (const hook of beforeEachHooks) await hook();
        await item.fn();
      } catch (error) {
        failure = formatError(error);
      } finally {
        for (const hook of afterEachHooks) {
          try { await hook(); } catch (error) { if (!failure) failure = formatError(error); }
        }
      }
      const duration = performance.now() - started;
      callbackWall += duration;
      results.push({name: item.name, status: failure ? "failed" : "passed", duration_ms: duration, ...(failure ? {error: failure} : {})});
    }
    collecting = false;
    const total = performance.now() - suiteStart;
    const network = performance.getEntriesByType("resource").reduce((sum, entry) => sum + entry.duration, 0);
    const application = Math.max(0, callbackWall - intentionalWait - network);
    const runner = Math.max(0, total - callbackWall);
    const payload = {
      id,
      file: filename,
      tests: results,
      timing: {
        build_ms: 0,
        runner_ms: runner,
        application_ms: application,
        network_ms: network,
        wait_ms: intentionalWait,
        total_ms: total,
      },
    };
    await window.__rushReport(JSON.stringify(payload));
  }

  window.__rush = {execute};
  baselineGlobals = new Set(Reflect.ownKeys(globalThis));
  window.__rushReady();
})();
</script></body></html>`
