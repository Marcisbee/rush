# Windows WebView2 adapter

## Runtime requirements

- A supported Windows desktop environment with the [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) installed. The Evergreen Runtime is discovered by default.
- Go 1.24 or newer to build Rush. The adapter is pure Go and embeds the loader path used by `go-webview2`; it does not require a separate C/C++ toolchain.
- A writable user-data directory. Rush defaults to `.rush/webview2` and gives each pooled realm its own persistent subdirectory.
- An interactive Windows desktop only for trusted mouse or keyboard automation. Normal browser execution, JavaScript evaluation, bridge traffic, and artifact capture can use the hidden host. Windows services running in session 0 cannot use `SendInput` as trusted automation and receive an explicit focus error.

For a fixed-version WebView2 deployment, set `Config.BrowserExecutableFolder` to the unpacked runtime directory. Microsoft documents Evergreen and fixed deployment options in the [WebView2 deployment guide](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution).

## Hosting behavior

`ModeHidden` creates an off-screen Win32 host and sets the WebView2 controller invisible for normal runs. The native host and WebView2 controller remain alive when a suite releases its realm. Release navigates to a fresh document before the realm returns to the pool, replacing page-owned DOM, timers, listeners, and script globals without restarting the browser process.

`ModeDebug` creates a visible host, enables the controller, and opens the WebView2 DevTools window. It uses the same bridge and realm lifecycle as hidden execution.

Page messages are copied into ordered batches. The default batch limits are 128 messages, 256 KiB, or 2 ms, whichever is reached first. These limits can be changed independently of the page runtime.

Failure capture uses WebView2 `CapturePreview` for a real rendered PNG and evaluates `document.documentElement.outerHTML` for the DOM snapshot. Because an invisible WebView2 is transparent and not rendered, hidden failure capture briefly enables the controller while the off-screen host stays outside the desktop.

## Trusted automation

`Realm.TrustedMouse` and `Realm.TrustedKey` use Windows `SendInput`. They temporarily activate a hidden host, focus WebView2, send native input, wait for Chromium to consume the queued events, and restore hidden execution. This path produces `Event.isTrusted === true` and is intentionally separate from synthetic DOM interactions.

Trusted automation can move the user's pointer or change foreground focus. Use it only for behavior that depends on native input, such as selection, `beforeinput`, accelerator keys, or browser-default editing behavior.

## Validation harness

Build and run the harness from a Windows shell:

```powershell
go run ./cmd/rush-webview2-harness -repeats 10
```

Add `-debug` to show the WebView and open DevTools. The harness exits non-zero if browser conformance, performance targets, trusted input, bridge batching, failure capture, or realm reuse fails.

The adapter-independent conformance checks cover DOM behavior, Selection, `beforeinput`, Shadow DOM, and same-origin iframe behavior. Performance measurements report in-page work separately from native adapter overhead and use the parent Rush targets: 1,000 trivial assertions under 250 ms warm and 1,000 DOM create/query/mutate operations under 1 second warm.

The Windows harness was run on 2026-08-25 using Windows 11 build 26200 and WebView2 Runtime 151.0.4129.101. Across 10 warm repeats, median total time was 0.511 ms for 1,000 trivial assertions and 2.924 ms for 1,000 DOM operations. The run also verified ten ordered bridge messages, persistent realm reuse, real PNG and DOM artifacts, and trusted mouse and keyboard events with `isTrusted === true`. These measurements validate this machine and workload; they are not a universal guarantee for application, network, or intentional-wait behavior.
