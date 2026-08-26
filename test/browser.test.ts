import {
  beforeEach,
  describe,
  expect,
  screen,
  test,
  transformHoistedMocks,
  vi,
} from "@rush/browser";

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
});

describe("static mock hoisting", () => {
  test("registers mocks before rewriting imports", async () => {
    const transformed = await transformHoistedMocks(`
      import { vi, test } from "@rush/browser";
      import service, { read as load } from "./service.js";
      vi.mock("./service.js", () => ({ default: { mocked: true }, read: vi.fn(() => 7) }));
      test("mocked", () => load());
    `);

    expect(transformed).toContain(`__rushRegisterMock__("./service.js", () => ({ default: { mocked: true }, read: vi.fn(() => 7) }));`);
    expect(transformed).toContain(`const { default: service, read: load } = await __rushImport__("./service.js"`);
    expect(transformed.indexOf("__rushRegisterMock__")).toBeLessThan(transformed.indexOf("const { default: service"));
    expect(transformed).not.toContain(`vi.mock("./service.js"`);
  });

  test("leaves modules without static mocks unchanged", async () => {
    const source = `import { test } from "@rush/browser"; test("ok", () => {});`;
    expect(await transformHoistedMocks(source)).toBe(source);
  });
});
