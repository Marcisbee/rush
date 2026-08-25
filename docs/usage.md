# Commands and browser API

## Shipped Linux CLI

The source-built Linux proof accepts these commands:

```text
rush test [--headed] [--json] [--timeout DURATION] FILE...
rush bench [--repeat N] [--cold-repeat N] [--json]
rush doctor
rush stop [--headed]
```

Examples:

```sh
./bin/rush test examples/basic.test.ts
./bin/rush test examples/browser-api.test.ts examples/javascript.test.js
./bin/rush test --json 'examples/*.test.ts'
./bin/rush test --timeout 45s --headed examples/react.test.tsx
./bin/rush stop
```

`test` expands shell-style file globs, removes duplicate paths, and rejects directories. The timeout applies separately to each suite. Normal output lists failures, per-suite timing phases, result counts, request wall time, and cold startup when the command started the browser. `--json` emits the native response instead.

`doctor` validates the selected WebView backend. `stop` stops the headless daemon; `stop --headed` stops the separate debug daemon.

The packages under `command`, `app`, `watch`, `reporter`, `artifact`, and `execution` define a broader host integration surface. Their `run`, `watch`, `debug`, reporter, build-plugin, and artifact options are tested contracts, but `cmd/rush` does not accept them yet. Do not add those flags to consumer scripts until the native CLI integration lands.

## Registering tests

Suites execute in a browser page and import what they use:

```ts
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  page,
  test,
  vi,
} from "@rush/browser";

describe("counter", () => {
  beforeEach(() => {
    document.body.innerHTML = `<button type="button">0</button>`;
  });

  afterEach(() => vi.restoreAllMocks());

  test.each([[1], [2]])("increments by %i", (amount) => {
    const button = page.getByRole("button");
    button.element().textContent = String(amount);
    expect(button.textContent()).toBe(String(amount));
  });
});
```

Supported suite primitives include `test`/`it`, `describe`/`suite`, `beforeAll`, `afterAll`, `beforeEach`, `afterEach`, `.each`, `.skip`, `.only`, and `.todo`. `test.browser`, `test.app`, and `test.session` select the execution model described in [Test models and trusted automation](test-models.md).

## Assertions and snapshots

Rush provides Vitest-like equality, truthiness, type, collection, numeric, property, throw, object, DOM, mock-call, and snapshot matchers. `.not`, `.resolves`, `.rejects`, asymmetric matchers, `expect.extend`, `toMatchSnapshot`, and `toMatchInlineSnapshot` are available.

Snapshot files are not implicitly managed by the Linux proof CLI. Hosts configure snapshot values and update mode through `configureSnapshots`; migrations should verify the chosen host's persistence behavior before replacing an existing snapshot command.

## Mocks and timers

`vi.fn`, `vi.spyOn`, mock implementation and return helpers, clear/reset/restore helpers, fake timers, system-time control, and statically hoisted `vi.mock` are supported. Rush transforms statically analyzable mocks before esbuild executes a suite. Dynamic module patterns that depend on Vitest's Node process or plugin container must be rewritten or kept in Vitest.

Fake timers are restored after each test. File isolation resets the registry and mock runtime independently for every bundled suite.

## Queries, locators, and interactions

Testing Library DOM queries are re-exported. `page` and the per-test `page` context provide locators for role, text, label, placeholder, and test id. Locator methods include querying descendants, reading text or attributes, and synthetic `click`, `type`, `fill`, and keyboard dispatch.

```ts
import { expect, test } from "@rush/browser";

test("submits locally", ({ page }) => {
  document.body.innerHTML = `
    <label>Name <input></label>
    <button type="button">Save</button>
  `;

  const name = page.getByRole("textbox", { name: "Name" });
  name.fill("Ada");
  page.getByRole("button", { name: "Save" }).click();

  expect((name.element() as HTMLInputElement).value).toBe("Ada");
});
```

Synthetic calls are intentionally fast and remain inside the page. They do not claim `Event.isTrusted === true`. Use the explicit native path only for behavior that truly depends on operating-system input.

## Isolation and timing

Linux assigns files deterministically across a bounded warm WebView pool. Before a realm's next suite, it clears the DOM, injected style nodes, timers, animation frames, registered event listeners, cookies, local and session storage, IndexedDB, Cache Storage, service workers, performance entries, and globals added by the previous bundle. A fresh registry and mock runtime are scoped to every file. App tests also reset routing and application-frame state; named session clients use independent WebKit profiles and are scrubbed before reuse.

The native response reports:

- `build`: host-side incremental esbuild work.
- `runner`: page registration and orchestration outside hooks and test callbacks.
- `application`: callback wall time after observed network duration and completed requested timer delays are subtracted.
- `network`: WebKit resource-timing duration observed during the suite.
- `intentional wait`: requested delays for timers that fired during a test.
- `page total`: registration and test execution inside WebKit.

JSON output also reports request-level bundle, native-host, bridge, browser-execution, reset, and reporting timings, plus the number of active browser realms.

Network and timers can overlap, so attribution phases are diagnostic signals rather than an accounting identity. Build time is never folded into page time.
