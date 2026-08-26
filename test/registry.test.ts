import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  configureSnapshots,
  describe,
  expect,
  getSnapshotValues,
  it,
  test,
} from "rush-webtest";

describe("suite registry", () => {
  const events: string[] = [];

  beforeAll(() => { events.push("beforeAll"); });
  beforeEach(() => { events.push("beforeEach"); });
  afterEach(() => { events.push("afterEach"); });
  afterAll(() => {
    events.push("afterAll");
    expect(events).toEqual([
      "beforeAll",
      "beforeEach", "3:3", "afterEach",
      "beforeEach", "5:5", "afterEach",
      "afterAll",
    ]);
  });

  test.each([[1, 2, 3], [2, 3, 5]] as const)("%i + %i = %i", (left, right, total) => {
    events.push(`${left + right}:${total}`);
    expect(left + right).toBe(total);
  });
  test.skip("skips callbacks", () => { events.push("never"); });
  test.todo("reports unfinished tests");
});

describe("snapshot registry", () => {
  test("compares deterministic values by test name", () => {
    const key = "snapshot registry > compares deterministic values by test name > 1";
    configureSnapshots({
      values: { [key]: '{"alpha": 1, "beta": 2}' },
      update: "none",
    });

    expect({ beta: 2, alpha: 1 }).toMatchSnapshot();
    expect(getSnapshotValues()).toEqual({
      [key]: '{"alpha": 1, "beta": 2}',
    });
  });
});

describe("parameterized row shapes", () => {
  const received: unknown[] = [];

  afterAll(() => {
    expect(received).toEqual([
      null,
      "ready",
      { label: "object", value: 42 },
      "recording.webm",
      "suite",
    ]);
  });

  it.each([null, "ready"] as const)("passes scalar row %s", (value) => {
    received.push(value);
  });

  test.each([{ label: "object", value: 42 }] as const)("passes $label row", (value) => {
    received.push(value);
  });

  test.each([new File(["video"], "recording.webm")])("passes File row $name", (file) => {
    received.push(file.name);
  });

  describe.each([{ label: "suite" }] as const)("$label row suite", (value) => {
    test("receives one object argument", () => {
      received.push(value.label);
    });
  });
});
