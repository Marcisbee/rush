import { computeAccessibleName } from "dom-accessibility-api";
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
type Assertion = Expectation & Record<string, (...expected: unknown[]) => MatcherResult>;

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

type DomConstructorName = "Node" | "Element" | "HTMLElement";
type ClassExpectation = string | RegExp;
type StyleExpectation = string | Record<string, unknown>;

function isDomInstance<T extends object>(value: unknown, constructorName: DomConstructorName): value is T {
  if (typeof value !== "object" || value === null) return false;
  const ownerDocument = (value as { ownerDocument?: Document | null }).ownerDocument;
  const realm = ownerDocument?.defaultView ?? globalThis;
  const constructor = realm[constructorName];
  return typeof constructor === "function" && value instanceof constructor;
}

function isNode(value: unknown): value is Node {
  return isDomInstance<Node>(value, "Node");
}

function isElement(value: unknown): value is Element {
  return isDomInstance<Element>(value, "Element");
}

function isHtmlElement(value: unknown): value is HTMLElement {
  return isDomInstance<HTMLElement>(value, "HTMLElement");
}

function matchesPattern(value: string, expected: string | RegExp): boolean {
  if (typeof expected === "string") return value === expected;
  expected.lastIndex = 0;
  return expected.test(value);
}

function classList(value: unknown): string[] | undefined {
  if (!isElement(value)) return undefined;
  return (value.getAttribute("class") ?? "").split(/\s+/).filter(Boolean);
}

function parseExpectedClasses(values: readonly ClassExpectation[]): ClassExpectation[] {
  const expected: ClassExpectation[] = [];
  for (const value of values) {
    if (typeof value === "string") expected.push(...value.split(/\s+/).filter(Boolean));
    else expected.push(value);
  }
  return expected;
}

function hasClasses(value: unknown, expected: readonly ClassExpectation[], exact: boolean): boolean {
  const received = classList(value);
  if (!received || expected.length === 0) return false;
  const containsExpected = expected.every((item) => received.some((className) => matchesPattern(className, item)));
  return containsExpected && (!exact || received.length === expected.length);
}

function expectedStyles(element: Element, css: StyleExpectation): Record<string, string> {
  const declaration = element.ownerDocument.createElement("div").style;
  if (typeof css === "string") declaration.cssText = css;
  else {
    for (const [property, value] of Object.entries(css)) {
      if (property.startsWith("--")) declaration.setProperty(property, String(value));
      else (declaration as unknown as Record<string, string>)[property] = String(value);
    }
  }

  const styles: Record<string, string> = {};
  for (let index = 0; index < declaration.length; index += 1) {
    const property = declaration.item(index);
    styles[property] = declaration.getPropertyValue(property);
  }
  return styles;
}

function hasStyles(value: unknown, expected: StyleExpectation): boolean {
  if (!isElement(value)) return false;
  const styles = expectedStyles(value, expected);
  if (Object.keys(styles).length === 0) return false;
  const view = value.ownerDocument.defaultView;
  if (!view) return false;
  const computed = view.getComputedStyle(value);
  return Object.entries(styles).every(([property, expectedValue]) => computed.getPropertyValue(property) === expectedValue);
}

const disableableTags = new Set(["fieldset", "input", "select", "optgroup", "option", "button", "textarea"]);

function canBeDisabled(element: Element): boolean {
  const tag = element.tagName.toLowerCase();
  return disableableTags.has(tag) || tag.includes("-");
}

function isFirstLegend(element: Element, fieldset: Element): boolean {
  return element.tagName.toLowerCase() === "legend"
    && fieldset.tagName.toLowerCase() === "fieldset"
    && [...fieldset.children].find((child) => child.tagName.toLowerCase() === "legend") === element;
}

function isDisabledElement(element: Element): boolean {
  return canBeDisabled(element) && element.hasAttribute("disabled");
}

function hasDisabledAncestor(element: Element): boolean {
  const parent = element.parentElement;
  if (!parent) return false;
  return (isDisabledElement(parent) && !isFirstLegend(element, parent)) || hasDisabledAncestor(parent);
}

function isDisabled(value: unknown): boolean {
  return isElement(value) && canBeDisabled(value) && (isDisabledElement(value) || hasDisabledAncestor(value));
}

function isChecked(value: unknown): boolean {
  if (!isElement(value)) return false;
  if (value.tagName.toLowerCase() === "input") {
    const input = value as HTMLInputElement;
    return (input.type === "checkbox" || input.type === "radio") && input.checked;
  }
  const role = value.getAttribute("role");
  return (role === "checkbox" || role === "radio" || role === "switch")
    && value.getAttribute("aria-checked") === "true";
}

class Expectation {
  constructor(
    private readonly received: unknown,
    private readonly inverted = false,
    private readonly promiseMode: PromiseMode = "none",
  ) {}

  get not(): Assertion {
    return createExpectation(this.received, !this.inverted, this.promiseMode);
  }

  get resolves(): Assertion {
    return createExpectation(this.received, this.inverted, "resolves");
  }

  get rejects(): Assertion {
    return createExpectation(this.received, this.inverted, "rejects");
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
  toBeTypeOf(expected: unknown): MatcherResult {
    return this.apply("to be of type", (value) => typeof value === expected, expected);
  }
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
      if (this.promiseMode === "rejects") return matchesThrownValue(value, expected);
      if (typeof value !== "function") return false;
      try { value(); return false; } catch (error) {
        return matchesThrownValue(error, expected);
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
    return this.apply("to be in the document", (value) => isNode(value) && (value.ownerDocument?.documentElement.contains(value) ?? false));
  }
  toBeEmptyDOMElement(): MatcherResult {
    return this.apply("to be an empty DOM element", (value) => isElement(value)
      && [...value.childNodes].every((child) => child.nodeType === child.COMMENT_NODE));
  }
  toContainElement(expected: unknown): MatcherResult {
    return this.apply("to contain element", (value) => isElement(value) && isElement(expected) && value.contains(expected), expected);
  }
  toHaveTextContent(expected: unknown): MatcherResult {
    return this.apply("to have text content", (value) => isNode(value) && (expected instanceof RegExp ? expected.test(value.textContent ?? "") : (value.textContent ?? "").includes(String(expected))), expected);
  }
  toHaveAttribute(name: string, expected?: unknown): MatcherResult {
    return this.apply("to have attribute", (value) => isElement(value) && value.hasAttribute(name) && (arguments.length < 2 || value.getAttribute(name) === String(expected)), name);
  }
  toHaveClass(...values: Array<ClassExpectation | { exact: boolean }>): MatcherResult {
    const maybeOptions = values.at(-1);
    const options = typeof maybeOptions === "object" && !(maybeOptions instanceof RegExp) ? values.pop() as { exact: boolean } : undefined;
    const expected = parseExpectedClasses(values as ClassExpectation[]);
    if (options?.exact && expected.some((value) => value instanceof RegExp)) {
      throw new Error("Exact class matching does not support regular expressions");
    }
    return this.apply("to have class", (value) => {
      if (expected.length === 0) return this.inverted && (classList(value)?.length ?? 0) > 0;
      return hasClasses(value, expected, options?.exact ?? false);
    }, expected);
  }
  toHaveStyle(expected: StyleExpectation): MatcherResult {
    return this.apply("to have style", (value) => hasStyles(value, expected), expected);
  }
  toHaveAccessibleName(expected?: string | RegExp | AsymmetricMatcher): MatcherResult {
    const hasExpected = arguments.length > 0;
    return this.apply("to have accessible name", (value) => {
      if (!isElement(value)) return false;
      const name = computeAccessibleName(value);
      return hasExpected
        ? expected instanceof RegExp ? matchesPattern(name, expected) : equals(name, expected)
        : name !== "";
    }, expected);
  }
  toHaveFocus(): MatcherResult {
    return this.apply("to have focus", (value) => isElement(value) && value.ownerDocument.activeElement === value);
  }
  toBeVisible(): MatcherResult {
    return this.apply("to be visible", (value) => isHtmlElement(value) && !value.hidden && value.style.display !== "none" && value.style.visibility !== "hidden");
  }
  toBeDisabled(): MatcherResult {
    return this.apply("to be disabled", isDisabled);
  }
  toBeEnabled(): MatcherResult {
    return this.apply("to be enabled", (value) => isElement(value) && !isDisabled(value));
  }
  toBeChecked(): MatcherResult {
    return this.apply("to be checked", isChecked);
  }
  toHaveValue(expected: unknown): MatcherResult {
    return this.apply("to have value", (value) => isHtmlElement(value) && "value" in value && Object.is(value.value, expected), expected);
  }
  toHaveBeenCalled(): MatcherResult { return this.apply("to have been called", hasCalls); }
  toHaveBeenCalledOnce(): MatcherResult { return this.apply("to have been called once", (value) => callList(value)?.length === 1); }
  toHaveBeenCalledTimes(expected: number): MatcherResult { return this.apply("to have been called times", (value) => callList(value)?.length === expected, expected); }
  toHaveBeenCalledWith(...expected: unknown[]): MatcherResult {
    return this.apply("to have been called with", (value) => callList(value)?.some((call) => equals(call, expected)) ?? false, expected);
  }
  toHaveBeenLastCalledWith(...expected: unknown[]): MatcherResult {
    return this.apply("to have been last called with", (value) => {
      const calls = callList(value); return calls !== undefined && calls.length > 0 && equals(calls.at(-1), expected);
    }, expected);
  }
  toHaveBeenNthCalledWith(index: number, ...expected: unknown[]): MatcherResult {
    return this.apply("to have been nth called with", (value) => {
      const calls = callList(value);
      return Number.isInteger(index) && index > 0 && calls !== undefined && equals(calls[index - 1], expected);
    }, [index, ...expected]);
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

function matchesThrownValue(value: unknown, expected: unknown): boolean {
  if (expected === undefined) return true;
  if (typeof expected === "string") return value instanceof Error && value.message.includes(expected);
  if (expected instanceof RegExp) {
    expected.lastIndex = 0;
    return value instanceof Error && expected.test(value.message);
  }
  if (typeof expected === "function") return value instanceof expected;
  return equals(value, expected);
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

function createExpectation(received: unknown, inverted = false, promiseMode: PromiseMode = "none"): Assertion {
  return new Proxy(new Expectation(received, inverted, promiseMode), {
    get(target, property, receiver) {
      if (typeof property === "string" && customMatchers.has(property)) {
        return (...expected: unknown[]) => target.custom(property, expected);
      }
      return Reflect.get(target, property, receiver);
    },
  }) as Assertion;
}

export const expect = ((received: unknown) => createExpectation(received)) as ExpectStatic;

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
