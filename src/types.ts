import type { Locator } from "./locator.js";

export type Awaitable<T> = T | Promise<T>;
export type TestMode = "run" | "skip" | "only" | "todo";
export type TestModel = "browser" | "app" | "session";

export interface BrowserContext {
  model: "browser";
  window: Window;
  document: Document;
  page: Locator;
}

export interface AppContext {
  model: "app";
  window: Window;
  document: Document;
  page: Locator;
  url(): string;
  goto(url: string): Promise<void>;
}

export interface SessionClient {
  name: string;
  page: Locator;
  url(): string;
  goto(url: string): Promise<void>;
  evaluate<T>(callback: () => Awaitable<T>): Promise<T>;
}

export interface SessionContext {
  model: "session";
  clients: readonly SessionClient[];
  client(nameOrIndex: string | number): SessionClient;
}

export type TestContext = BrowserContext | AppContext | SessionContext;
export type TestCallback<TContext extends TestContext = TestContext> = (context: TContext) => Awaitable<void>;
export type HookCallback = (context: TestContext) => Awaitable<void>;

export interface SessionOptions {
  clients?: number | readonly string[];
}

export interface NativeAutomation {
  click(target: Element): Promise<void>;
  type(target: Element, text: string): Promise<void>;
  press(key: string, target?: Element): Promise<void>;
}

export interface SessionRealm {
  name: string;
  root: ParentNode;
  url(): string;
  goto(url: string): Promise<void>;
  evaluate<T>(callback: () => Awaitable<T>): Promise<T>;
  dispose?(): Awaitable<void>;
}

export interface RuntimeAdapter {
  navigate?(url: string): Promise<void>;
  native?: NativeAutomation;
  createSession?(names: readonly string[]): Promise<readonly SessionRealm[]>;
  emitResults?(results: readonly TestResult[]): Awaitable<void>;
}

export interface TestResult {
  id: string;
  name: string;
  fullName: string;
  model: TestModel;
  state: "passed" | "failed" | "skipped" | "todo";
  durationMs: number;
  error?: SerializedError;
}

export interface SerializedError {
  name: string;
  message: string;
  stack?: string;
}

export interface RunResult {
  tests: readonly TestResult[];
  passed: number;
  failed: number;
  skipped: number;
  todo: number;
  durationMs: number;
}
