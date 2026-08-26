# Installation and configuration

Rush publishes its browser API as the public, unscoped npm package `rush-webtest`. Matching native `rush` executables for Linux and macOS are attached to each GitHub release; there is no npm binary, Homebrew, winget, or system-package distribution yet.

Install the browser API in each consumer project:

```sh
npm install --save-dev rush-webtest
```

Download the executable archive whose release version matches the browser package and whose operating system and architecture match the target machine. The archive keeps `bin/rush` beside its required `dist/` browser API directory.

## Common build dependencies

- Go 1.24 or newer.
- Node.js 22 and npm for the browser API and fixtures.
- Platform browser dependencies described below.

Build the TypeScript package and default Go binary from the repository root:

```sh
npm ci
make build
```

`make build` writes `dist/` and `bin/rush`. Both directories are ignored build output. Run `./bin/rush doctor` before the first test on a machine or image.

## Linux: WebKitGTK

The default Linux binary dynamically loads the embedded WebView binding, GTK 3, and WebKitGTK 4.1. WebKit development headers and a C compiler are not required for this build.

Debian or Ubuntu:

```sh
sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0 libxtst6 xvfb xauth
```

Ubuntu releases using the 64-bit time ABI may provide `libgtk-3-0t64` instead of `libgtk-3-0`. Installing WebKitGTK normally selects the correct GTK package.

Fedora:

```sh
sudo dnf install webkit2gtk4.1 gtk3 libXtst xorg-x11-server-Xvfb xorg-x11-xauth
```

Headless mode starts an authenticated Xvfb display for the current command and cleans it up on exit. In watch mode, the display remains warm until the foreground command is interrupted. In a WSL or container environment where `/tmp/.X11-unix` cannot safely host a local socket, Rush uses a loopback TCP display protected by a generated Xauthority cookie.

Headed mode uses the current desktop and requires `DISPLAY` or `WAYLAND_DISPLAY`. It enables the WebView debug flag. Trusted input uses XTest on the selected X11 display and fails explicitly when `libXtst` or a usable display is unavailable.

## Linux: optional WPE headless build

WPE WebKit 2.52 or newer can replace WebKitGTK and Xvfb for headless CI:

```sh
make build-wpe
./bin/wpe/rush doctor
./bin/wpe/rush test examples/basic.test.ts
```

This build requires a C compiler, `pkg-config`, `wpe-webkit-2.0`, and `wpe-platform-headless-2.0` development files. `bin/wpe/rush` and `bin/wpe/libwebview.so` must stay together. The matching WPE web, network, and GPU process executables and their runtime dependencies must also be installed.

WPE is headless-only and has no visible inspector path. It remains opt-in because the evaluated Ubuntu 26.04 package indexes did not provide WPE WebKit 2.x packages. See [the WPE evaluation](wpe-evaluation.md) for the measured decision.

## macOS

Rush requires macOS 13 or newer, Go 1.24 or newer, and the C/Objective-C compiler and macOS SDK supplied by Xcode Command Line Tools. It does not require the Swift compiler or runtime:

```sh
xcode-select --install # only when the command-line tools are missing
npm ci
make build
./bin/rush doctor
./bin/rush test examples/basic.test.ts
```

The build uses cgo to compile a thin Objective-C C-ABI shim directly into `bin/rush`. The resulting executable links Apple AppKit, WebKit, ApplicationServices, and CoreGraphics system frameworks. It does not extract a WebView dynamic library or start a Swift/helper adapter process.

Normal runs create hidden WKWebView windows. `--headed` presents the window and enables Web Inspector on macOS 13.3 or newer. Hidden tests need no Accessibility permission. Trusted Core Graphics input requires a headed run, a logged-in GUI session, and explicit Accessibility authorization for the Rush executable or its launching terminal.

## Windows

The WebView2 adapter is available under `platform/webview2` and is validated against the Evergreen Microsoft Edge WebView2 Runtime, including hidden execution, visible DevTools, pooled realms, bridge batching, failure capture, and trusted `SendInput`.

The adapter requires a supported Windows desktop, WebView2 Runtime, Go 1.24 or newer, and a writable user-data directory. Trusted input additionally requires an interactive desktop; Windows services in session 0 cannot use it. Run its standalone validation harness from a Windows shell:

```powershell
go run ./cmd/rush-webview2-harness -repeats 10
```

The adapter is not yet selectable through `bin/rush`. See [Windows WebView2 setup and validation](windows-webview2.md).

## Consumer package resolution

Every test imports the working package name:

```ts
import { expect, test } from "rush-webtest";
```

The npm package provides declarations for TypeScript and editors. Production native binaries embed the browser implementation built from the same release, so suite bundles reference one already-loaded runtime instead of copying Testing Library, fake timers, and Rush internals into every file.

For local development against an unpublished browser API build, set `RUSH_BROWSER_MODULE` to its absolute `dist/index.js` path. That explicit override is bundled with the suite.

## Environment configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `RUSH_BROWSER_MODULE` | Absolute path to a locally built browser API override | Native binary's embedded runtime |
| `RUSH_JSX_IMPORT_SOURCE` | Overrides automatic JSX runtime selection | React if declared, otherwise Preact if it alone is declared, otherwise React |
| `RUSH_NODE_ENV` | Compile-time value of `process.env.NODE_ENV` in suites | `test` |
| `VITE_*` | Public string values compiled into `import.meta.env` | Unset |
| `RUSH_WEBVIEW_POOL_SIZE` | Explicit number of reusable Linux or macOS browser realms, from one through four | Up to three hidden, capped by file count; one headed |
| `DISPLAY` / `WAYLAND_DISPLAY` | Select the existing desktop for `--headed` | Rush-managed Xvfb in headless Linux mode |

`RUSH_READY_FD` and `RUSH_LIFETIME_FD` are internal host lifecycle channels and are not user configuration.

Changing the JSX import source or Node environment creates a distinct incremental build context during watch mode. A new invocation always starts with the current executable, environment, and local package paths.
