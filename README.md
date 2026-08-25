# Rush

Rush is a persistent WebView-native JavaScript and TypeScript test runner. This repository currently contains the first Linux proof: a native Go daemon keeps WebKitGTK and incremental esbuild contexts alive between CLI runs, while tests register and execute inside the page.

The repository is under active development. The `@rush/browser` package contains the public, Vitest-compatible API that executes inside a real browser page. Native hosts provide navigation, isolated sessions, and trusted input through explicit adapters; ordinary assertions, queries, mocks, timers, snapshots, and synthetic interactions stay in the page.

## Linux prerequisites

The checked-in Go dependency embeds the small native WebView adapter, but the operating system must provide GTK 3, WebKitGTK 4.1, and Xvfb. Rush reports the native loader error and names WebKitGTK when those libraries are absent.

Debian or Ubuntu:

```sh
sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0 xvfb xauth
```

Ubuntu releases using the 64-bit time ABI may name GTK's runtime package `libgtk-3-0t64`; installing `libwebkit2gtk-4.1-0` normally resolves the right GTK package automatically.

Fedora:

```sh
sudo dnf install webkit2gtk4.1 gtk3 xorg-x11-server-Xvfb xorg-x11-xauth
```

Build and install the fixture dependency:

```sh
npm install
make build
```

No WebKitGTK development headers or C compiler are needed because the Go binding loads its embedded adapter and system runtime libraries dynamically.

## Running tests

```sh
./bin/rush test examples/basic.test.ts
./bin/rush test examples/browser-api.test.ts examples/javascript.test.js
./bin/rush test examples/session.test.ts
./bin/rush test --json 'examples/*.test.ts'
./bin/rush test --headed examples/basic.test.ts
./bin/rush stop
```

Headless mode launches an authenticated Xvfb display and keeps it alive with the daemon. Headed mode requires an existing `DISPLAY` or `WAYLAND_DISPLAY`, uses a separate warm daemon, and enables the WebView debug flag. The daemon and its esbuild contexts remain warm across later invocations until `rush stop` is called.

Each suite is bundled independently with `@rush/browser` and must import the APIs it uses. The WebKit harness executes the package's shared registry and maps its batched results onto the native protocol. Assertions, Testing Library queries, mocks, spies, fake timers, snapshots, and synthetic interactions therefore run in the real browser page instead of a duplicate embedded test implementation.

Automatic JSX uses React when the project declares React, Preact when it declares only Preact, and React otherwise. `RUSH_JSX_IMPORT_SOURCE` provides an explicit override. Bundles run with `process.env.NODE_ENV` set to `test` by default so framework testing APIs remain available; `RUSH_NODE_ENV` can override it.

Before the next suite, Rush clears the DOM, style nodes, timers, animation frames, registered event listeners, cookies, local and session storage, performance entries, and bundle globals. Bundle scoping supplies a fresh registry and mock runtime for every file. Rush does not yet provide a separate WebView realm per browser test file, service-worker cleanup, native input, or network interception.

Session tests use `test.session({clients: ["alice", "bob"]})`. The Linux adapter assigns each named client a persistent worker from a bounded four-WebView pool. Each worker has a separate WebKit profile, so clients navigating to the same realtime application do not share cookies, local storage, or session storage. `client.goto(url)` performs lifecycle navigation and `client.evaluate(callback)` sends one coarse callback to execute entirely inside that client's page; DOM and application operations inside the callback do not cross the host bridge. Clients can evaluate concurrently with `Promise.all`, and disposal scrubs visible browser state before returning a worker to the pool.

## Timing model

Normal and JSON output report these measurements separately:

- `build`: the host-side esbuild context rebuild.
- `runner`: page time outside hooks and test callbacks, including registration and orchestration.
- `application`: callback wall time after subtracting observed network-resource duration and completed requested timer delays.
- `network`: WebKit resource timing durations observed during the suite.
- `intentional wait`: requested delays for timers that fired while a test was executing.
- `page total`: registration plus test execution inside WebKit.

Network requests and timers can overlap, so the phase values are attribution signals rather than an accounting identity. Host build time is never folded into page time.

## Reproducible benchmarks

```sh
./bin/rush bench
./bin/rush bench --repeat 10 --cold-repeat 5 --json
```

The harness validates the exact number of executed passing tests and compares medians against the product targets without changing them:

| Scenario | Fixture | Target metric |
| --- | --- | ---: |
| Process-cold WebKitGTK startup | smoke | under 2,000 ms before build/user test time |
| 1,000 trivial assertions | assertions | under 250 ms warm page total |
| 1,000 DOM tests | DOM | under 1,000 ms warm page total |
| 1,000 Preact component tests | components | under 5,000 ms warm page total |
| Incremental rebuild + 100 affected tests | generated edit | under 500 ms median |
| 100 mixed browser tests | mixed | under 1,000 ms warm page total |

The cold scenario starts a new native process and browser for every sample. It cannot flush kernel, filesystem, or WebKit caches, so the output describes process-cold startup on the current host, not a clean-machine CI claim. The 100-test fixture is a synthetic browser mix; it does not claim the separate representative Kodē migration target or the 10× Vitest comparison.

All raw samples, phase medians, measurement definitions, targets, and pass/fail verdicts are emitted in JSON. A missed target makes the benchmark command fail.

## Architecture

The CLI connects to a mode-specific Unix socket under the user's cache directory. If needed, it starts the Go daemon and waits on a dedicated readiness pipe. The daemon owns one WebKitGTK event loop and a cache of esbuild `BuildContext` instances keyed by absolute suite path. Requests are serialized through the page, results cross the native bridge once per suite, and a per-suite timeout bounds a hung page.

The Unix socket and log are user-only. Xvfb normally uses its local Unix socket. In read-only WSL/container environments where `/tmp/.X11-unix` lacks the required sticky bit, Rush falls back to a loopback TCP display protected by a generated Xauthority cookie.

## Host integration contracts

The adapter-independent host packages define the intended `run`, `watch`, and `debug` command surface, including terminal, JUnit XML, TAP, JSON, and GitHub reporters. Build configuration supports JSX modes, aliases, transforms, and esbuild plugins. Failed tests can produce screenshots and DOM snapshots under `.rush/artifacts` after measured user-test execution.

- `app` connects parsed commands to a native runtime and maps outcomes to process exit codes.
- `command` parses commands and produces build, reporter, and artifact configuration.
- `watch` maintains esbuild's reverse import graph and selects only transitively affected suites. Config and plugin changes invalidate all suites.
- `result` is the stable native-host-to-reporter result protocol.
- `execution` orders runtime execution, failure collection, and reporting while preserving separate user, runner, artifact, and reporter timings.
- `reporter` and `artifact` emit observable output without depending on a platform adapter.

The Linux proof currently exposes its daemon through the `rush test` commands above. Wiring that adapter into the final command package is follow-on integration work; this README does not claim the two command paths are already unified.
