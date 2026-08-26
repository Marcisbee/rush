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
  network: AppNetwork;
  url(): string;
  goto(url: string): Promise<void>;
}

export interface AppRequest {
  readonly id: string;
  readonly url: string;
  readonly method: string;
  readonly headers: Readonly<Record<string, string>>;
  readonly body?: string;
}

export interface AppResponse {
  status?: number;
  headers?: Readonly<Record<string, string>>;
  body?: string;
}

export interface AppRequestOverrides {
  url?: string;
  method?: string;
  headers?: Readonly<Record<string, string>>;
  body?: string;
}

export interface AppRoute {
  readonly request: AppRequest;
  fulfill(response?: AppResponse): void;
  continue(overrides?: AppRequestOverrides): void;
  abort(reason?: string): void;
}

export type AppRequestPattern = string | RegExp | ((request: AppRequest) => boolean);
export type AppRouteHandler = (route: AppRoute) => Awaitable<void>;

export interface AppNetwork {
  route(pattern: AppRequestPattern, handler: AppRouteHandler): () => void;
  requests(pattern?: AppRequestPattern): readonly AppRequest[];
  waitForRequest(pattern: AppRequestPattern, options?: { timeout?: number }): Promise<AppRequest>;
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
  /** Present when the adapter exposes the client DOM in this JavaScript realm. */
  root?: ParentNode;
  url(): string;
  goto(url: string): Promise<void>;
  evaluate<T>(callback: () => Awaitable<T>): Promise<T>;
  dispose?(): Awaitable<void>;
}

export interface AppRealm {
  window(): Window;
  document(): Document;
  url(): string;
  goto(url: string): Promise<void>;
  network: AppNetwork;
  native?: NativeAutomation;
  dispose?(): Awaitable<void>;
}

export interface RuntimeAdapter {
  createApp?(): Awaitable<AppRealm>;
  /** @deprecated Use createApp so every test receives an isolated application realm. */
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
  skipReason?: string;
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
