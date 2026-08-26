import { afterAll, beforeAll, describe, expect, test, vi } from "rush-webtest";

const selected = vi.fn(() => {});
const unrelatedHook = vi.fn(() => {});

describe("unrelated suite", () => {
  beforeAll(unrelatedHook);
  test("ordinary test", selected);
});

test.only("focused test", selected);

afterAll(() => {
  expect(selected).toHaveBeenCalledTimes(1);
  expect(unrelatedHook).not.toHaveBeenCalled();
});
