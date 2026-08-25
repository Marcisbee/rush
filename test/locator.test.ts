import { beforeEach, describe, expect, test, vi } from "vitest";
import { configureRuntime, createPage, native, screen } from "../src/index.js";

beforeEach(() => {
  document.body.innerHTML = `
    <label>Name <input data-testid="name"></label>
    <button type="button">Save changes</button>
    <input type="checkbox" aria-label="Subscribe">
  `;
  configureRuntime({});
});

describe("in-page locators", () => {
  test("queries by role, text, and test id", () => {
    const page = createPage();
    expect(page.getByRole("button", { name: "Save changes" }).textContent()).toBe("Save changes");
    expect(page.getByText("Name").count()).toBe(1);
    expect(page.getByTestId("name").element()).toBeInstanceOf(HTMLInputElement);
    expect(screen.getByRole("button", { name: "Save changes" })).toBeInstanceOf(HTMLButtonElement);
  });

  test("performs fast synthetic input without claiming trusted events", () => {
    const page = createPage();
    const input = page.getByTestId("name");
    const trust: boolean[] = [];
    input.element().addEventListener("input", (event) => trust.push(event.isTrusted));
    input.fill("Ada");
    input.type(" Lovelace");
    expect((input.element() as HTMLInputElement).value).toBe("Ada Lovelace");
    expect(trust.every((value) => value === false)).toBe(true);
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

  test("keeps trusted automation on an explicit adapter path", async () => {
    const page = createPage();
    await expect(native.click(page.getByRole("button"))).rejects.toThrow("Trusted native automation is unavailable");

    const click = vi.fn(async () => {});
    configureRuntime({ native: { click, type: async () => {}, press: async () => {} } });
    await native.click(page.getByRole("button"));
    expect(click).toHaveBeenCalledWith(page.getByRole("button").element());
  });
});
