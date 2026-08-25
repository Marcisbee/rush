# Workspace migration guidance

Rush is a candidate replacement for JavaScript browser-facing tests, not a mandate to move every test. Migrate one independently verifiable slice at a time, keep the former runner available, and remove it only after correctness, platform, artifact, and performance seams all pass in the consumer's CI.

The names `rush` and `@rush/browser` are provisional. All trials must use pinned private revisions and must not publish packages or binaries.

## Choose the model before porting

Classify every candidate file by its observable behavior:

- **Browser:** rendering, synchronous units, component state, Testing Library queries, accessibility semantics, mocks, fake timers, and browser storage inside one page.
- **App:** real navigation, server responses, authentication, origin storage, service workers, request interception, or browser-default application flows.
- **Session:** two or more independently isolated clients interacting through a server, WebSocket, or other realtime channel.
- **Keep elsewhere:** Node-only code, filesystem/process behavior, backend integration, unsupported browser state, or a platform dependency Rush cannot currently provide.

Do not port an app test as a browser test by replacing navigation with hand-built DOM. Do not port a session test by sharing objects between fake clients in one page. A faster test with a weaker correctness boundary is not a successful migration.

## Establish the baseline

Before editing the suite:

1. Select a representative slice with stable pass counts and named behavior categories.
2. Record the current runner version, command, machine/image, cold or warm state, repeat count, raw samples, median, and failures.
3. Record artifact and debugging behavior the project relies on.
4. Define the rollback command and keep it working throughout the trial.
5. Run the identical logical tests in Rush and verify counts before comparing timing.

For browser performance proofs, use at least five measured repetitions after an explicit warm-up. For app and session suites, report Rush overhead separately from application, network, and intentional waits rather than imposing the synthetic browser threshold.

## Porting Vitest-style files

Change explicit imports and preserve the test body where the compatibility surface matches:

```diff
- import { describe, expect, test, vi } from "vitest";
+ import { describe, expect, test, vi } from "@rush/browser";
```

Then check these seams:

- Replace implicit globals with imports.
- Keep statically analyzable `vi.mock` calls; retain dynamic or Node-dependent mocking in Vitest until equivalent behavior is proven.
- Use Rush's Testing Library exports or existing browser-compatible Testing Library packages.
- Verify fake timer and snapshot behavior instead of assuming every Vitest option exists.
- Move reusable setup into explicit imports while the Linux CLI has no setup-file configuration.
- Replace Node-only utilities and environment access with browser-safe fixtures, or keep the test in its former runner.
- Use `RUSH_JSX_IMPORT_SOURCE` only when automatic React/Preact detection is wrong.
- Use `RUSH_BROWSER_MODULE` to point at the pinned private build; do not create a temporary public package.

Run the source-built CLI from the consumer repository so dependency and JSX detection use the consumer's `package.json` and `node_modules`:

```sh
RUSH_BROWSER_MODULE=/absolute/private/rush/dist/index.js \
  /absolute/private/rush/bin/rush test 'src/**/*.rush.test.tsx'
```

Use a separate `.rush.test.*` pattern during migration. This prevents both runners from collecting the same file and makes rollback a command change rather than a file-history rewrite.

## Project-specific guidance

### Kodē

The established proof boundary is 12 files and exactly 100 tests covering React rendering, Testing Library and user-event behavior, extensive mocks, fake timers, storage, and accessibility.

The sequential architecture failed the required 10× comparison even after batching and caching. The bounded three-realm prototype preserved per-file isolation and measured 109.0 ms against a 2,035.5 ms Vitest/jsdom baseline, or 18.7×, with a separate 147.9 ms warm median. Use that architecture only after it is integrated into the delivery branch and re-run the unchanged proof in Kodē CI.

Do not claim the complete 1,169-test milestone. Port the remaining suite in browser-safe behavior slices, keep Node/server tests in their current runner, and measure the full suite before adopting the under-10-second milestone or under-5-second stretch target.

### Editpal

Editpal's proof defines the conformance split:

- Selection reads, DOM assertions, accessibility queries, same-origin iframe content, Shadow DOM queries, and deliberately synthetic input can use browser tests.
- `beforeinput`, browser-default editing, native keyboard behavior, and assertions about `Event.isTrusted` require explicit trusted automation.

Run synthetic coverage in ordinary headless browser CI. Keep trusted-input coverage in a permissioned interactive job for the selected platform, and keep Editpal's former browser runner until Rush reproduces both groups without weakening assertions. WPE cannot host the trusted-input group.

### Strike

Strike's representative app proof covers seeded authentication, real navigation, trusted password entry, token storage and isolation, request inspection, and transient feed recovery. That proof validates the native app-adapter direction, including transparent URL semantics and cross-realm DOM matching.

Do not move the proof onto the checked-in Linux CLI yet: app navigation, interception, and native input are pending integration. When they land, start Strike's deterministic test server and seed data outside Rush, run the same authentication and recovery flow, verify storage isolation, and keep application/network/wait timing separate from runner overhead. Retain the previous end-to-end command as rollback.

### Puzzle

Puzzle's representative session proof uses four isolated clients in one realtime room. It verifies independent local/session storage and window/history state, concurrent grouping and contention, and convergence after reconnect.

The proof passed against the isolated-session adapter with application, network, and intentional-wait phases reported separately. It does not imply the checked-in Linux CLI supports sessions. Adopt it only after the session adapter is integrated, then re-run without local package links and verify that client profiles reset between tests. Keep the original four-player browser scenario until the Rush job is stable in CI.

### Other workspace projects

Start with browser-only files that avoid Node and unsupported persistent browser state. Add app or session slices only when their exact native adapter is available on the project's CI platforms. Reuse the project-specific evidence standard above; do not infer readiness from another repository's proof.

## Adoption gates

A project can replace an existing runner for a slice only when all of these are true:

- The same observable behavior and pass count execute in a real browser engine.
- Required operating-system and WebView dependencies are installed in local and CI environments.
- Synthetic versus trusted input is explicit and permissioned appropriately.
- Isolation covers every state surface the slice modifies, or teardown is owned by the test.
- Required reports and failure artifacts are available through the actual executable, not only a library contract.
- The measured result meets the slice's declared target with reproducible raw samples.
- The previous runner remains a one-command rollback until the Rush slice is stable.

If any gate fails, keep that slice in its current runner and record the missing capability. Do not broaden Rush's claimed compatibility to make a migration appear complete.

## Removing the former runner

Remove old dependencies, configuration, and CI steps only after every owned slice has passed its adoption gates on the default branch. Preserve unrelated Node, backend, or end-to-end test infrastructure still used by non-Rush suites. Because distribution names are unsettled, defer package-lock normalization and public release wiring until naming is approved.
