# Rush

Rush is a persistent WebView-native JavaScript and TypeScript test runner focused on extreme browser-test performance.

The repository is under active development. The `@rush/browser` package contains the public, Vitest-compatible API that executes inside a real browser page. Native hosts provide navigation, isolated sessions, and trusted input through explicit adapters; ordinary assertions, queries, mocks, timers, snapshots, and synthetic interactions stay in the page.

## Command surface

The native host consumes the adapter-independent command package. The supported forms are deliberately small:

```text
rush [run] [patterns...]
rush watch [patterns...]
rush debug [patterns...]
```

`--watch` (`-w`) aliases `watch`. `--debug` aliases `debug`, which always enables `--headed`. Normal runs remain headless unless `--headed` is explicit.

Reporters are selected with repeatable or comma-separated flags:

```text
--reporter=terminal,junit,json
--output-file=junit=reports/junit.xml
--output-file=json=reports/results.json
```

Rush supports `terminal`, `junit`, `tap`, `json`, and `github` output. Reporter work happens only after browser execution and is tracked separately from measured user-test time.

Build customization passes through to the long-lived esbuild host:

```text
--jsx=automatic
--jsx-import-source=preact
--alias=@app=./src
--transform=react=./tools/react-transform.js
--plugin=./tools/esbuild-plugin.js
```

JSX modes are `automatic`, `transform`, and `preserve`. Alias, transform, and plugin flags are repeatable.

Failed browser tests capture screenshots and DOM snapshots under `.rush/artifacts` by default. Configure the location with `--artifacts-dir`, or disable either artifact with `--screenshots=false` and `--dom-snapshots=false`.

## Integration contracts

- `app` connects parsed commands to a native runtime and maps outcomes to process exit codes.
- `command` parses commands and produces build, reporter, and artifact configuration.
- `watch` maintains esbuild's reverse import graph and selects only transitively affected suites. Config and plugin changes invalidate all suites.
- `result` is the stable native-host-to-reporter result protocol.
- `execution` orders runtime execution, failure collection, and reporting while preserving separate user, runner, artifact, and reporter timings.
- `reporter` and `artifact` emit observable output without depending on a platform adapter.
