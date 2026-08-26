import FakeTimers, { type Clock, type Config, type FakeMethod } from "@sinonjs/fake-timers";
import type { Awaitable } from "./types.js";

export interface MockResult<T = unknown> {
  type: "return" | "throw";
  value: T;
}

export interface MockState<TArgs extends unknown[] = unknown[], TReturn = unknown> {
  calls: TArgs[];
  contexts: unknown[];
  instances: unknown[];
  invocationCallOrder: number[];
  results: MockResult<TReturn>[];
  lastCall?: TArgs;
}

export interface MockFunction<TArgs extends unknown[] = unknown[], TReturn = unknown> {
  (...args: TArgs): TReturn;
  readonly _isMockFunction: true;
  readonly mock: MockState<TArgs, TReturn>;
  getMockImplementation(): ((...args: TArgs) => TReturn) | undefined;
  getMockName(): string;
  mockName(name: string): this;
  mockClear(): this;
  mockReset(): this;
  mockRestore(): this;
  mockImplementation(implementation: (...args: TArgs) => TReturn): this;
  mockImplementationOnce(implementation: (...args: TArgs) => TReturn): this;
  mockReturnValue(value: TReturn): this;
  mockReturnValueOnce(value: TReturn): this;
  mockResolvedValue(value: Awaited<TReturn>): this;
  mockResolvedValueOnce(value: Awaited<TReturn>): this;
  mockRejectedValue(value: unknown): this;
  mockRejectedValueOnce(value: unknown): this;
}

interface MockControl {
  clear(): void;
  reset(): void;
  restore(): void;
}

const controls = new Set<MockControl>();
let callOrder = 0;

export function fn<TArgs extends unknown[] = unknown[], TReturn = unknown>(implementation?: (...args: TArgs) => TReturn): MockFunction<TArgs, TReturn> {
  let currentImplementation = implementation;
  const initialImplementation = implementation;
  const once: Array<(...args: TArgs) => TReturn> = [];
  let name = "vi.fn()";
  let restore = (): void => {};
  let state: MockState<TArgs, TReturn> = createState();

  const callable = function (this: unknown, ...args: TArgs): TReturn {
    state.calls.push(args);
    state.contexts.push(this);
    state.instances.push(this);
    state.invocationCallOrder.push(++callOrder);
    state.lastCall = args;
    const selected = once.shift() ?? currentImplementation;
    try {
      const value = selected?.apply(this, args) as TReturn;
      state.results.push({ type: "return", value });
      return value;
    } catch (error) {
      state.results.push({ type: "throw", value: error as TReturn });
      throw error;
    }
  } as MockFunction<TArgs, TReturn>;

  const control: MockControl = {
    clear() { state = createState(); },
    reset() { state = createState(); once.length = 0; currentImplementation = undefined; },
    restore() { restore(); controls.delete(control); },
  };
  controls.add(control);

  Object.defineProperties(callable, {
    _isMockFunction: { value: true },
    mock: { get: () => state },
  });
  callable.getMockImplementation = () => currentImplementation;
  callable.getMockName = () => name;
  callable.mockName = (value) => { name = value; return callable; };
  callable.mockClear = () => { control.clear(); return callable; };
  callable.mockReset = () => { control.reset(); return callable; };
  callable.mockRestore = () => { control.restore(); return callable; };
  callable.mockImplementation = (value) => { currentImplementation = value; return callable; };
  callable.mockImplementationOnce = (value) => { once.push(value); return callable; };
  callable.mockReturnValue = (value) => callable.mockImplementation(() => value);
  callable.mockReturnValueOnce = (value) => callable.mockImplementationOnce(() => value);
  callable.mockResolvedValue = (value) => callable.mockImplementation(() => Promise.resolve(value) as TReturn);
  callable.mockResolvedValueOnce = (value) => callable.mockImplementationOnce(() => Promise.resolve(value) as TReturn);
  callable.mockRejectedValue = (value) => callable.mockImplementation(() => Promise.reject(value) as TReturn);
  callable.mockRejectedValueOnce = (value) => callable.mockImplementationOnce(() => Promise.reject(value) as TReturn);

  Object.defineProperty(control, "restore", {
    value: () => { restore(); controls.delete(control); },
  });
  Object.defineProperty(callable, "__setRestore", {
    value: (callback: () => void) => { restore = callback; },
  });
  Object.defineProperty(callable, "__initialImplementation", { value: initialImplementation });
  return callable;
}

function createState<TArgs extends unknown[], TReturn>(): MockState<TArgs, TReturn> {
  return { calls: [], contexts: [], instances: [], invocationCallOrder: [], results: [] };
}

export function spyOn<T extends object, K extends keyof T>(target: T, property: K, accessType?: "get" | "set"): MockFunction {
  const descriptor = findDescriptor(target, property);
  if (!descriptor) throw new Error(`Cannot spy on ${String(property)} because it does not exist`);
  if (descriptor.configurable === false) throw new Error(`Cannot spy on non-configurable property ${String(property)}`);

  const original = Object.getOwnPropertyDescriptor(target, property);
  const restore = (): void => {
    if (original) Object.defineProperty(target, property, original);
    else delete target[property];
  };

  if (accessType) {
    const implementation = descriptor[accessType];
    if (!implementation) throw new Error(`${String(property)} does not have a ${accessType} accessor`);
    const spy = fn(implementation.bind(target) as (...args: unknown[]) => unknown);
    (spy as unknown as { __setRestore(callback: () => void): void }).__setRestore(restore);
    Object.defineProperty(target, property, { ...descriptor, [accessType]: spy });
    return spy;
  }

  if (typeof descriptor.value !== "function") throw new Error(`${String(property)} is not a function`);
  const spy = fn(descriptor.value as (...args: unknown[]) => unknown);
  (spy as unknown as { __setRestore(callback: () => void): void }).__setRestore(restore);
  Object.defineProperty(target, property, { ...descriptor, value: spy });
  return spy;
}

function findDescriptor(target: object, property: PropertyKey): PropertyDescriptor | undefined {
  let cursor: object | null = target;
  while (cursor) {
    const descriptor = Object.getOwnPropertyDescriptor(cursor, property);
    if (descriptor) return descriptor;
    cursor = Object.getPrototypeOf(cursor) as object | null;
  }
  return undefined;
}

let clock: Clock | undefined;

export interface WaitForOptions {
  timeout?: number;
  interval?: number;
}

export async function waitFor<T>(callback: () => Awaitable<T>, options: number | WaitForOptions = {}): Promise<T> {
  const resolved = typeof options === "number" ? { timeout: options } : options;
  const timeout = resolved.timeout ?? 1_000;
  const interval = resolved.interval ?? 50;
  let elapsed = 0;
  let lastError: unknown;

  for (;;) {
    try {
      return await callback();
    } catch (error) {
      lastError = error;
    }

    if (elapsed >= timeout) throw lastError;
    const delay = Math.min(interval, timeout - elapsed);
    if (clock) await clock.tickAsync(delay);
    else await new Promise<void>((resolve) => setTimeout(resolve, delay));
    elapsed += delay;
  }
}

export function useFakeTimers(options: Config = {}): Clock {
  if (clock) clock.uninstall();
  const timers = FakeTimers.withGlobal(globalThis);
  const toFake = options.toFake ?? (Object.keys(timers.timers) as FakeMethod[])
    .filter((method) => method !== "nextTick" && method !== "queueMicrotask");
  clock = timers.install({ ...options, toFake });
  return clock;
}

export function useRealTimers(): void {
  clock?.uninstall();
  clock = undefined;
}

function requireClock(): Clock {
  if (!clock) throw new Error("Fake timers are not installed. Call vi.useFakeTimers() first.");
  return clock;
}

type ImportOriginal = <T extends Record<string, unknown> = Record<string, unknown>>() => Promise<T>;
type MockFactory = ((importOriginal: ImportOriginal) => Awaitable<Record<string, unknown>>) | Record<string, unknown> | undefined;
interface ModuleMock {
  factory: MockFactory;
  loaded?: Promise<Record<string, unknown>>;
}
const moduleMocks = new Map<string, ModuleMock>();

export function registerMock(id: string, factory?: MockFactory): void {
  moduleMocks.set(id, { factory });
}

export function unmock(id: string): void {
  moduleMocks.delete(id);
}

export async function importWithMocks<T extends Record<string, unknown>>(id: string, importer: () => Promise<T>): Promise<T> {
  if (!moduleMocks.has(id)) return importer();
  const registration = moduleMocks.get(id) as ModuleMock;
  registration.loaded ??= (async () => {
    if (registration.factory === undefined) {
      const actual = await importer();
      return Object.fromEntries(Object.entries(actual).map(([key, value]) => [key, typeof value === "function" ? fn() : value]));
    }
    const importOriginal = (() => importer()) as unknown as ImportOriginal;
    return typeof registration.factory === "function" ? registration.factory(importOriginal) : registration.factory;
  })();
  return await registration.loaded as T;
}

export async function importActual<T>(importer: () => Promise<T>): Promise<T> {
  return importer();
}

export function resetModules(): void {
  moduleMocks.clear();
}

export const vi = {
  fn,
  waitFor,
  spyOn,
  isMockFunction(value: unknown): value is MockFunction { return typeof value === "function" && (value as Partial<MockFunction>)._isMockFunction === true; },
  mocked<T>(value: T): T { return value; },
  clearAllMocks(): void { for (const control of controls) control.clear(); },
  resetAllMocks(): void { for (const control of controls) control.reset(); },
  restoreAllMocks(): void { for (const control of [...controls]) control.restore(); },
  useFakeTimers,
  useRealTimers,
  isFakeTimers(): boolean { return clock !== undefined; },
  setSystemTime(value: number | string | Date): void { requireClock().setSystemTime(new Date(value).getTime()); },
  getMockedSystemTime(): Date | null { return clock ? new Date(clock.now) : null; },
  advanceTimersByTime(milliseconds: number): void { requireClock().tick(milliseconds); },
  advanceTimersByTimeAsync(milliseconds: number): Promise<number> { return requireClock().tickAsync(milliseconds); },
  advanceTimersToNextTimer(steps?: number): void { requireClock().next(); if (steps && steps > 1) for (let index = 1; index < steps; index += 1) requireClock().next(); },
  runAllTimers(): void { requireClock().runAll(); },
  runAllTimersAsync(): Promise<number> { return requireClock().runAllAsync(); },
  runOnlyPendingTimers(): void { requireClock().runToLast(); },
  clearAllTimers(): void { requireClock().reset(); },
  getTimerCount(): number { return requireClock().countTimers(); },
  mock: registerMock,
  doMock: registerMock,
  unmock,
  doUnmock: unmock,
  resetModules,
  importActual,
  importMock<T extends Record<string, unknown>>(id: string, importer: () => Promise<T>): Promise<T> { return importWithMocks(id, importer); },
  hoisted<T>(factory: () => T): T { return factory(); },
};

export function resetMockRuntime(): void {
  vi.restoreAllMocks();
  useRealTimers();
  resetModules();
}

export { registerMock as __rushRegisterMock__, importWithMocks as __rushImport__ };
