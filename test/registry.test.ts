import { beforeEach as vitestBeforeEach, describe as vitestDescribe, expect as assert, test as vitestTest, vi as vitestVi } from "vitest";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  configureRuntime,
  describe,
  resetRegistry,
  run,
  test,
} from "../src/index.js";

vitestBeforeEach(() => {
  resetRegistry();
  configureRuntime({});
  document.body.innerHTML = "";
});

vitestDescribe("suite registry", () => {
  vitestTest("runs nested hooks and parameterized tests in page order", async () => {
    const events: string[] = [];
    describe("math", () => {
      beforeAll(() => { events.push("beforeAll"); });
      afterAll(() => { events.push("afterAll"); });
      beforeEach(() => { events.push("beforeEach"); });
      afterEach(() => { events.push("afterEach"); });
      test.each([[1, 2, 3], [2, 3, 5]] as const)("%i + %i = %i", (left, right, total) => {
        events.push(`${left + right}:${total}`);
      });
      test.skip("later", () => { events.push("never"); });
      test.todo("eventually");
    });

    const result = await run({ emit: false });

    assert(result.passed).toBe(2);
    assert(result.skipped).toBe(1);
    assert(result.todo).toBe(1);
    assert(events).toEqual([
      "beforeAll",
      "beforeEach", "3:3", "afterEach",
      "beforeEach", "5:5", "afterEach",
      "afterAll",
    ]);
  });

  vitestTest("honors only selection", async () => {
    const called = vitestVi.fn();
    const unrelatedHook = vitestVi.fn();
    describe("unrelated", () => {
      beforeAll(unrelatedHook);
      test("ordinary", called);
    });
    test.only("focused", called);
    const result = await run({ emit: false });
    assert(result.tests.map(({ name, state }) => [name, state])).toEqual([
      ["ordinary", "skipped"],
      ["focused", "passed"],
    ]);
    assert(called).toHaveBeenCalledTimes(1);
    assert(unrelatedHook).not.toHaveBeenCalled();
  });

  vitestTest("batches results through the runtime adapter", async () => {
    const emitResults = vitestVi.fn();
    configureRuntime({ emitResults });
    test("works", () => {});
    await run();
    assert(emitResults).toHaveBeenCalledOnce();
    assert(emitResults.mock.calls[0]?.[0]).toHaveLength(1);
  });
});

vitestDescribe("test models", () => {
  vitestTest("provides browser and app contexts", async () => {
    const navigations: string[] = [];
    configureRuntime({ navigate: async (url) => { navigations.push(url); } });
    test.browser("browser", ({ model, document: testDocument }) => {
      assert(model).toBe("browser");
      assert(testDocument).toBe(document);
    });
    test.app("app", async (context) => {
      assert(context.model).toBe("app");
      if (context.model === "app") await context.goto("https://example.test/account");
    });

    const result = await run({ emit: false });
    assert(result.failed).toBe(0);
    assert(navigations).toEqual(["https://example.test/account"]);
  });

  vitestTest("creates named isolated session clients and disposes them", async () => {
    const disposed: string[] = [];
    configureRuntime({
      createSession: async (names) => names.map((name) => {
        const root = document.createElement("section");
        root.innerHTML = `<button>${name}</button>`;
        return {
          name,
          root,
          url: () => `https://${name}.test/`,
          goto: async () => {},
          evaluate: async <T>(callback: () => T | Promise<T>) => callback(),
          dispose: () => { disposed.push(name); },
        };
      }),
    });
    test.session({ clients: ["alice", "bob"] })("realtime", (context) => {
      assert(context.model).toBe("session");
      if (context.model !== "session") return;
      assert(context.client("alice").page.getByRole("button").textContent()).toBe("alice");
      assert(context.client(1).name).toBe("bob");
    });

    const result = await run({ emit: false });
    assert(result.failed).toBe(0);
    assert(disposed).toEqual(["alice", "bob"]);
  });
});
