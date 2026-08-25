import { describe, expect, test } from "vitest";
import { transformHoistedMocks } from "../src/index.js";

describe("static vi.mock hoisting", () => {
  test("registers mocks before rewriting imports to page-local loading", async () => {
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
