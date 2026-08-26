import {
  beforeEach,
  describe,
  expect,
  screen,
  test,
  transformHoistedMocks,
  vi,
} from "rush-webtest";

beforeEach(() => {
  document.body.innerHTML = `
    <label>Name <input data-testid="name"></label>
    <button type="button">Save changes</button>
    <input type="checkbox" aria-label="Subscribe">
  `;
});

describe("browser API", () => {
  test("supports common, asymmetric, promise, and DOM matchers", async () => {
    const button = screen.getByRole("button", { name: "Save changes" });

    expect({ value: 2, label: "saved" }).toEqual({ value: 2, label: expect.stringContaining("save") });
    expect([1, 2, 3]).toEqual(expect.arrayContaining([3, 1]));
    expect(() => { throw new Error("boom"); }).toThrow(/boom/);
    expect(button).toBeInTheDocument();
    expect(button).toHaveTextContent("Save");
    await expect(Promise.resolve(4)).resolves.toBeGreaterThan(3);
    await expect(Promise.reject(new Error("nope"))).rejects.toBeInstanceOf(Error);
  });

  test("preserves custom matchers across assertion chains", async () => {
    expect.extend({
      toBeDivisibleBy(received, divisor) {
        return { pass: typeof received === "number" && typeof divisor === "number" && received % divisor === 0 };
      },
    });

    (expect(8) as any).toBeDivisibleBy(2);
    (expect(7).not as any).toBeDivisibleBy(2);
    await (expect(Promise.resolve(8)).resolves as any).toBeDivisibleBy(2);
    await (expect(Promise.resolve(7)).resolves.not as any).toBeDivisibleBy(2);
    await (expect(Promise.reject(8)).rejects as any).toBeDivisibleBy(2);
  });

  test("matches rejected errors with toThrow", async () => {
    await expect(Promise.reject(new Error("nope"))).rejects.toThrow("nope");
    await expect(Promise.reject(new TypeError("wrong type"))).rejects.toThrow(TypeError);
    await expect(Promise.reject(new Error("nope"))).rejects.not.toThrow("different");
  });

  test("supports remaining core and DOM matchers", () => {
    const parent = document.createElement("div");
    const child = document.createElement("span");
    const comment = document.createComment("ignored");
    parent.append(child);

    expect("saved").toBeTypeOf("string");
    expect(null).toBeTypeOf("object");
    expect(parent).toContainElement(child);
    expect(child).not.toContainElement(parent);
    expect(parent).not.toBeEmptyDOMElement();
    expect(child).toBeEmptyDOMElement();
    child.append(comment);
    expect(child).toBeEmptyDOMElement();
    child.append(" ");
    expect(child).not.toBeEmptyDOMElement();
  });

  test("waits for synchronous and asynchronous callbacks with vi.waitFor", async () => {
    let attempts = 0;
    const value = await vi.waitFor(() => {
      attempts += 1;
      if (attempts < 3) throw new Error("not ready");
      return "ready";
    }, { interval: 1, timeout: 20 });

    let asynchronousAttempts = 0;
    const asynchronousValue = await vi.waitFor(async () => {
      asynchronousAttempts += 1;
      if (asynchronousAttempts < 2) throw new Error("not ready");
      return 42;
    }, 20);

    expect(value).toBe("ready");
    expect(attempts).toBe(3);
    expect(asynchronousValue).toBe(42);
    expect(asynchronousAttempts).toBe(2);

    await expect(vi.waitFor(() => {
      throw new Error("still pending");
    }, { interval: 1, timeout: 2 })).rejects.toThrow("still pending");
  });

  test("advances fake timers while waiting with vi.waitFor", async () => {
    vi.useFakeTimers();
    let ready = false;
    setTimeout(() => { ready = true; }, 20);

    const value = await vi.waitFor(() => {
      expect(ready).toBe(true);
      return "ready";
    }, { interval: 10, timeout: 30 });

    expect(value).toBe("ready");
    expect(Date.now()).toBe(20);
  });

  test.browser("queries and interacts with the real DOM", ({ page }) => {
    const input = page.getByTestId("name");
    const inputTrust: boolean[] = [];
    input.element().addEventListener("input", (event: Event) => inputTrust.push(event.isTrusted));

    expect(page.getByText("Name").count()).toBe(1);
    expect(input.element()).toBeInstanceOf(HTMLInputElement);
    input.fill("Ada");
    input.type(" Lovelace");
    expect((input.element() as HTMLInputElement).value).toBe("Ada Lovelace");
    expect(inputTrust.every((value) => value === false)).toBe(true);

    page.getByRole("checkbox", { name: "Subscribe" }).check();
    expect((page.getByRole("checkbox").element() as HTMLInputElement).checked).toBe(true);

    const clickEvents: string[] = [];
    const button = page.getByRole("button");
    for (const type of ["pointerdown", "mousedown", "pointerup", "mouseup", "click"]) {
      button.element().addEventListener(type, () => clickEvents.push(type));
    }
    button.click();
    expect(clickEvents).toEqual(["pointerdown", "mousedown", "pointerup", "mouseup", "click"]);
  });

  test("tracks mock calls and one-time implementations", () => {
    const mock = vi.fn((value: number) => value * 2)
      .mockImplementationOnce(() => 99);

    expect(mock(2)).toBe(99);
    expect(mock(3)).toBe(6);
    expect(mock).toHaveBeenCalledTimes(2);
    expect(mock).toHaveBeenLastCalledWith(3);
  });

  test("supports Vitest-compatible mock inspection matchers", () => {
    const implementation = (value: number) => value * 2;
    const mock = vi.fn(implementation).mockImplementationOnce(() => 99);
    const structured = vi.fn();

    expect(mock.getMockImplementation()).toBe(implementation);
    expect(mock(1)).toBe(99);
    mock(2);
    structured({ id: 7 });

    expect(mock).not.toHaveBeenCalledOnce();
    expect(mock).toHaveBeenNthCalledWith(1, 1);
    expect(mock).toHaveBeenNthCalledWith(2, 2);
    expect(structured).toHaveBeenCalledOnce();
    expect(structured).toHaveBeenNthCalledWith(1, expect.objectContaining({ id: 7 }));
    expect(mock.getMockImplementation()).toBe(implementation);
    mock.mockReset();
    expect(mock.getMockImplementation()).toBeUndefined();
  });

  test("supports Vitest-compatible DOM state matchers", () => {
    const button = screen.getByRole("button", { name: "Save changes" });
    const checkbox = screen.getByRole("checkbox", { name: "Subscribe" });

    button.className = "primary action-button";
    button.style.cssText = "width: 170px; color: rgb(12, 34, 56)";
    button.focus();
    (checkbox as HTMLInputElement).checked = true;

    expect(button).toHaveClass("primary", "action-button");
    expect(button).toHaveClass(/action-/);
    expect(button).toHaveClass("primary action-button", { exact: true });
    expect(button).not.toHaveClass("primary", { exact: true });
    expect(document.body).not.toHaveClass();
    expect(button).toHaveStyle({ width: "170px", color: "#0c2238" });
    expect(button).toHaveStyle("width: 170px");
    expect(button).toHaveAccessibleName("Save changes");
    expect(button).toHaveAccessibleName(/save/i);
    expect(button).toHaveAccessibleName();
    expect(button).toHaveFocus();
    expect(checkbox).toBeChecked();
    expect(button).toBeEnabled();

    const ariaCheckbox = document.createElement("div");
    ariaCheckbox.setAttribute("role", "checkbox");
    ariaCheckbox.setAttribute("aria-checked", "true");
    expect(ariaCheckbox).toBeChecked();

    const fieldset = document.createElement("fieldset");
    fieldset.disabled = true;
    const disabledButton = document.createElement("button");
    fieldset.append(disabledButton);
    expect(disabledButton).toBeDisabled();
    expect(disabledButton).not.toBeEnabled();
  });

  test("spies on and restores methods", () => {
    const target = { greet(name: string) { return `hello ${name}`; } };
    const original = target.greet;
    const spy = vi.spyOn(target, "greet");

    expect(target.greet("Ada")).toBe("hello Ada");
    expect(spy.mock.calls).toEqual([["Ada"]]);
    spy.mockRestore();
    expect(target.greet).toBe(original);
  });

  test("runs fake timers without waiting for wall time", () => {
    vi.useFakeTimers({ now: new Date("2026-01-01T00:00:00Z") });
    const callback = vi.fn();
    setTimeout(callback, 500);

    vi.advanceTimersByTime(499);
    expect(callback).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(callback).toHaveBeenCalled();
    expect(Date.now()).toBe(new Date("2026-01-01T00:00:00.500Z").getTime());
  });

  test("lets queued browser work schedule timers before an async advance", async () => {
    vi.useFakeTimers();
    const pending = new Promise((resolve) => {
      queueMicrotask(async () => {
        await new Response("done").text();
        setTimeout(() => resolve("done"), 300);
      });
    });

    await vi.advanceTimersByTimeAsync(300);

    await expect(pending).resolves.toBe("done");
  });

  test("passes importOriginal to async mock factories", async () => {
    vi.mock("partial-module", async (importOriginal) => {
      const actual = await importOriginal<{ read(): string }>();
      return { ...actual, read: () => `mocked:${actual.read()}` };
    });

    const loaded = await vi.importMock("partial-module", async () => ({ read: () => "real" }));
    expect(loaded.read()).toBe("mocked:real");
  });
});

describe("static mock hoisting", () => {
  test("registers mocks before rewriting imports", async () => {
    const transformed = await transformHoistedMocks(`
      import { vi, test } from "rush-webtest";
      import service, { read as load } from "./service.js";
      vi.mock("./service.js", () => ({ default: { mocked: true }, read: vi.fn(() => 7) }));
      test("mocked", () => load());
    `);

    expect(transformed).toContain(`__rushRegisterMock__("./service.js", () => ({ default: { mocked: true }, read: vi.fn(() => 7) }));`);
    expect(transformed).toContain(`const { default: service, read: load } = await __rushImport__("./service.js"`);
    expect(transformed.indexOf("__rushRegisterMock__")).toBeLessThan(transformed.indexOf("const { default: service"));
    expect(transformed).not.toContain(`vi.mock("./service.js"`);
  });

  test("initializes vi.hoisted state before mocks and delayed imports", async () => {
    const transformed = await transformHoistedMocks(`
      import { vi, test } from "rush-webtest";
      import { read } from "./service.js";
      const { state } = vi.hoisted(() => ({ state: { read: vi.fn(() => "mocked") } }));
      vi.mock("./service.js", () => state);
      test("mocked", () => read());
    `);

    const hoisted = transformed.indexOf("const { state } = __rushVi.hoisted");
    const registration = transformed.indexOf(`__rushRegisterMock__("./service.js", () => state)`);
    const delayedImport = transformed.indexOf(`await __rushImport__("./service.js"`);
    expect(hoisted).not.toBe(-1);
    expect(hoisted).toBeLessThan(registration);
    expect(registration).toBeLessThan(delayedImport);
    expect(transformed).not.toContain("const { state } = vi.hoisted");
  });

  test("leaves modules without static mocks unchanged", async () => {
    const source = `import { test } from "rush-webtest"; test("ok", () => {});`;
    expect(await transformHoistedMocks(source)).toBe(source);
  });
});
