import { matchSnapshot, serialize } from "./snapshots.js";

const asymmetric = Symbol("rush.asymmetric");

export interface AsymmetricMatcher {
  readonly [asymmetric]: true;
  matches(value: unknown): boolean;
  describe(): string;
}

type PromiseMode = "none" | "resolves" | "rejects";
type MatcherResult = void | Promise<void>;
type Matcher = (received: unknown, ...expected: unknown[]) => boolean | { pass: boolean; message?: string };

const customMatchers = new Map<string, Matcher>();

function isAsymmetric(value: unknown): value is AsymmetricMatcher {
  return typeof value === "object" && value !== null && asymmetric in value;
}

export function equals(received: unknown, expected: unknown, seen = new WeakMap<object, object>()): boolean {
  if (isAsymmetric(expected)) return expected.matches(received);
  if (Object.is(received, expected)) return true;
  if (typeof received !== "object" || received === null || typeof expected !== "object" || expected === null) return false;
  if (Object.getPrototypeOf(received) !== Object.getPrototypeOf(expected)) return false;
  if (seen.get(received) === expected) return true;
  seen.set(received, expected);

  if (received instanceof Date && expected instanceof Date) return received.getTime() === expected.getTime();
  if (received instanceof RegExp && expected instanceof RegExp) return String(received) === String(expected);
  if (received instanceof Map && expected instanceof Map) {
    return received.size === expected.size && [...received].every(([key, value]) => expected.has(key) && equals(value, expected.get(key), seen));
  }
  if (received instanceof Set && expected instanceof Set) {
    return received.size === expected.size && [...received].every((value) => [...expected].some((item) => equals(value, item, seen)));
  }

  const receivedKeys = Reflect.ownKeys(received);
  const expectedKeys = Reflect.ownKeys(expected);
  return receivedKeys.length === expectedKeys.length && receivedKeys.every((key) =>
    expectedKeys.includes(key) && equals((received as Record<PropertyKey, unknown>)[key], (expected as Record<PropertyKey, unknown>)[key], seen),
  );
}

function format(value: unknown): string {
  return serialize(value);
}

class Expectation {
  constructor(
    private readonly received: unknown,
    private readonly inverted = false,
    private readonly promiseMode: PromiseMode = "none",
  ) {}

  get not(): Expectation {
    return new Expectation(this.received, !this.inverted, this.promiseMode);
  }

  get resolves(): Expectation {
    return new Expectation(this.received, this.inverted, "resolves");
  }

  get rejects(): Expectation {
    return new Expectation(this.received, this.inverted, "rejects");
  }

  private apply(name: string, matcher: (received: unknown) => boolean, expected?: unknown): MatcherResult {
    const evaluate = (received: unknown): void => {
      const pass = matcher(received);
      if (pass === this.inverted) {
        const qualifier = this.inverted ? " not" : "";
        throw new Error(`Expected ${format(received)}${qualifier} ${name}${expected === undefined ? "" : ` ${format(expected)}`}`);
      }
    };

    if (this.promiseMode === "none") return evaluate(this.received);
    if (!(this.received instanceof Promise) && (typeof this.received !== "object" || this.received === null || !("then" in this.received))) {
      return Promise.reject(new Error(`Expected a promise for .${this.promiseMode}`));
    }
    const promise = Promise.resolve(this.received);
    if (this.promiseMode === "resolves") {
      return promise.then(evaluate, (error: unknown) => {
        throw new Error(`Expected promise to resolve, but it rejected with ${format(error)}`);
      });
    }
    return promise.then(
      (value) => { throw new Error(`Expected promise to reject, but it resolved with ${format(value)}`); },
      evaluate,
    );
  }

  toBe(expected: unknown): MatcherResult { return this.apply("to be", (value) => Object.is(value, expected), expected); }
  toEqual(expected: unknown): MatcherResult { return this.apply("to equal", (value) => equals(value, expected), expected); }
  toStrictEqual(expected: unknown): MatcherResult { return this.toEqual(expected); }
  toBeTruthy(): MatcherResult { return this.apply("to be truthy", Boolean); }
  toBeFalsy(): MatcherResult { return this.apply("to be falsy", (value) => !value); }
  toBeNull(): MatcherResult { return this.apply("to be null", (value) => value === null); }
  toBeUndefined(): MatcherResult { return this.apply("to be undefined", (value) => value === undefined); }
  toBeDefined(): MatcherResult { return this.apply("to be defined", (value) => value !== undefined); }
  toBeNaN(): MatcherResult { return this.apply("to be NaN", (value) => Number.isNaN(value)); }
  toBeInstanceOf(expected: unknown): MatcherResult {
    return this.apply("to be an instance of", (value) => typeof expected === "function" && value instanceof expected, expected);
  }
  toContain(expected: unknown): MatcherResult {
    return this.apply("to contain", (value) => typeof value === "string"
      ? value.includes(String(expected))
      : Array.isArray(value) && value.some((item) => Object.is(item, expected)), expected);
  }
  toContainEqual(expected: unknown): MatcherResult {
    return this.apply("to contain equal", (value) => Array.isArray(value) && value.some((item) => equals(item, expected)), expected);
  }
  toHaveLength(expected: unknown): MatcherResult {
    return this.apply("to have length", (value) => value != null && "length" in Object(value) && Object(value).length === expected, expected);
  }
  toMatch(expected: unknown): MatcherResult {
    return this.apply("to match", (value) => typeof value === "string" && (expected instanceof RegExp ? expected.test(value) : value.includes(String(expected))), expected);
  }
  toBeGreaterThan(expected: unknown): MatcherResult { return this.apply("to be greater than", (value) => typeof value === "number" && typeof expected === "number" && value > expected, expected); }
  toBeGreaterThanOrEqual(expected: unknown): MatcherResult { return this.apply("to be greater than or equal to", (value) => typeof value === "number" && typeof expected === "number" && value >= expected, expected); }
  toBeLessThan(expected: unknown): MatcherResult { return this.apply("to be less than", (value) => typeof value === "number" && typeof expected === "number" && value < expected, expected); }
  toBeLessThanOrEqual(expected: unknown): MatcherResult { return this.apply("to be less than or equal to", (value) => typeof value === "number" && typeof expected === "number" && value <= expected, expected); }
  toBeCloseTo(expected: unknown, precision = 2): MatcherResult {
    return this.apply("to be close to", (value) => typeof value === "number" && typeof expected === "number" && Math.abs(value - expected) < 0.5 * 10 ** -precision, expected);
  }
  toHaveProperty(path: unknown, expected?: unknown): MatcherResult {
    const parts = Array.isArray(path) ? path.map(String) : String(path).split(".");
    let propertyValue: unknown = this.received;
    let exists = true;
    for (const part of parts) {
      if (propertyValue == null || !(part in Object(propertyValue))) { exists = false; break; }
      propertyValue = Object(propertyValue)[part];
    }
    return this.apply("to have property", () => exists && (arguments.length < 2 || equals(propertyValue, expected)), path);
  }
  toThrow(expected?: unknown): MatcherResult {
    return this.apply("to throw", (value) => {
      if (typeof value !== "function") return false;
      try { value(); return false; } catch (error) {
        if (expected === undefined) return true;
        if (typeof expected === "string") return error instanceof Error && error.message.includes(expected);
        if (expected instanceof RegExp) return error instanceof Error && expected.test(error.message);
        if (typeof expected === "function") return error instanceof expected;
        return equals(error, expected);
      }
    }, expected);
  }
  toMatchObject(expected: unknown): MatcherResult {
    const subset = (value: unknown, shape: unknown): boolean => {
      if (isAsymmetric(shape)) return shape.matches(value);
      if (typeof shape !== "object" || shape === null) return equals(value, shape);
      if (typeof value !== "object" || value === null) return false;
      return Reflect.ownKeys(shape).every((key) => subset((value as Record<PropertyKey, unknown>)[key], (shape as Record<PropertyKey, unknown>)[key]));
    };
    return this.apply("to match object", (value) => subset(value, expected), expected);
  }
  toBeInTheDocument(): MatcherResult {
    return this.apply("to be in the document", (value) => value instanceof Node && (value.ownerDocument?.documentElement.contains(value) ?? false));
  }
  toHaveTextContent(expected: unknown): MatcherResult {
    return this.apply("to have text content", (value) => value instanceof Node && (expected instanceof RegExp ? expected.test(value.textContent ?? "") : (value.textContent ?? "").includes(String(expected))), expected);
  }
  toHaveAttribute(name: string, expected?: unknown): MatcherResult {
    return this.apply("to have attribute", (value) => value instanceof Element && value.hasAttribute(name) && (arguments.length < 2 || value.getAttribute(name) === String(expected)), name);
  }
  toBeVisible(): MatcherResult {
    return this.apply("to be visible", (value) => value instanceof HTMLElement && !value.hidden && value.style.display !== "none" && value.style.visibility !== "hidden");
  }
  toBeDisabled(): MatcherResult {
    return this.apply("to be disabled", (value) => value instanceof HTMLElement && ("disabled" in value ? Boolean(value.disabled) : value.getAttribute("aria-disabled") === "true"));
  }
  toHaveValue(expected: unknown): MatcherResult {
    return this.apply("to have value", (value) => value instanceof HTMLElement && "value" in value && Object.is(value.value, expected), expected);
  }
  toHaveBeenCalled(): MatcherResult { return this.apply("to have been called", hasCalls); }
  toHaveBeenCalledTimes(expected: number): MatcherResult { return this.apply("to have been called times", (value) => callList(value)?.length === expected, expected); }
  toHaveBeenCalledWith(...expected: unknown[]): MatcherResult {
    return this.apply("to have been called with", (value) => callList(value)?.some((call) => equals(call, expected)) ?? false, expected);
  }
  toHaveBeenLastCalledWith(...expected: unknown[]): MatcherResult {
    return this.apply("to have been last called with", (value) => {
      const calls = callList(value); return calls !== undefined && calls.length > 0 && equals(calls.at(-1), expected);
    }, expected);
  }
  toMatchSnapshot(name?: string): MatcherResult {
    if (this.promiseMode !== "none") return this.apply("to match snapshot", (value) => { matchSnapshot(value, name); return true; });
    if (this.inverted) throw new Error("Snapshot matchers cannot be negated");
    matchSnapshot(this.received, name);
  }
  toMatchInlineSnapshot(expected: string): MatcherResult {
    return this.apply("to match inline snapshot", (value) => serialize(value) === expected.trim(), expected);
  }

  custom(name: string, expected: unknown[]): MatcherResult {
    const matcher = customMatchers.get(name);
    if (!matcher) throw new Error(`Unknown matcher ${name}`);
    return this.apply(name, (value) => {
      const result = matcher(value, ...expected);
      return typeof result === "boolean" ? result : result.pass;
    }, expected);
  }
}

function callList(value: unknown): unknown[][] | undefined {
  if (typeof value !== "function" || !("mock" in value)) return undefined;
  const mock = (value as { mock?: { calls?: unknown[][] } }).mock;
  return mock?.calls;
}

function hasCalls(value: unknown): boolean {
  return (callList(value)?.length ?? 0) > 0;
}

function matcher(matches: (value: unknown) => boolean, description: string): AsymmetricMatcher {
  return { [asymmetric]: true, matches, describe: () => description };
}

export interface ExpectStatic {
  (received: unknown): Expectation & Record<string, (...expected: unknown[]) => MatcherResult>;
  anything(): AsymmetricMatcher;
  any(constructor: Function): AsymmetricMatcher;
  stringContaining(value: string): AsymmetricMatcher;
  stringMatching(value: string | RegExp): AsymmetricMatcher;
  objectContaining(value: Record<PropertyKey, unknown>): AsymmetricMatcher;
  arrayContaining(value: readonly unknown[]): AsymmetricMatcher;
  extend(matchers: Record<string, Matcher>): void;
}

export const expect = ((received: unknown) => new Proxy(new Expectation(received), {
  get(target, property, receiver) {
    if (typeof property === "string" && customMatchers.has(property)) {
      return (...expected: unknown[]) => target.custom(property, expected);
    }
    return Reflect.get(target, property, receiver);
  },
})) as ExpectStatic;

expect.anything = () => matcher((value) => value !== null && value !== undefined, "Anything");
expect.any = (constructor) => matcher((value) => {
  if (constructor === String) return typeof value === "string";
  if (constructor === Number) return typeof value === "number";
  if (constructor === Boolean) return typeof value === "boolean";
  return typeof constructor === "function" && value instanceof constructor;
}, `Any<${constructor.name}>`);
expect.stringContaining = (part) => matcher((value) => typeof value === "string" && value.includes(part), `StringContaining<${part}>`);
expect.stringMatching = (pattern) => matcher((value) => typeof value === "string" && (pattern instanceof RegExp ? pattern.test(value) : value.includes(pattern)), `StringMatching<${pattern}>`);
expect.objectContaining = (shape) => matcher((value) => typeof value === "object" && value !== null && Reflect.ownKeys(shape).every((key) => equals((value as Record<PropertyKey, unknown>)[key], shape[key])), "ObjectContaining");
expect.arrayContaining = (items) => matcher((value) => Array.isArray(value) && items.every((item) => value.some((candidate) => equals(candidate, item))), "ArrayContaining");
expect.extend = (matchers) => { for (const [name, implementation] of Object.entries(matchers)) customMatchers.set(name, implementation); };
