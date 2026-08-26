# Rush

Rush is a WebView-native JavaScript and TypeScript test runner. One-shot commands clean up their native host before exiting; watch mode keeps the browser and incremental esbuild graph warm until interrupted.

Rush's browser API is published on npm as `rush-webtest`. Matching Linux and macOS native hosts are attached to each GitHub release.

## What works on this revision

| Surface | Status |
| --- | --- |
| Linux WebKitGTK runner | Browser, app, and isolated-session tests through `./bin/rush` in headless or headed mode |
| Linux trusted input | X11/XTest mouse and keyboard input through the explicit native API |
| Linux WPE runner | Opt-in headless browser runner; no headed debugging or trusted desktop input |
| macOS WKWebView runner | In-process Objective-C/cgo WKWebView adapter through `./bin/rush`, hidden or headed |
| Windows WebView2 adapter | Go adapter and validation harness; not wired to the Go CLI |
| Browser API | Vitest-like suites, assertions, mocks, fake timers, snapshots, Testing Library queries, and locators |
| Execution | Bounded parallel browser realms, app navigation/interception, and isolated named clients |
| Host contracts | Dependency-aware watch mode is exposed by the CLI; reporters and failure artifacts remain tested package contracts |

The distinction matters: a compiled adapter or passing contract test is not automatically an end-user CLI capability. See [Compatibility and platform status](docs/compatibility.md) before choosing Rush for a suite.

## Quick start

Install the browser API in the project that owns the tests:

```sh
npm install --save-dev rush-webtest
```

Download the matching Linux archive from the same GitHub release, or build the native host from that revision. Source builds require Go 1.24 or newer and Node.js 22. The Linux host also needs WebKitGTK 4.1, GTK 3, X11/XTest, Xvfb, and Xauthority support. On Debian or Ubuntu:

```sh
sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0 libxtst6 xvfb xauth
npm ci
make build
./bin/rush doctor
./bin/rush test examples/basic.test.ts examples/browser-api.test.ts
RUSH_WEBVIEW_POOL_SIZE=1 ./bin/rush test examples/app-automation.test.ts
./bin/rush test examples/session.test.ts
```

Ubuntu releases using the 64-bit time ABI may call the GTK package `libgtk-3-0t64`; installing `libwebkit2gtk-4.1-0` normally resolves the correct runtime dependency.

On macOS 13 or newer, the same build produces one native executable using Apple system frameworks. Xcode Command Line Tools are required for their C/Objective-C compiler and macOS SDK; Swift is not:

```sh
xcode-select --install # if the command-line tools are not installed
npm ci
make build
./bin/rush doctor
./bin/rush test examples/basic.test.ts examples/browser-api.test.ts
```

Rush starts an authenticated Xvfb display for each headless command and removes it when the command exits. Headed mode requires an existing `DISPLAY` or `WAYLAND_DISPLAY`:

```sh
./bin/rush test --headed examples/browser-api.test.ts
```

The native binary embeds the matching `rush-webtest` browser runtime, while the npm package supplies TypeScript declarations and editor resolution. `RUSH_BROWSER_MODULE` remains available for local development against an unpublished browser API build.

## Test models

Browser tests run assertions, DOM queries, mocks, timers, and synthetic interactions entirely inside reusable real-browser realms:

```ts
import { expect, test } from "rush-webtest";

test("updates the real DOM", ({ page }) => {
  document.body.innerHTML = `<button type="button">Save</button>`;
  const button = page.getByRole("button", { name: "Save" });

  button.click();
  expect(button.element()).toBeInTheDocument();
});
```

App tests use real navigation through Rush's loopback proxy, request interception, per-test storage cleanup, and explicit native input. Session tests use `test.session({ clients: [...] })` and give every named client an independent WebKit profile. See [Test models and trusted automation](docs/test-models.md).

## Isolation and performance

Hidden Linux and macOS commands use up to three browser realms by default, but never create more realms than the selected test files require; `RUSH_WEBVIEW_POOL_SIZE` selects one through four realms explicitly. Watch mode reuses those realms between reruns. The CLI builds while the native browser starts, and immutable Rush framework code is loaded once per realm instead of being copied into every suite. Each file still receives its own esbuild bundle, application module graph, registry, and mock runtime. Before reuse, Rush clears DOM and style state, timers, listeners, cookies, local and session storage, IndexedDB, Cache Storage, service workers, performance entries, and bundle globals.

The benchmark command validates pass counts and the declared product targets from raw repeated samples:

```sh
./bin/rush bench
./bin/rush bench --repeat 10 --cold-repeat 5 --json
```

Normal and JSON output report build, runner, application, network, intentional-wait, browser-execution, reset, and reporting measurements separately. Network and timers can overlap, so the phases are attribution signals rather than an accounting identity. See [Benchmark methodology and measured boundaries](docs/benchmarks.md).

## Commands

```text
rush test [--watch] [--headed] [--verbose] [--json] [--timeout DURATION] FILE...
rush bench [--repeat N] [--cold-repeat N] [--json]
rush doctor
```

Shell globs and multiple files are accepted. The default console output groups individual results by file and prints a compact pass/fail summary. `--verbose` adds the detailed build, browser, and startup timing phases. `--watch` performs an initial run, watches the complete esbuild input set, and reruns with the same command-scoped browser host until `Ctrl+C`. The adapter-independent command packages also define the intended `run`, `debug`, reporter, and artifact surface, but those options are not accepted by `./bin/rush` yet.

## Documentation

- [Installation and configuration](docs/installation.md)
- [Commands and browser API](docs/usage.md)
- [Browser, app, and session tests](docs/test-models.md)
- [Reporters, failure artifacts, and CI](docs/ci-reporting.md)
- [Benchmark methodology and results](docs/benchmarks.md)
- [Compatibility and platform status](docs/compatibility.md)
- [Publishing releases](docs/releasing.md)
- [Windows WebView2 setup and validation](docs/windows-webview2.md)
- [WPE WebKit evaluation](docs/wpe-evaluation.md)
- [macOS WKWebView build and validation](docs/macos-wkwebview.md)

## Repository layout

`src/` is the browser package. `internal/rush/` is the Linux and macOS CLI runtime, while `internal/wkwebview/`, `platform/webview2/`, and `native/wpe/` contain the platform adapters. The small top-level Go packages define adapter-independent commands, execution, reporting, artifacts, and watch contracts. Product examples live in `examples/`, measured workloads in `benchmarks/`, and tests in `test/`.

Rush runs every TypeScript test in `test/` through its own real WebView. The self-tests exercise the public runner contract; native app and session behavior is covered by the corresponding real-adapter examples.

## Verification

```sh
npm ci
make test
./bin/rush test examples/basic.test.ts examples/browser-api.test.ts examples/javascript.test.js examples/react.test.tsx
RUSH_WEBVIEW_POOL_SIZE=1 ./bin/rush test examples/app-automation.test.ts
./bin/rush test examples/session.test.ts
./bin/rush bench --repeat 5 --cold-repeat 3 --json
go build ./cmd/rush # uses Objective-C/cgo automatically on macOS
```

GitHub Actions runs the Go race and TypeScript checks in parallel with the real WebKitGTK/Xvfb self-test, examples, and measured benchmark contract. Path-filtered workflows validate the Windows WebView2 and macOS WKWebView adapters only when their implementations or dependencies change. The macOS job also rejects Swift and external WebView library linkage. Benchmark JSON is retained as a workflow artifact so a pass can be audited from its raw samples.
