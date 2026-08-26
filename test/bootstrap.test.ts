import {
  beforeEach as vitestBeforeEach,
  describe as vitestDescribe,
  expect as assert,
  test as vitestTest,
  vi as vitestVi,
} from "vitest";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  configureRuntime,
  configureSnapshots,
  createPage,
  describe,
  expect as rushExpect,
  getSnapshotValues,
  native,
  resetRegistry,
  run,
  test,
} from "../src/index.js";
import { enterSnapshotTest, leaveSnapshotTest } from "../src/snapshots.js";

vitestBeforeEach(() => {
  resetRegistry();
  configureRuntime({});
  configureSnapshots();
  document.body.innerHTML = "";
});

vitestDescribe("suite bootstrap", () => {
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

vitestDescribe("runtime adapter injection", () => {
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

  vitestTest("uses a fresh application realm and disposes it after each app test", async () => {
    const disposed: number[] = [];
    let sequence = 0;
    configureRuntime({
      createApp: () => {
        const id = ++sequence;
        const frame = document.createElement("iframe");
        document.body.append(frame);
        const testDocument = frame.contentDocument as Document;
        testDocument.title = `app-${id}`;
        testDocument.body.innerHTML = `<button>realm ${id}</button>`;
        return {
          window: () => frame.contentWindow as Window,
          document: () => testDocument,
          url: () => `https://app-${id}.test/`,
          goto: async () => {},
          network: {
            route: () => () => {},
            requests: () => [],
            waitForRequest: async () => { throw new Error("unused"); },
          },
          dispose: () => { disposed.push(id); frame.remove(); },
        };
      },
    });
    test.app("first", ({ page, document: testDocument }) => {
      assert(testDocument.title).toBe("app-1");
      assert(page.getByRole("button").textContent()).toBe("realm 1");
    });
    test.app("second", ({ page, url }) => {
      assert(url()).toBe("https://app-2.test/");
      assert(page.getByRole("button").textContent()).toBe("realm 2");
    });

    const result = await run({ emit: false });

    assert(result.failed).toBe(0);
    assert(disposed).toEqual([1, 2]);
  });

  vitestTest("matches DOM state across browser and application realms", async () => {
    configureRuntime({
      createApp: () => {
        const frame = document.createElement("iframe");
        document.body.append(frame);
        const testDocument = frame.contentDocument as Document;
        testDocument.body.innerHTML = `<button data-state="ready" disabled>Save changes</button><input value="Ada">`;
        return {
          window: () => frame.contentWindow as Window,
          document: () => testDocument,
          url: () => "https://app.test/",
          goto: async () => {},
          network: {
            route: () => () => {},
            requests: () => [],
            waitForRequest: async () => { throw new Error("unused"); },
          },
          dispose: () => { frame.remove(); },
        };
      },
    });
    const verifyDomState = (button: Element, input: Element) => {
      rushExpect(button).toBeInTheDocument();
      rushExpect(button).toHaveTextContent("Save");
      rushExpect(button).toHaveAttribute("data-state", "ready");
      rushExpect(button).toBeVisible();
      rushExpect(button).toBeDisabled();
      rushExpect(input).toHaveValue("Ada");
    };
    document.body.innerHTML = `<button data-state="ready" disabled>Save changes</button><input value="Ada">`;
    test.browser("browser", ({ page }) => {
      verifyDomState(page.getByRole("button").element(), page.getByRole("textbox").element());
    });
    test.app("app", ({ page }) => {
      verifyDomState(page.getByRole("button").element(), page.getByRole("textbox").element());
    });

    const result = await run({ emit: false });

    assert(result.tests.map(({ model, state }) => [model, state])).toEqual([
      ["browser", "passed"],
      ["app", "passed"],
    ]);
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

  vitestTest("keeps trusted automation behind the injected adapter", async () => {
    document.body.innerHTML = `<button type="button">Save</button>`;
    const page = createPage();
    await assert(native.click(page.getByRole("button"))).rejects.toThrow("Trusted native automation is unavailable");

    const click = vitestVi.fn(async () => {});
    configureRuntime({ native: { click, type: async () => {}, press: async () => {} } });
    await native.click(page.getByRole("button"));
    assert(click).toHaveBeenCalledWith(page.getByRole("button").element());
  });
});

vitestDescribe("snapshot bootstrap", () => {
  vitestTest("records and compares snapshots by test name", () => {
    enterSnapshotTest("suite > snapshot");
    rushExpect({ beta: 2, alpha: 1 }).toMatchSnapshot();
    assert(getSnapshotValues()).toEqual({
      "suite > snapshot > 1": "{\"alpha\": 1, \"beta\": 2}",
    });

    configureSnapshots({ values: getSnapshotValues(), update: "none" });
    enterSnapshotTest("suite > snapshot");
    rushExpect({ alpha: 1, beta: 2 }).toMatchSnapshot();
    leaveSnapshotTest();
  });
});
