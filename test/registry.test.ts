import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  configureSnapshots,
  describe,
  expect,
  getSnapshotValues,
  test,
} from "@rush/browser";

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
