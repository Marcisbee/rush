# macOS WKWebView adapter

Rush's macOS backend is compiled into the normal Go executable. Go owns the daemon, browser pool, session workers, protocol, incremental builder, and test lifecycle. A small Objective-C shim under `internal/wkwebview` exposes only a C ABI for AppKit and WKWebView operations; cgo connects that ABI to the existing Go WebView contract.

## Runtime behavior

- Normal runs use hidden 1280×800 WKWebView windows and the same reusable Go-owned realm pool as Linux.
- `--headed` shows one debugging window by default and enables Web Inspector on macOS 13.3 or newer.
- JavaScript bridge calls cross one WKScriptMessageHandler and resolve as Go-backed promises. Rush's in-page runtime continues to batch suite results before that crossing.
- Named sessions launch additional copies of the same Rush executable. There is no separate adapter executable, Swift runtime, or extracted WebView library.
- Native clicks, typing, and key presses use Core Graphics only through the explicit trusted-input API. They require a headed window and Accessibility permission.
- The adapter can capture the current WKWebView as PNG and serialize the current DOM for failure artifacts.

## Build and inspect

```sh
npm ci
make build
otool -L bin/rush
```

The native dependencies shown by `otool` must be Apple system libraries and frameworks. In particular, the output must not contain `libwebview`, `libswift`, or `RushWKWebViewAdapter`.

## Validate

```sh
make test
./bin/rush doctor
./bin/rush test examples/basic.test.ts examples/browser-api.test.ts examples/javascript.test.js
./bin/rush stop
```

The macOS GitHub Actions job runs the same adapter test and normal CLI path on `macos-14`, then audits the executable's dynamic linkage.
