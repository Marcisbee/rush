import { afterEach, beforeEach, describe, expect as assert, test } from "vitest";
import {
  configureSnapshots,
  expect,
  getSnapshotValues,
  vi as rushVi,
} from "../src/index.js";
import { enterSnapshotTest, leaveSnapshotTest } from "../src/snapshots.js";

beforeEach(() => {
  configureSnapshots();
});

afterEach(() => {
  rushVi.restoreAllMocks();
  rushVi.useRealTimers();
  leaveSnapshotTest();
});

describe("expect compatibility", () => {
  test("supports common, asymmetric, promise, and DOM matchers", async () => {
    const node = document.createElement("button");
    node.textContent = "Save changes";
    document.body.append(node);

    expect({ value: 2, label: "saved" }).toEqual({ value: 2, label: expect.stringContaining("save") });
    expect([1, 2, 3]).toEqual(expect.arrayContaining([3, 1]));
    expect(() => { throw new Error("boom"); }).toThrow(/boom/);
    expect(node).toBeInTheDocument();
    expect(node).toHaveTextContent("Save");
    await expect(Promise.resolve(4)).resolves.toBeGreaterThan(3);
    await expect(Promise.reject(new Error("nope"))).rejects.toBeInstanceOf(Error);
  });

  test("records and compares snapshots by test name", () => {
    enterSnapshotTest("suite > snapshot");
    expect({ beta: 2, alpha: 1 }).toMatchSnapshot();
    assert(getSnapshotValues()).toEqual({
      "suite > snapshot > 1": "{\"alpha\": 1, \"beta\": 2}",
    });

    configureSnapshots({ values: getSnapshotValues(), update: "none" });
    enterSnapshotTest("suite > snapshot");
    expect({ alpha: 1, beta: 2 }).toMatchSnapshot();
  });
});

describe("vi compatibility", () => {
  test("tracks calls and one-time implementations", () => {
    const mock = rushVi.fn((value: number) => value * 2)
      .mockImplementationOnce(() => 99);
    assert(mock(2)).toBe(99);
    assert(mock(3)).toBe(6);
    expect(mock).toHaveBeenCalledTimes(2);
    expect(mock).toHaveBeenLastCalledWith(3);
  });

  test("spies and restores methods", () => {
    const target = { greet(name: string) { return `hello ${name}`; } };
    const original = target.greet;
    const spy = rushVi.spyOn(target, "greet");
    assert(target.greet("Ada")).toBe("hello Ada");
    assert(spy.mock.calls).toEqual([["Ada"]]);
    spy.mockRestore();
    assert(target.greet).toBe(original);
  });

  test("runs browser timers without waiting for wall time", () => {
    rushVi.useFakeTimers({ now: new Date("2026-01-01T00:00:00Z") });
    const callback = rushVi.fn();
    setTimeout(callback, 500);
    rushVi.advanceTimersByTime(499);
    assert(callback).not.toHaveBeenCalled();
    rushVi.advanceTimersByTime(1);
    expect(callback).toHaveBeenCalled();
    assert(Date.now()).toBe(new Date("2026-01-01T00:00:00.500Z").getTime());
  });

  test("resolves registered static module factories in page", async () => {
    rushVi.mock("./service.js", () => ({ answer: rushVi.fn(() => 42) }));
    const service = await rushVi.importMock("./service.js", async () => ({ answer: () => 0 }));
    assert(service.answer()).toBe(42);
    expect(service.answer).toHaveBeenCalled();
  });
});
