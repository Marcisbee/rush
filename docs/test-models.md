# Test models and trusted automation

Rush separates three test models because they need different isolation and browser control. Choosing the smallest sufficient model keeps tests fast and makes unavailable native capabilities fail explicitly.

| Model | Use for | Isolation boundary | Native adapter required |
| --- | --- | --- | --- |
| Browser | Synchronous units, DOM components, accessibility queries, mocks, timers | Fresh bundle registry and browser reset per file | No |
| App | Real navigation, HTTP, origin storage, service workers, application flows | Application realm and origin state per test | Yes |
| Session | Realtime or multi-user behavior with independent clients | Named browser realm/profile per client | Yes |

All three models execute through the Linux WebKitGTK CLI on this revision. App and session tests use coarse native capabilities and keep DOM work inside their browser pages. A missing native capability throws instead of silently falling back to weaker browser-only behavior.

## Browser tests

Use browser tests for behavior that can run in the runner page:

```ts
import { expect, test } from "@rush/browser";

test.browser("renders a component state", ({ document, page }) => {
  document.body.innerHTML = `<output aria-label="Status">ready</output>`;
  expect(page.getByRole("status", { name: "Status" }).textContent()).toBe("ready");
});
```

Browser tests can use the real DOM, Selection, same-origin iframes, Shadow DOM roots, Testing Library queries, synthetic events, mocks, fake timers, and browser storage. They should not start application servers or assume that a synthetic event exercises browser-default editing behavior.

## App tests

Use app tests when navigation and origin behavior are part of the assertion:

```ts
import { expect, test } from "@rush/browser";

test.app("loads the account route", async ({ goto, page, window }) => {
  await goto("http://127.0.0.1:4173/account");
  expect(window.location.pathname).toBe("/account");
  expect(page.getByRole("heading", { name: "Account" }).textContent()).toBe("Account");
});
```

The Linux app adapter preserves the requested path, query, and fragment while keeping the controller bridge alive. It provides isolated cookies and local/session storage, request routing with fulfill/continue/abort behavior, request inspection, and trusted input. Application proxy, network, intentional-wait, and runner time are attributed separately.

Start application servers outside Rush and use loopback URLs with deterministic fixtures. Keep backend startup and seeding out of measured runner overhead. A test that only renders a component belongs in the browser model even if the component normally appears in an application.

## Session tests

Session tests declare either a count or stable client names:

```ts
import { expect, test } from "@rush/browser";

test.session({ clients: ["alice", "bob"] })("isolates clients", async ({ client }) => {
  const alice = client("alice");
  const bob = client("bob");

  await Promise.all([
    alice.goto("http://127.0.0.1:4173/room"),
    bob.goto("http://127.0.0.1:4173/room"),
  ]);

  await alice.evaluate(() => localStorage.setItem("identity", "alice"));
  expect(await bob.evaluate(() => localStorage.getItem("identity"))).toBeNull();
});
```

Named clients remain stable for the test and expose `page`, `url`, `goto`, and `evaluate`. The Linux session runtime pools a bounded number of WebViews, gives clients independent WebKit profiles, storage, and browser state, and resets them before reuse.

Use session tests only when concurrent browser identity is observable. Do not model ordinary parallel assertions as clients, and do not share application state through the test-realm closure when the behavior is meant to cross a real server or WebSocket.

## Synthetic versus trusted input

Synthetic locator operations dispatch DOM events in the page. They are the preferred path for ordinary component behavior because they avoid native bridge round trips.

Trusted automation uses the explicit `native` API or locator-native methods:

```ts
import { expect, native, test, waitFor } from "@rush/browser";

test.app("uses browser-default editing", async ({ goto, page }) => {
  await goto("http://127.0.0.1:4173/editor");
  const editor = page.getByRole("textbox");

  await native.click(editor);
  await native.type(editor, "hello");
  await native.press("Enter", editor);

  await waitFor(() => expect(editor.textContent()).toContain("hello"));
});
```

Use trusted input for:

- `beforeinput`, selection changes, and browser-default editing.
- Focus, accelerators, or keyboard behavior whose correctness depends on the operating system.
- Assertions that explicitly require `Event.isTrusted === true`.

Do not use it merely to imitate a click. Native input can move the pointer, change focus, require permissions, and serialize otherwise fast work.

On Linux, keep trusted-input files in a separate invocation with `RUSH_WEBVIEW_POOL_SIZE=1`. X11 focus is process-global, so concurrent pooled realm or session navigation can steal the active native target even though browser-only work remains safely parallel.

Platform requirements differ:

- Linux WebKitGTK uses XTest on the active X11 display; the CLI wires it through the explicit native API.
- macOS uses Core Graphics and requires Accessibility authorization in a logged-in GUI session.
- Windows uses `SendInput`, requires an interactive desktop, and cannot run from session-0 services.
- WPE's headless backend has no trusted desktop-input path.

Rush never turns a synthetic interaction into a claimed trusted event. If a native adapter is not configured, trusted calls fail with a capability error.

## Cleanup expectations

Browser suite cleanup covers the DOM, styles, timers, animation frames, listeners registered through the page, cookies, local/session storage, IndexedDB, Cache Storage, service workers, performance entries, globals, registry state, mocks, and fake timers.

Do not rely on automatic cleanup of browser permissions, downloads, external processes, cross-origin state outside Rush's app proxy or session profiles, or operating-system state. Verify the exact surfaces a project modifies before removing its former runner.
