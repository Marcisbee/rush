# WPE WebKit headless evaluation

## Decision

WPE WebKit preserves Rush's current real-browser conformance boundary and every measured performance target. Rush therefore includes an opt-in WPE build for Linux CI. WebKitGTK under Xvfb remains the default because its runtime packages are readily available and it supplies Rush's headed debugging path.

## Environment and method

Both adapters ran the same Rush commit, fixture dependency, conformance fixtures, and benchmark command on the same x86-64 Ubuntu 26.04 host:

```sh
rush bench --repeat 5 --cold-repeat 3 --json
```

The WPE run used a release build of WPE WebKit 2.52.6 with `WPEDisplayHeadless`. The WebKitGTK run used the system WebKitGTK 4.1 runtime under Rush's authenticated Xvfb display. Each adapter used an isolated daemon cache. The nested WPE build container did not permit the bubblewrap network namespace, so only that evaluation run set `WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1`; disabling the sandbox is not part of the adapter or recommended CI configuration.

The API registration, hook, skip/todo, suite isolation/reset, network-resource timing, and intentional-wait fixtures all passed through the WPE bridge before measurement.

## Results

Medians are milliseconds. The values describe this host and process-cold runs; they are not universal latency claims.

| Scenario | Target | WPE headless | WebKitGTK + Xvfb |
| --- | ---: | ---: | ---: |
| Cold process to bridge ready | <2,000 | 114.59 | 244.86 |
| 1,000 assertions, warm page | <250 | 1.00 | 1.00 |
| 1,000 DOM tests, warm page | <1,000 | 4.00 | 6.00 |
| 1,000 Preact components, warm page | <5,000 | 6.00 | 7.00 |
| 100 mixed tests, warm page | <1,000 | 1.00 | 1.00 |
| Incremental rebuild + 100 tests | <500 | 0.72 | 1.13 |

Cold samples were `132.020702`, `114.592967`, and `112.467819` for WPE, and `252.644471`, `244.862083`, and `230.364198` for WebKitGTK. All five warm repetitions passed for every scenario on both adapters. Values at or below one millisecond are near the harness clock's granularity and should be read as contract checks, not meaningful adapter differences.

## Dependencies and limitations

- The WPE adapter requires WPE WebKit 2.52 or newer built with WPE Platform and its headless backend. A bridge library alone is insufficient: the matching WPE web, network, and GPU process executables and their GLib, libsoup, GStreamer, font, graphics, and media dependencies must be installed.
- The evaluated Ubuntu 26.04 package indexes contained no `libwpewebkit-2.0` runtime or development package. A source build worked, but its cost and process-path installation make WPE an opt-in adapter rather than Rush's portable Linux default.
- The WPE adapter has no visible window or headed inspector mode. The default WebKitGTK build remains necessary for interactive debugging.
- The comparison covers Rush's current harness and inherits its limits: no service-worker reset, native trusted-input path, network interception, or separate WebView realm per file is claimed.
- WPE removes the Xvfb and `xauth` dependency and does not touch `DISPLAY` or `WAYLAND_DISPLAY`. It does not remove WebKit's normal multiprocess runtime or sandbox requirements.
