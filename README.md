# Rush

Rush is a persistent WebView-native JavaScript and TypeScript test runner. Tests execute in a real browser engine, while a native host keeps the browser and incremental esbuild graph warm between runs.

Rush is private and under architectural validation. `rush` and `@rush/browser` are working names, not published distribution contracts. Build from a pinned private repository revision; do not publish either name or make downstream release automation depend on it yet.

## What works on this revision

| Surface | Status |
| --- | --- |
| Linux WebKitGTK browser runner | Available through `./bin/rush` in headless or headed mode |
| Linux WPE headless runner | Opt-in source build; no headed debugging |
| macOS WKWebView adapter | Available as a Swift package and validation harness; not wired to the Go CLI |
| Windows WebView2 adapter | Validated in project delivery work, but not present on this revision |
| Browser API | Available through the private `@rush/browser` working package |
| App and isolated-session models | Public API contracts exist; native implementations are pending integration |
| Watch, reporters, and failure artifacts | Adapter-independent host contracts are tested; not wired to the Linux proof CLI |

The distinction matters: a compiled package or passing adapter harness is not the same as an end-user CLI capability. See [Compatibility and platform status](docs/compatibility.md) before choosing Rush for a suite.

## Quick start on Linux

Install Go 1.24 or newer, Node.js 22, WebKitGTK 4.1, GTK 3, Xvfb, and Xauthority support. On Debian or Ubuntu:

```sh
sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0 xvfb xauth
npm ci
make build
./bin/rush doctor
./bin/rush test examples/basic.test.ts
```

Ubuntu releases using the 64-bit time ABI may call the GTK package `libgtk-3-0t64`; `libwebkit2gtk-4.1-0` normally resolves the correct runtime dependency.

Rush starts an authenticated Xvfb display for headless runs and keeps its daemon warm. Headed mode requires an existing `DISPLAY` or `WAYLAND_DISPLAY`:

```sh
./bin/rush test --headed examples/browser-api.test.ts
./bin/rush stop
```

No registry installation is supported while naming remains provisional. Consumer projects should build a pinned private revision and point `RUSH_BROWSER_MODULE` at that revision's `dist/index.js` when the adjacent package cannot be resolved.

## Test example

```ts
import { expect, test } from "@rush/browser";

test("updates the real DOM", ({ page }) => {
  document.body.innerHTML = `<button type="button">Save</button>`;
  const button = page.getByRole("button", { name: "Save" });

  button.click();
  expect(button.element()).toBeInTheDocument();
});
```

Each suite is bundled independently and must import the APIs it uses. The page runtime supplies Vitest-like suites, hooks, assertions, mocks, fake timers, snapshots, Testing Library queries, locators, and explicitly separate synthetic and trusted-native interactions.

## Commands available in the Linux proof

```sh
./bin/rush test [--headed] [--json] [--timeout 30s] FILE...
./bin/rush bench [--repeat 5] [--cold-repeat 3] [--json]
./bin/rush doctor
./bin/rush stop [--headed]
```

Shell globs and multiple files are accepted. Headless and headed daemons are separate so a debug run cannot silently reuse the hidden browser.

The adapter-independent command package also defines the intended `run`, `watch`, and `debug` surface plus build, reporter, and artifact options. Those options are documented as host integration contracts, not as flags accepted by `./bin/rush` today.

## Documentation

- [Installation and configuration](docs/installation.md)
- [Commands and browser API](docs/usage.md)
- [Browser, app, and session tests](docs/test-models.md)
- [Reporters, failure artifacts, and CI](docs/ci-reporting.md)
- [Benchmark methodology and results](docs/benchmarks.md)
- [Compatibility and platform status](docs/compatibility.md)
- [Workspace migration guidance](docs/migration.md)
- [WPE WebKit evaluation](docs/wpe-evaluation.md)

## Verification

```sh
npm ci
npm run check
npm test
go test ./...
go test -race ./...
make build
./bin/rush test examples/basic.test.ts examples/browser-api.test.ts examples/javascript.test.js examples/react.test.tsx
./bin/rush bench --repeat 5 --cold-repeat 3 --json
swift test # macOS only
```

GitHub Actions runs the core Go and TypeScript checks, a real WebKitGTK/Xvfb example run, the measured benchmark contract, and the macOS WKWebView harness. Benchmark JSON is retained as a workflow artifact so a pass can be audited from its raw samples.
