# Kodē representative performance decision

## Boundary and method

The representative proof is the exact 12-file, 100-test Kodē set from its benchmark change. It covers React rendering, Testing Library and user-event flows, mocks, fake timers, storage, and accessibility. Every sample executes all 100 tests in WebKitGTK with a fresh module factory and browser reset for each file.

The benchmark runs five fresh Vitest/jsdom processes, warms the persistent Rush daemon once, and then measures five Rush CLI process-wall samples. It repeats the warm Rush series for the independent sub-second target. The original proof used Rush `05b1111`; the current result was measured from the same Kodē checkout at Rush code revision `1df573d`, with this branch rebased onto `main` at `8635381`.

## Measurements

| Measurement | Before samples (ms) | Before median | Reworked samples (ms) | Reworked median | Target | Verdict |
| --- | --- | ---: | --- | ---: | ---: | --- |
| Vitest/jsdom fresh process | 1,578.1; 1,612.6; 1,600.8; 1,598.8; 1,619.2 | 1,600.8 ms | 2,227.3; 1,612.6; 1,598.0; 1,893.9; 1,819.2 | 1,819.2 ms | baseline | pass |
| Rush, same 100 tests | 1,647.0; 1,376.5; 1,308.1; 1,453.8; 1,592.4 | 1,453.8 ms | 267.4; 332.2; 387.9; 471.3; 482.7 | 387.9 ms | at most 181.9 ms (10×) | **failed: 4.7×** |
| Rush warm 100-test repeat | 1,661.3; 1,996.2; 2,408.6; 2,138.3; 2,112.7 | 2,112.7 ms | 566.7; 618.4; 676.6; 690.0; 861.1 | 676.6 ms | under 1,000 ms | **pass** |

All 100 tests passed in every sample. The rework reduced the representative median by 73% and the independent warm median by 68%.

## Profile and architectural decision

The original warm request spent about 435 ms rebuilding 12 separate esbuild contexts, about 263 ms in measured page execution, and the balance in repeated native dispatch, source transfer, reset, and reporting. The rework makes an unchanged batch a zero-rebuild cache hit, traverses shared dependencies once after an edit, retains isolated per-file bundles, caches compiled factories in WebKit, and crosses the native bridge once per request.

The 10× result is a no-go for the current single-WebView, sequential file-isolation architecture. The current required ceiling is 181.9 ms process wall. Warm profiles still attribute roughly 90–130 ms to real-browser suite execution before process startup, native dispatch, reset, result serialization, or WebKit cleanup between repeated React module graphs. Sharing one module graph across files would remove much of that work, but it would also change mock, singleton, and module-state isolation and is outside the correctness boundary of this proof.

The next architecture worth measuring is a bounded pool of independently isolated WebView realms that executes files in parallel and recycles realms to cap retained browser work. Rush should not claim the 10× representative milestone unless that design, or another design preserving the same file isolation and real-browser runtime, passes the unchanged five-run benchmark. The complete 1,169-test targets remain unmeasured and unclaimed.

## Bounded parallel-realm follow-up

The follow-up prototype keeps four independent WebKitGTK realms warm under one native daemon and event loop. The exact same 12-file, 100-test Kodē fixture and five-run process-wall method produced:

| Measurement | Samples (ms) | Median | Target | Verdict |
| --- | --- | ---: | ---: | --- |
| Vitest/jsdom fresh process | 1,690.9; 1,604.1; 1,586.0; 1,582.2; 1,612.2 | 1,604.1 ms | baseline | pass |
| Rush, same 100 tests | 67.2; 66.0; 72.9; 84.2; 88.2 | 72.9 ms | at most 160.4 ms (10×) | **pass: 22.0×** |
| Rush warm 100-test repeat | 99.5; 103.2; 114.1; 117.9; 120.6 | 114.1 ms | under 1,000 ms | **pass** |

All 100 tests passed in every sample. Files keep separate esbuild bundles, module graphs, registries, and mock runtimes. Each realm still performs the complete DOM, timer, listener, storage, cookie, performance-entry, and added-global reset before its next file.

This is a measured go for bounded parallel execution. Headless mode defaults to up to four realms based on available Go parallelism, configuration is rejected outside one through eight, and headed mode defaults to one. The benchmark daemon used four realms and retained exactly four WebKit web-process children across the warm series. Each realm retains at most 64 compiled factories, so both native realm count and browser factory retention are capped independently of the number of suites. The complete 1,169-test targets remain unmeasured and unclaimed.
