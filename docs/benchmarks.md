# Benchmark methodology and measured boundaries

Rush treats performance as a measured contract. Runner overhead is reported separately from application, network, and intentional-wait time so an optimized harness cannot hide slow product behavior or vice versa.

## Built-in harness

Run from the Rush repository after `make build`:

```sh
./bin/rush bench
./bin/rush bench --repeat 10 --cold-repeat 5 --json
```

The default is five measured warm repetitions and three process-cold repetitions. Each warm fixture is run once before measurement. The command verifies the exact number of passing tests, computes medians, emits every raw sample and phase median in JSON, and exits non-zero when a target is missed.

| Scenario | Fixture | Target metric |
| --- | --- | ---: |
| Process-cold browser startup | `smoke.ts` | Under 2,000 ms from native process start to bridge readiness |
| 1,000 trivial assertions | `assertions.ts` | Under 250 ms warm page total |
| 1,000 DOM tests | `dom.ts` | Under 1,000 ms warm page total |
| 1,000 Preact component tests | `components.tsx` | Under 5,000 ms warm page total |
| Incremental rebuild and 100 affected tests | Generated edit | Under 500 ms build plus page total median |
| 100 mixed browser tests | `hundred.ts` | Under 1,000 ms warm page total |

The cold scenario starts a fresh native process and browser for every sample. It does not flush kernel, filesystem, dynamic-loader, or WebKit caches, so it is process-cold on the measured host, not a clean-machine promise.

Warm page total measures `performance.now()` around registration and test execution inside WebKit. The incremental target adds host esbuild time to affected page execution. CLI process startup is not included in page timing.

## Timing interpretation

Normal and JSON test output separate build, runner, application, observed network, intentional waits, and page total. Network requests and timers may overlap. The values are attribution signals and need not sum exactly.

When comparing another runner:

- Use the same test files, assertions, application fixtures, machine, and browser/runtime boundary.
- Record raw samples and median rather than a best run.
- State whether each process, browser, module graph, and application server is cold or warm.
- Verify pass counts before accepting a timing.
- Keep product waits and network behavior in their own reported phases.

## Adapter evaluation

The WPE decision used the same Rush commit, fixtures, host, and `--repeat 5 --cold-repeat 3 --json` method for WPE WebKit 2.52.6 and WebKitGTK 4.1 under Xvfb. Both passed every built-in target. WPE reduced the measured cold median from 244.86 ms to 114.59 ms on that host. Values near one millisecond were at the harness clock's useful granularity and were not treated as meaningful engine differences.

Those results justify an opt-in WPE adapter, not a universal speed claim. See [WPE WebKit headless evaluation](wpe-evaluation.md) for raw samples and deployment constraints.

## Representative Kodē proof

The representative migration boundary is 12 Kodē files and exactly 100 tests covering React rendering, Testing Library and user-event flows, mocks, fake timers, storage, and accessibility.

The initial single-WebView proof missed both requested outcomes. A rework with batched builds and cached factories reached a 387.9 ms Rush median against a 1,819.2 ms Vitest/jsdom median: 4.7× rather than the required 10×. Its separate warm 100-test median was 676.6 ms and passed the sub-second target.

A follow-up prototype kept three independently isolated WebKitGTK realms warm and preserved a separate bundle, module graph, registry, mock runtime, and full browser reset for every file. On its five-run method:

| Measurement | Samples (ms) | Median | Verdict |
| --- | --- | ---: | --- |
| Vitest/jsdom fresh process | 2,089.2; 2,035.5; 2,746.4; 1,995.3; 1,820.6 | 2,035.5 | Baseline |
| Rush, same 100 tests | 82.9; 72.9; 109.0; 117.2; 111.4 | 109.0 | 18.7×; passes 10× |
| Rush warm 100-test repeat | 121.1; 147.9; 142.1; 166.6; 169.5 | 147.9 | Passes under one second |

All 100 tests passed in every sample. This is a measured go decision for bounded parallel realms, not evidence for the single-WebView runtime checked into this revision. The complete 1,169-test Kodē milestones—under 10 seconds and a stretch target under 5 seconds—remain unmeasured and unclaimed.

Application, server, PWA, realtime, and deliberately delayed suites must publish their own method and phase attribution. The sub-second browser fixture is never a guarantee for those models.
