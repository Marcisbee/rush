import { createPage, setNativeAutomation } from "./locator.js";
import { enterSnapshotTest, leaveSnapshotTest } from "./snapshots.js";
import { useRealTimers } from "./mock.js";
import type {
  AppContext,
  Awaitable,
  BrowserContext,
  HookCallback,
  RunResult,
  RuntimeAdapter,
  SerializedError,
  SessionContext,
  SessionOptions,
  TestCallback,
  TestContext,
  TestMode,
  TestModel,
  TestResult,
} from "./types.js";

interface TestDefinition {
  kind: "test";
  id: string;
  name: string;
  mode: TestMode;
  model: TestModel;
  callback?: TestCallback;
  sessionOptions?: SessionOptions;
  parent: SuiteDefinition;
}

interface SuiteDefinition {
  kind: "suite";
  id: string;
  name: string;
  mode: Exclude<TestMode, "todo">;
  parent?: SuiteDefinition;
  entries: Array<SuiteDefinition | TestDefinition>;
  hooks: {
    beforeAll: HookCallback[];
    afterAll: HookCallback[];
    beforeEach: HookCallback[];
    afterEach: HookCallback[];
  };
}

export interface TestAPI<TContext extends TestContext = TestContext> {
  (name: string, callback?: TestCallback<TContext>): void;
  skip: TestAPI<TContext>;
  only: TestAPI<TContext>;
  todo(name: string): void;
  each<T extends readonly unknown[]>(cases: readonly T[]): (name: string, callback: (...values: [...T]) => Awaitable<void>) => void;
  browser: TestAPI<BrowserContext>;
  app: TestAPI<AppContext>;
  session: SessionTestAPI;
}

export interface SessionTestAPI extends TestAPI<SessionContext> {
  (options: SessionOptions): TestAPI<SessionContext>;
}

export interface DescribeAPI {
  (name: string, callback: () => void): void;
  skip: DescribeAPI;
  only: DescribeAPI;
  each<T extends readonly unknown[]>(cases: readonly T[]): (name: string, callback: (...values: [...T]) => void) => void;
}

let sequence = 0;
let root = createRoot();
let currentSuite = root;
let runtimeAdapter: RuntimeAdapter = {};

function createRoot(): SuiteDefinition {
  return {
    kind: "suite",
    id: "root",
    name: "",
    mode: "run",
    entries: [],
    hooks: { beforeAll: [], afterAll: [], beforeEach: [], afterEach: [] },
  };
}

function registerTest(mode: TestMode, model: TestModel, sessionOptions: SessionOptions | undefined, name: string, callback?: TestCallback): void {
  const definition: TestDefinition = {
    kind: "test",
    id: `test-${++sequence}`,
    name,
    mode: callback ? mode : "todo",
    model,
    parent: currentSuite,
  };
  if (callback) definition.callback = callback;
  if (sessionOptions) definition.sessionOptions = sessionOptions;
  currentSuite.entries.push(definition);
}

function makeTest(mode: TestMode = "run", model: TestModel = "browser", sessionOptions?: SessionOptions): TestAPI {
  const callable = ((name: string, callback?: TestCallback) => registerTest(mode, model, sessionOptions, name, callback)) as TestAPI;
  callable.todo = (name) => registerTest("todo", model, sessionOptions, name);
  callable.each = (cases) => (name, callback) => {
    cases.forEach((values, index) => registerTest(mode, model, sessionOptions, formatEachName(name, values, index), () => callback(...values)));
  };
  const session = ((nameOrOptions: string | SessionOptions, callback?: TestCallback) => {
    if (typeof nameOrOptions === "object") return makeTest(mode, "session", nameOrOptions);
    return registerTest(mode, "session", sessionOptions, nameOrOptions, callback);
  }) as SessionTestAPI;
  session.todo = (name) => registerTest("todo", "session", sessionOptions, name);
  session.each = (cases) => (name, callback) => cases.forEach((values, index) => registerTest(mode, "session", sessionOptions, formatEachName(name, values, index), () => callback(...values)));
  Object.defineProperties(session, {
    skip: { get: () => mode === "skip" ? session : makeTest("skip", "session", sessionOptions).session },
    only: { get: () => mode === "only" ? session : makeTest("only", "session", sessionOptions).session },
    browser: { get: () => makeTest(mode, "browser") },
    app: { get: () => makeTest(mode, "app") },
    session: { get: () => session },
  });
  Object.defineProperties(callable, {
    skip: { get: () => mode === "skip" ? callable : makeTest("skip", model, sessionOptions) },
    only: { get: () => mode === "only" ? callable : makeTest("only", model, sessionOptions) },
    browser: { get: () => model === "browser" ? callable : makeTest(mode, "browser") },
    app: { get: () => model === "app" ? callable : makeTest(mode, "app") },
    session: { get: () => session },
  });
  return callable;
}

function makeDescribe(mode: Exclude<TestMode, "todo"> = "run"): DescribeAPI {
  const callable = ((name: string, callback: () => void) => {
    const parent = currentSuite;
    const suite: SuiteDefinition = {
      kind: "suite",
      id: `suite-${++sequence}`,
      name,
      mode,
      parent,
      entries: [],
      hooks: { beforeAll: [], afterAll: [], beforeEach: [], afterEach: [] },
    };
    parent.entries.push(suite);
    currentSuite = suite;
    try { callback(); } finally { currentSuite = parent; }
  }) as DescribeAPI;
  callable.each = (cases) => (name, callback) => cases.forEach((values, index) => callable(formatEachName(name, values, index), () => callback(...values)));
  Object.defineProperties(callable, {
    skip: { get: () => mode === "skip" ? callable : makeDescribe("skip") },
    only: { get: () => mode === "only" ? callable : makeDescribe("only") },
  });
  return callable;
}

function formatEachName(name: string, values: readonly unknown[], index: number): string {
  let cursor = 0;
  const formatted = name.replace(/%[sidjo]/g, () => String(values[cursor++]));
  return formatted === name ? `${name} ${index}` : formatted;
}

export const test = makeTest();
export const it = test;
export const describe = makeDescribe();
export const suite = describe;

export function beforeAll(callback: HookCallback): void { currentSuite.hooks.beforeAll.push(callback); }
export function afterAll(callback: HookCallback): void { currentSuite.hooks.afterAll.push(callback); }
export function beforeEach(callback: HookCallback): void { currentSuite.hooks.beforeEach.push(callback); }
export function afterEach(callback: HookCallback): void { currentSuite.hooks.afterEach.push(callback); }

export function configureRuntime(adapter: RuntimeAdapter): void {
  runtimeAdapter = adapter;
  setNativeAutomation(adapter.native);
}

export function resetRegistry(): void {
  sequence = 0;
  root = createRoot();
  currentSuite = root;
}

export async function run(options: { emit?: boolean } = {}): Promise<RunResult> {
  const started = performance.now();
  const results: TestResult[] = [];
  const hasOnly = containsOnly(root, false);
  await runSuite(root, [], results, hasOnly, false, false);
  if (options.emit !== false) await runtimeAdapter.emitResults?.(results);
  return {
    tests: results,
    passed: results.filter((result) => result.state === "passed").length,
    failed: results.filter((result) => result.state === "failed").length,
    skipped: results.filter((result) => result.state === "skipped").length,
    todo: results.filter((result) => result.state === "todo").length,
    durationMs: performance.now() - started,
  };
}

function containsOnly(suiteDefinition: SuiteDefinition, inheritedOnly: boolean): boolean {
  if (suiteDefinition.mode === "skip") return false;
  const suiteOnly = inheritedOnly || suiteDefinition.mode === "only";
  return suiteDefinition.entries.some((entry) => entry.kind === "test" ? suiteOnly || entry.mode === "only" : containsOnly(entry, suiteOnly));
}

async function runSuite(
  suiteDefinition: SuiteDefinition,
  ancestors: SuiteDefinition[],
  results: TestResult[],
  hasOnly: boolean,
  inheritedOnly: boolean,
  inheritedSkip: boolean,
): Promise<void> {
  const selectedBySuite = inheritedOnly || suiteDefinition.mode === "only";
  const skippedBySuite = inheritedSkip || suiteDefinition.mode === "skip";
  const browserContext = makeBrowserContext();
  const executeHooks = !skippedBySuite && (!hasOnly || selectedBySuite || containsOnly(suiteDefinition, false));
  if (executeHooks) for (const hook of suiteDefinition.hooks.beforeAll) await hook(browserContext);

  const lineage = [...ancestors, suiteDefinition];
  for (const entry of suiteDefinition.entries) {
    if (entry.kind === "suite") {
      await runSuite(entry, lineage, results, hasOnly, selectedBySuite, skippedBySuite);
      continue;
    }
    await runTest(entry, lineage, results, hasOnly, selectedBySuite, skippedBySuite);
  }

  if (executeHooks) for (const hook of [...suiteDefinition.hooks.afterAll].reverse()) await hook(browserContext);
}

async function runTest(
  definition: TestDefinition,
  lineage: SuiteDefinition[],
  results: TestResult[],
  hasOnly: boolean,
  selectedBySuite: boolean,
  skippedBySuite: boolean,
): Promise<void> {
  const fullName = [...lineage.map((item) => item.name).filter(Boolean), definition.name].join(" > ");
  const base = { id: definition.id, name: definition.name, fullName, model: definition.model, durationMs: 0 } as const;
  if (definition.mode === "todo") { results.push({ ...base, state: "todo" }); return; }
  if (skippedBySuite || definition.mode === "skip" || (hasOnly && !selectedBySuite && definition.mode !== "only")) {
    results.push({ ...base, state: "skipped" }); return;
  }

  const started = performance.now();
  let context: TestContext | undefined;
  let failure: unknown;
  enterSnapshotTest(fullName);
  try {
    context = await createContext(definition);
    for (const suiteDefinition of lineage) for (const hook of suiteDefinition.hooks.beforeEach) await hook(context);
    await definition.callback?.(context);
  } catch (error) {
    failure = error;
  } finally {
    if (context) {
      for (const suiteDefinition of [...lineage].reverse()) {
        for (const hook of [...suiteDefinition.hooks.afterEach].reverse()) {
          try { await hook(context); } catch (error) { failure ??= error; }
        }
      }
      await disposeContext(context);
    }
    leaveSnapshotTest();
    useRealTimers();
  }

  const durationMs = performance.now() - started;
  results.push(failure === undefined
    ? { ...base, durationMs, state: "passed" }
    : { ...base, durationMs, state: "failed", error: serializeError(failure) });
}

function makeBrowserContext(): BrowserContext {
  return { model: "browser", window, document, page: createPage(document) };
}

async function createContext(definition: TestDefinition): Promise<TestContext> {
  if (definition.model === "browser") return makeBrowserContext();
  if (definition.model === "app") {
    if (runtimeAdapter.createApp) {
      const realm = await runtimeAdapter.createApp();
      const context = {
        model: "app",
        page: createPage(realm.document),
        network: realm.network,
        url: realm.url,
        goto: realm.goto,
        __dispose: realm.dispose,
      } as AppContext & { __dispose?: () => Awaitable<void> };
      Object.defineProperties(context, {
        window: { enumerable: true, get: realm.window },
        document: { enumerable: true, get: realm.document },
      });
      if (realm.native) setNativeAutomation(realm.native);
      return context;
    }
    const context: AppContext = {
      model: "app",
      window,
      document,
      page: createPage(document),
      network: unavailableAppNetwork(),
      url: () => window.location.href,
      goto: async (url) => {
        if (!runtimeAdapter.navigate) throw new Error("App navigation requires a RuntimeAdapter.navigate implementation");
        await runtimeAdapter.navigate(url);
      },
    };
    return context;
  }

  if (!runtimeAdapter.createSession) throw new Error("Session tests require a RuntimeAdapter.createSession implementation");
  const requested = definition.sessionOptions?.clients ?? 2;
  const names = typeof requested === "number" ? Array.from({ length: requested }, (_, index) => `client-${index + 1}`) : [...requested];
  const realms = await runtimeAdapter.createSession(names);
  if (realms.length !== names.length) throw new Error(`Session adapter created ${realms.length} clients; expected ${names.length}`);
  const clients = realms.map((realm) => ({
    name: realm.name,
    // Native session clients live in separate WebViews. Their page code runs in
    // evaluate(), while same-realm adapters can additionally expose a DOM root.
    page: createPage(realm.root ?? document.createDocumentFragment()),
    url: realm.url,
    goto: realm.goto,
    evaluate: realm.evaluate,
    __dispose: realm.dispose,
  }));
  const context: SessionContext = {
    model: "session",
    clients,
    client(nameOrIndex) {
      const client = typeof nameOrIndex === "number" ? clients[nameOrIndex] : clients.find((item) => item.name === nameOrIndex);
      if (!client) throw new Error(`Unknown session client ${String(nameOrIndex)}`);
      return client;
    },
  };
  return context;
}

async function disposeContext(context: TestContext): Promise<void> {
  if (context.model === "app") {
    await (context as AppContext & { __dispose?: () => Awaitable<void> }).__dispose?.();
    setNativeAutomation(runtimeAdapter.native);
    return;
  }
  if (context.model !== "session") return;
  for (const client of context.clients as Array<(typeof context.clients)[number] & { __dispose?: () => Awaitable<void> }>) {
    await client.__dispose?.();
  }
}

function unavailableAppNetwork(): AppContext["network"] {
  const unavailable = () => { throw new Error("Request interception requires a RuntimeAdapter.createApp implementation"); };
  return {
    route: unavailable,
    requests: unavailable,
    waitForRequest: unavailable,
  };
}

function serializeError(error: unknown): SerializedError {
  if (error instanceof Error) {
    const serialized: SerializedError = { name: error.name, message: error.message };
    if (error.stack) serialized.stack = error.stack;
    return serialized;
  }
  return { name: "Error", message: String(error) };
}
