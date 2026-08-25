# Installation and configuration

Rush currently supports source builds from this private repository. The binary name `rush` and package name `@rush/browser` are provisional. There is no supported npm, Homebrew, winget, or system-package distribution yet.

Pin the repository revision used by a consumer and keep the repository and built artifacts private until the architecture and names are approved.

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
sudo apt-get install libwebkit2gtk-4.1-0 libgtk-3-0 xvfb xauth
```

Ubuntu releases using the 64-bit time ABI may provide `libgtk-3-0t64` instead of `libgtk-3-0`. Installing WebKitGTK normally selects the correct GTK package.

Fedora:

```sh
sudo dnf install webkit2gtk4.1 gtk3 xorg-x11-server-Xvfb xorg-x11-xauth
```

Headless mode starts an authenticated Xvfb display and keeps it with the warm daemon. In a WSL or container environment where `/tmp/.X11-unix` cannot safely host a local socket, Rush uses a loopback TCP display protected by a generated Xauthority cookie.

Headed mode uses the current desktop and requires `DISPLAY` or `WAYLAND_DISPLAY`. It enables the WebView debug flag and uses a separate daemon from headless mode.

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

`RushWKWebViewAdapter` requires macOS 13 or newer and a Swift 5.9-compatible toolchain:

```sh
swift test
```

Normal realms are unattached to a window. Debug realms are visible and inspectable through Safari's Develop menu on macOS 13.3 or newer. Hidden conformance tests need no Accessibility permission. Trusted Core Graphics input requires a logged-in GUI session and explicit Accessibility authorization.

The Swift adapter is a library and harness on this revision. It is not selected by `bin/rush`.

## Windows

The WebView2 adapter has been validated in project delivery work against the Evergreen Microsoft Edge WebView2 Runtime, including hidden execution, visible DevTools, pooled realms, bridge batching, failure capture, and trusted `SendInput`. Its implementation is not present on this revision, so there is no Windows build command in the checked-in CI yet.

The validated adapter requires a supported Windows desktop, WebView2 Runtime, Go 1.24 or newer, and a writable user-data directory. Trusted input additionally requires an interactive desktop; Windows services in session 0 cannot use it.

## Consumer package resolution

Every test imports the working package name:

```ts
import { expect, test } from "@rush/browser";
```

The Linux builder resolves that import in this order:

1. `RUSH_BROWSER_MODULE`.
2. `<consumer>/node_modules/@rush/browser/dist/index.js`.
3. `<consumer>/dist/index.js`.
4. `dist/index.js` adjacent to the Rush binary's parent directory.

For private cross-repository trials, build a pinned Rush checkout and set an absolute `RUSH_BROWSER_MODULE` path. Do not publish a temporary registry package or rely on a global symlink.

## Environment configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `RUSH_BROWSER_MODULE` | Absolute path to the built browser API for private consumer layouts | Resolver search above |
| `RUSH_JSX_IMPORT_SOURCE` | Overrides automatic JSX runtime selection | React if declared, otherwise Preact if it alone is declared, otherwise React |
| `RUSH_NODE_ENV` | Compile-time value of `process.env.NODE_ENV` in suites | `test` |
| `DISPLAY` / `WAYLAND_DISPLAY` | Select the existing desktop for `--headed` | Rush-managed Xvfb in headless Linux mode |

`RUSH_READY_FD` is an internal daemon handshake and is not user configuration.

Changing the JSX import source or Node environment creates a distinct incremental build context. Stop the daemon after changing toolchain revisions or private package paths so the next run starts with an unambiguous process and module graph.

