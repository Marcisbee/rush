# Compatibility and platform status

This page describes the source tree at the revision containing it. Project proof branches demonstrate architectural feasibility, but only checked-in code and CI are supported delivery surfaces.

## Platform matrix

| Platform | Browser engine | Headless | Headed debug | Trusted input | Current integration |
| --- | --- | --- | --- | --- | --- |
| Linux default | WebKitGTK 4.1 under Xvfb | Yes | Yes, with an existing display | X11/XTest through the explicit native API | Browser, app, and session CLI available |
| Linux optional | WPE WebKit 2.52+ headless backend | Yes | No | No | Browser CLI available through `make build-wpe` |
| macOS 13+ | WKWebView | Hidden windows | Visible window and Web Inspector | Core Graphics with Accessibility permission | Browser, app, and session CLI available |
| Windows | WebView2 | Hidden/off-screen host | Visible window and DevTools | `SendInput` on an interactive desktop | Go adapter and validation harness |

A clean Linux server does not already include a browser runtime. Install the packages in [Installation and configuration](installation.md) or provide a complete WPE deployment.

## Browser API coverage

The public working package supports:

- JavaScript, TypeScript, JSX, and TSX bundled for ES2022 browsers.
- Vitest-like suites, hooks, selection, parameterized tests, and todo tests.
- Familiar core, promise, asymmetric, DOM, mock, and snapshot assertions.
- `vi.fn`, `vi.spyOn`, `vi.stubGlobal`, statically hoisted `vi.mock`, `vi.hoisted` mock state, and fake timers.
- Testing Library DOM queries, locators, `waitFor`, and synthetic interactions.
- Browser, app, and named-session type contracts.

It is not a drop-in implementation of every Vitest or Playwright feature. In particular:

- Node-only modules, Node globals, worker threads, filesystem calls, and Vitest plugin APIs do not execute in the browser page.
- Dynamic mocking patterns outside Rush's static hoist transform may not preserve Vitest behavior.
- Snapshot persistence and update commands depend on host integration and are not supplied by the Linux proof CLI.
- Browser-engine behavior differs between WebKit and Chromium. Keep engine-sensitive tests explicit.
- Cross-origin frame content remains subject to browser security boundaries.
- The Windows adapter remains a validation library and harness rather than a selectable backend in `bin/rush`.

## Isolation boundary

Every Linux or macOS browser suite receives a fresh bundled registry and mock runtime. Before reuse, the page clears DOM and style state, timers, animation frames, tracked listeners, cookies, local/session storage, IndexedDB, Cache Storage, service workers, performance entries, and globals introduced by the bundle.

The current boundary does not claim cleanup for permissions, downloads, browser extensions, operating-system clipboard state, or external processes. Tests that modify those surfaces need explicit teardown or a stronger per-profile/application isolation adapter.

The checked-in Linux runtime assigns files deterministically across a bounded pool of up to four WebViews and caps each realm's compiled-factory cache. App tests receive a fresh application frame and isolated routing state. Named session clients receive independent WebKit profiles and are scrubbed before pool reuse.

## Adapter versus CLI boundaries

The repository intentionally separates:

1. The in-page `rush-webtest` API.
2. Adapter-independent Go contracts for commands, watch selection, results, reporters, artifacts, and timing.
3. Native browser adapters.
4. The executable that wires one adapter to those contracts.

Passing tests in one layer do not imply that another layer is integrated. The current Linux executable exposes browser, app, and session execution through `test`, command-scoped warm reuse through `test --watch`, and explicit performance scenarios through `bench`. It does not yet wire the general debug, reporter, or artifact command contracts.

## Known operational limits

- Every one-shot command owns a unique native host and waits for it to stop before exiting. Watch mode keeps only its foreground command's host alive until interrupted.
- On Linux, run trusted-input files with `RUSH_WEBVIEW_POOL_SIZE=1` in a separate invocation. Native X11 focus is process-global and is not isolated from concurrent pooled realm or session navigation.
- WPE deployment must include matching helper processes and normal WebKit sandbox support; disabling the sandbox is not a supported configuration.
- Performance results describe a host, engine, fixture, and repeat method. They are not universal latency guarantees for applications, networks, realtime coordination, or intentional waits.
- The complete 1,169-test Kodē milestone remains unmeasured. Do not infer it from the 100-test representative proof.

## Privacy and naming

The public browser API package is `rush-webtest`. Native binaries are separate GitHub release artifacts and are not an npm distribution contract.
