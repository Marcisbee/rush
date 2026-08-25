# Kodē representative performance decision

## Boundary and method

The representative proof is the exact 12-file, 100-test Kodē set from its benchmark change. It covers React rendering, Testing Library and user-event flows, mocks, fake timers, storage, and accessibility. Every sample executes all 100 tests in WebKitGTK with a fresh module factory and browser reset for each file.

The benchmark runs five fresh Vitest/jsdom processes, warms the persistent Rush daemon once, and then measures five Rush CLI process-wall samples. It repeats the warm Rush series for the independent sub-second target. The original proof used Rush `05b1111`; the reworked result was measured from the same Kodē checkout after applying this branch on top of Rush `39e9fe0`.

## Measurements

| Measurement | Before samples (ms) | Before median | Reworked samples (ms) | Reworked median | Target | Verdict |
| --- | --- | ---: | --- | ---: | ---: | --- |
| Vitest/jsdom fresh process | 1,578.1; 1,612.6; 1,600.8; 1,598.8; 1,619.2 | 1,600.8 ms | 1,619.3; 1,620.1; 1,586.8; 1,572.5; 1,593.8 | 1,593.8 ms | baseline | pass |
| Rush, same 100 tests | 1,647.0; 1,376.5; 1,308.1; 1,453.8; 1,592.4 | 1,453.8 ms | 222.4; 305.2; 364.2; 425.1; 447.7 | 364.2 ms | at most 159.4 ms (10×) | **failed: 4.4×** |
| Rush warm 100-test repeat | 1,661.3; 1,996.2; 2,408.6; 2,138.3; 2,112.7 | 2,112.7 ms | 566.4; 613.1; 690.3; 582.2; 798.2 | 613.1 ms | under 1,000 ms | **pass** |

All 100 tests passed in every sample. The rework reduced the representative median by 75% and the independent warm median by 71%.

## Profile and architectural decision

The original warm request spent about 435 ms rebuilding 12 separate esbuild contexts, about 263 ms in measured page execution, and the balance in repeated native dispatch, source transfer, reset, and reporting. The rework makes an unchanged batch a zero-rebuild cache hit, traverses shared dependencies once after an edit, retains isolated per-file bundles, caches compiled factories in WebKit, and crosses the native bridge once per request.

The 10× result is a no-go for the current single-WebView, sequential file-isolation architecture. The required ceiling is 159.4 ms process wall. Warm profiles still attribute roughly 90–130 ms to real-browser suite execution before process startup, native dispatch, reset, result serialization, or WebKit cleanup between repeated React module graphs. Sharing one module graph across files would remove much of that work, but it would also change mock, singleton, and module-state isolation and is outside the correctness boundary of this proof.

The next architecture worth measuring is a bounded pool of independently isolated WebView realms that executes files in parallel and recycles realms to cap retained browser work. Rush should not claim the 10× representative milestone unless that design, or another design preserving the same file isolation and real-browser runtime, passes the unchanged five-run benchmark. The complete 1,169-test targets remain unmeasured and unclaimed.
