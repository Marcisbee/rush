# Rush

Rush is a persistent WebView-native JavaScript and TypeScript test runner. This repository currently contains the first Linux proof: a native Go daemon keeps WebKitGTK and incremental esbuild contexts alive between CLI runs, while tests register and execute inside the page.

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

### Optional WPE headless build

WPE WebKit 2.52 or newer can replace WebKitGTK and Xvfb for headless Linux CI. It uses WPE's in-process headless display backend, so no X11 or Wayland compositor is started:

```sh
make build-wpe
./bin/wpe/rush doctor
./bin/wpe/rush test examples/basic.test.ts
```

`build-wpe` requires a C compiler, `pkg-config`, and the `wpe-webkit-2.0` and `wpe-platform-headless-2.0` development packages. The resulting `rush` binary and `libwebview.so` must remain together. The complete matching WPE runtime must also install its web, network, and GPU process executables in the location configured by that WPE build.

The WPE binary is deliberately headless-only. Use the default WebKitGTK build for `--headed` debugging. WPE is opt-in because the evaluated Ubuntu 26.04 image did not offer WPE WebKit 2.x packages; building WebKit from source is not yet a reasonable default CI prerequisite. See [the WPE evaluation](docs/wpe-evaluation.md) for the measured comparison and limitations.

## Running tests

```sh
./bin/rush test examples/basic.test.ts
./bin/rush test --json 'examples/*.test.ts'
./bin/rush test --headed examples/basic.test.ts
./bin/rush stop
```

Headless mode launches an authenticated Xvfb display and keeps it alive with the daemon. Headed mode requires an existing `DISPLAY` or `WAYLAND_DISPLAY`, uses a separate warm daemon, and enables the WebView debug flag. The daemon and its esbuild contexts remain warm across later invocations until `rush stop` is called.

Each suite is bundled independently. Before the next suite, Rush clears the DOM, style nodes, timers, animation frames, registered event listeners, cookies, local and session storage, performance entries, hooks, and test registrations. Bundle scoping isolates top-level variables. This prototype does not yet provide a separate WebView realm per file, service-worker cleanup, native input, network interception, or the complete Vitest-compatible API.

The current in-page API deliberately covers the proof's registration and assertion seam: `test`/`it`, `describe`, `beforeEach`, `afterEach`, `skip`, `only`, `todo`, `each`, and common `expect` matchers. The broader compatibility API belongs to the follow-on runtime work.

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
| Process-cold browser startup | smoke | under 2,000 ms before build/user test time |
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
