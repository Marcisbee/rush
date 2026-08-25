# Reporters, failure artifacts, and CI

Rush keeps user execution, failure collection, and report writing as separate phases. Reporter or screenshot time must not inflate the application timing used for performance decisions.

## Reporter contracts

The adapter-independent host supports these reporters:

| Reporter | Output |
| --- | --- |
| `terminal` | Per-test PASS/FAIL/SKIP/TODO lines, errors, artifact paths, counts, user time, and runner time |
| `junit` | XML suites grouped by source suite, suitable for CI test ingestion |
| `tap` | TAP version 13 with skip/todo directives and failure diagnostics |
| `json` | The stable native `result.Summary`, including tests, artifacts, and timing phases |
| `github` | Escaped GitHub Actions error annotations with file, line, column, and title when available |

The intended host command syntax is:

```sh
rush run 'test/**/*.test.ts' \
  --reporter terminal,junit,github \
  --output-file junit=.rush/reports/junit.xml
```

Reporter names may be comma-separated or repeated. `--output-file reporter=path` is repeatable and is valid only for an active reporter. An output without a path writes to the host's stdout. Parent directories are created automatically.

These options are tested in the `command`, `reporter`, and `app` packages. The current Linux `bin/rush` proof does not wire them; use `test --json` for machine-readable Linux CLI output until that integration lands.

## Failure artifacts

The host contract enables PNG screenshots and HTML DOM snapshots for failed tests by default:

```sh
rush run \
  --artifacts-dir .rush/artifacts \
  --screenshots=true \
  --dom-snapshots=true \
  'test/**/*.test.ts'
```

Artifacts are collected after the failed user test stops. Their elapsed time is recorded under `artifact`, and report writing is recorded under `reporter`. Successfully written paths are attached to the failed test result. A failure to capture one artifact is reported without discarding other successful captures.

Artifact names include a stable result index plus sanitized suite and test names. Treat DOM snapshots as potentially sensitive application data. CI should upload them only from private runs, use short retention, and never capture secrets or production accounts.

The current Linux CLI does not yet invoke the adapter-independent collector. The macOS and Windows adapter harnesses exercise their native screenshot and DOM capture paths independently.

## Checked-in GitHub Actions

`.github/workflows/ci.yml` runs on every pull request and push to `main`:

- `Go and browser API` installs pinned Go and Node toolchains, checks TypeScript, runs Vitest API tests, runs all Go tests, and repeats Go tests with the race detector.
- `Linux WebKitGTK conformance and performance` installs the real WebKitGTK/Xvfb/XTest runtime, builds Rush, runs `doctor`, executes browser, app-automation, and isolated-session examples, and enforces the built-in benchmark targets.
- `Windows WebView2 adapter` runs the Windows Go packages and the real WebView2 harness for conformance, warm performance, trusted input, bridge batching, failure capture, and realm reuse.
- The Linux job uploads raw benchmark JSON for 14 days even when the benchmark command misses a target.

`.github/workflows/macos-adapter.yml` runs `swift test` on macOS for Swift adapter changes and on pushes to `main` that touch the adapter. It validates hidden WKWebView conformance and its performance harness.

A green core job alone is not evidence for a native adapter; use the corresponding real-WebView job or harness result.

## CI consumption guidance

For a private consumer trial:

1. Pin the Rush repository revision and browser module path.
2. Install platform browser dependencies explicitly in the job image.
3. Run `doctor` before tests so a loader or runtime mismatch fails early.
4. Keep the former test command available as the rollback path.
5. Store raw JSON or JUnit output and failure artifacts only in private, retention-limited CI storage.
6. Stop the warm daemon in an `always()` cleanup step.

Do not turn benchmark samples into a public badge or release claim while the repository and product names remain provisional.
