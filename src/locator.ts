import {
  fireEvent,
  getQueriesForElement,
  queries,
  screen,
  waitFor,
  within,
  type ByRoleOptions,
  type Matcher,
  type MatcherOptions,
} from "@testing-library/dom";
import type { NativeAutomation } from "./types.js";

type Resolver = () => readonly Element[];
let nativeAutomation: NativeAutomation | undefined;

export function setNativeAutomation(adapter: NativeAutomation | undefined): void {
  nativeAutomation = adapter;
}

export class Locator {
  constructor(
    private readonly resolve: Resolver,
    private readonly description = "locator",
  ) {}

  all(): readonly Element[] {
    return this.resolve();
  }

  count(): number {
    return this.resolve().length;
  }

  element(): Element {
    const matches = this.resolve();
    if (matches.length !== 1) {
      throw new Error(`${this.description} resolved to ${matches.length} elements; expected exactly one`);
    }
    return matches[0] as Element;
  }

  first(): Locator {
    return new Locator(() => this.resolve().slice(0, 1), `${this.description}.first()`);
  }

  last(): Locator {
    return new Locator(() => this.resolve().slice(-1), `${this.description}.last()`);
  }

  nth(index: number): Locator {
    return new Locator(() => {
      const item = this.resolve().at(index);
      return item ? [item] : [];
    }, `${this.description}.nth(${index})`);
  }

  filter(options: { hasText?: string | RegExp; has?: Locator }): Locator {
    return new Locator(() => this.resolve().filter((element) => {
      const textMatches = options.hasText === undefined
        || (options.hasText instanceof RegExp ? options.hasText.test(element.textContent ?? "") : (element.textContent ?? "").includes(options.hasText));
      const childMatches = options.has === undefined
        || options.has.all().some((child) => element.contains(child));
      return textMatches && childMatches;
    }), `${this.description}.filter()`);
  }

  locator(selector: string): Locator {
    return new Locator(() => this.resolve().flatMap((root) => [...root.querySelectorAll(selector)]), `${this.description}.locator(${JSON.stringify(selector)})`);
  }

  getByRole(role: ByRoleOptions extends never ? never : Parameters<typeof queries.queryAllByRole>[1], options?: ByRoleOptions): Locator {
    return new Locator(() => this.queryAll((root) => queries.queryAllByRole(root, role, options)), `${this.description}.getByRole(${JSON.stringify(role)})`);
  }

  getByText(text: Matcher, options?: MatcherOptions): Locator {
    return new Locator(() => this.queryAll((root) => queries.queryAllByText(root, text, options)), `${this.description}.getByText(${String(text)})`);
  }

  getByTestId(id: Matcher, options?: MatcherOptions): Locator {
    return new Locator(() => this.queryAll((root) => queries.queryAllByTestId(root, id, options)), `${this.description}.getByTestId(${String(id)})`);
  }

  async findByRole(role: Parameters<typeof queries.queryAllByRole>[1], options?: ByRoleOptions): Promise<Locator> {
    const result = this.getByRole(role, options);
    await waitFor(() => result.element());
    return result;
  }

  async findByText(text: Matcher, options?: MatcherOptions): Promise<Locator> {
    const result = this.getByText(text, options);
    await waitFor(() => result.element());
    return result;
  }

  async findByTestId(id: Matcher, options?: MatcherOptions): Promise<Locator> {
    const result = this.getByTestId(id, options);
    await waitFor(() => result.element());
    return result;
  }

  textContent(): string {
    return this.element().textContent ?? "";
  }

  getAttribute(name: string): string | null {
    return this.element().getAttribute(name);
  }

  isVisible(): boolean {
    const element = this.element();
    return element instanceof HTMLElement && !element.hidden && element.style.display !== "none" && element.style.visibility !== "hidden";
  }

  focus(): void {
    const element = this.element();
    if (!(element instanceof HTMLElement)) throw new Error(`${this.description} is not focusable`);
    element.focus();
  }

  click(): void {
    const element = this.element();
    if (!(element instanceof HTMLElement)) throw new Error(`${this.description} is not clickable`);
    if ("disabled" in element && element.disabled) return;
    fireEvent.pointerDown(element, { pointerType: "mouse", button: 0 });
    fireEvent.mouseDown(element, { button: 0 });
    element.focus();
    fireEvent.pointerUp(element, { pointerType: "mouse", button: 0 });
    fireEvent.mouseUp(element, { button: 0 });
    element.click();
  }

  dblClick(): void {
    const element = this.element();
    fireEvent.click(element, { detail: 1 });
    fireEvent.click(element, { detail: 2 });
    fireEvent.doubleClick(element, { detail: 2 });
  }

  fill(value: string): void {
    const element = this.element();
    setFormValue(element, value);
    fireEvent.input(element, { target: { value } });
    fireEvent.change(element, { target: { value } });
  }

  type(text: string): void {
    const element = this.element();
    if (!(element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLElement && element.isContentEditable)) {
      throw new Error(`${this.description} does not accept text input`);
    }
    element.focus();
    for (const character of text) {
      fireEvent.keyDown(element, { key: character });
      fireEvent.keyPress(element, { key: character });
      if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
        setFormValue(element, element.value + character);
      } else {
        element.textContent = (element.textContent ?? "") + character;
      }
      fireEvent.input(element, { data: character, inputType: "insertText" });
      fireEvent.keyUp(element, { key: character });
    }
  }

  press(key: string): void {
    const element = this.element();
    fireEvent.keyDown(element, { key });
    fireEvent.keyUp(element, { key });
  }

  check(): void {
    const element = this.element();
    if (!(element instanceof HTMLInputElement) || !["checkbox", "radio"].includes(element.type)) throw new Error(`${this.description} is not a checkbox or radio`);
    if (!element.checked) element.click();
  }

  uncheck(): void {
    const element = this.element();
    if (!(element instanceof HTMLInputElement) || element.type !== "checkbox") throw new Error(`${this.description} is not a checkbox`);
    if (element.checked) element.click();
  }

  selectOptions(values: string | readonly string[]): void {
    const element = this.element();
    if (!(element instanceof HTMLSelectElement)) throw new Error(`${this.description} is not a select`);
    const selected = new Set(Array.isArray(values) ? values : [values]);
    for (const option of element.options) option.selected = selected.has(option.value);
    fireEvent.input(element);
    fireEvent.change(element);
  }

  async nativeClick(): Promise<void> {
    await requireNative().click(this.element());
  }

  async nativeType(text: string): Promise<void> {
    await requireNative().type(this.element(), text);
  }

  async nativePress(key: string): Promise<void> {
    await requireNative().press(key, this.element());
  }

  private queryAll(query: (root: HTMLElement) => HTMLElement[]): Element[] {
    return this.resolve().flatMap((root) => query(root as HTMLElement));
  }
}

function setFormValue(element: Element, value: string): void {
  if (element instanceof HTMLInputElement) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(element, value);
    return;
  }
  if (element instanceof HTMLTextAreaElement) {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set?.call(element, value);
    return;
  }
  if (element instanceof HTMLElement && element.isContentEditable) {
    element.textContent = value;
    return;
  }
  throw new Error("Element does not accept a value");
}

function requireNative(): NativeAutomation {
  if (!nativeAutomation) {
    throw new Error("Trusted native automation is unavailable. Configure a native adapter; synthetic interactions never fall back to trusted input.");
  }
  return nativeAutomation;
}

export function createPage(root: ParentNode = document): Locator {
  return new Locator(() => {
    if (root instanceof Document) return root.documentElement ? [root.documentElement] : [];
    if (root instanceof Element) return [root];
    return [];
  }, "page");
}

export const page = new Proxy({} as Locator, {
  get(_target, property) {
    const value = createPage()[property as keyof Locator];
    return typeof value === "function" ? value.bind(createPage()) : value;
  },
});

export const native = {
  click(target: Locator): Promise<void> { return target.nativeClick(); },
  type(target: Locator, text: string): Promise<void> { return target.nativeType(text); },
  press(key: string, target?: Locator): Promise<void> { return target ? target.nativePress(key) : requireNative().press(key); },
};

export { fireEvent, getQueriesForElement, queries, screen, waitFor, within };
