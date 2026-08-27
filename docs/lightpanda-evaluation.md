# Lightpanda nightly evaluation

## Verdict

Lightpanda cannot currently provide a valid performance comparison for the Kodē browser partition. The opt-in adapter can start one Lightpanda process per Rush realm, navigate the controller, exchange asynchronous bindings, fetch bundles, and execute simple Rush suites. The unchanged 1,040-test partition does not complete, however, and isolated file runs expose browser behavior differences that fail existing assertions. No test was skipped or weakened, so no five-run timing samples were accepted.

## Prototype

The prototype uses Lightpanda's CDP endpoint behind Rush's existing WebView interface. It is excluded from normal builds and leaves WebKitGTK as the Linux default. Build and run it with:

```sh
make build-lightpanda
RUSH_LIGHTPANDA_PATH=/path/to/lightpanda RUSH_WEBVIEW_POOL_SIZE=2 \
  ./bin/lightpanda/rush test path/to/browser.test.ts
```

Each Rush browser realm owns one `lightpanda serve` child process. The evaluated two-realm Kodē command was observed with exactly two such processes. The adapter disables Lightpanda telemetry and core dumps for its child processes. Lightpanda does not fire the dynamic script-element load lifecycle used by the default runtime, so this build fetches and evaluates harness-served bundles. Missing optional Performance API cleanup methods are feature-detected.

## Environment and method

The evaluation used:

- Rush commit based on `e8b2b5b` (`v0.1.7`).
- Lightpanda `1.0.0-nightly.8925+a7bda0ea5`, Linux x86-64.
- Kodē migration revision `0a2499510df9beed28044dd773b7c0816fdc9b9f` from pull request 846.
- 125 browser files containing exactly 1,040 tests.
- One direct Rush command, no consumer-side sharding, and `RUSH_WEBVIEW_POOL_SIZE=2`.
- Existing loader and aliases: `.css=empty`, `vitest=./src/test/rush.ts`, and `virtual:pwa-register=./src/test/virtual-pwa-register.ts`.
- Existing two-worker Vitest median: 34.810 seconds.

The Rush smoke fixture passed unchanged in 67.14 ms complete-command wall time. Rush's browser API fixture executed all 23 tests in 118.86 ms, with 21 passing and two failing. The failures showed duplicated typed text (`Ada  LLoovveellaaccee` instead of `Ada Lovelace`) and incompatible computed-style values.

The complete Kodē command started two Lightpanda processes but returned no realm batch after more than two minutes. It was stopped because the aggregate Rush guard would otherwise permit a stalled 125-file batch to remain active for up to 125 minutes. Since it did not pass or return complete results, its elapsed time is not a benchmark sample.

To identify the incompatible boundaries, every unchanged Kodē browser file was then run in its own fresh Rush command with at most two commands active and an external 25-second diagnostic guard. This changes the process shape and is not benchmark evidence. Of the 125 file invocations, 117 passed, five completed with assertion failures, and three did not complete:

| File | Result | Observed incompatibility |
| --- | --- | --- |
| `ChatMessageRow.test.tsx` | 21 pass, 1 fail | A programmatically clicked `details` element did not retain its expected `open` state. |
| `MentionTextarea.test.tsx` | 16 pass, 1 fail | `getComputedStyle` returned an empty `boxSizing` value. |
| `ProjectAppearancePicker.test.tsx` | 4 pass, 1 fail | Directional keyboard navigation did not select the expected grid item. |
| `icons.test.tsx` | 4 pass, 1 fail | Computed style omitted the inherited SVG color. |
| `TicketPage.test.tsx` | 73 pass, 1 fail | Computed style did not report the expected editor `maxHeight` and `overflowY`. |
| `RichMarkdown.test.tsx` | Did not complete | The suite stalled before returning a test result. |
| `Sidebar.history.test.tsx` | Did not complete | The suite stalled before returning a test result. |
| `WorkspaceExtras.test.tsx` | Did not complete | The suite stalled before returning a test result. |

## Comparison boundary

The required comparison is the median of five passing, fresh, complete commands. Lightpanda produced zero qualifying samples, so calculating a median or a speedup against 34.810 seconds would be misleading. A future evaluation requires compatible text input, keyboard/focus, disclosure, computed-style, and async DOM scheduling behavior before performance is measured.
