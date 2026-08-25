# Rush

Rush is a persistent WebView-native JavaScript and TypeScript test runner focused on extreme performance.

## macOS WKWebView adapter

`RushWKWebViewAdapter` hosts a reusable pool of real `WKWebView` realms. Normal runs leave the views unattached to a window. Debug runs attach each realm to a visible window and mark it inspectable, so it appears in Safari's Develop menu on macOS 13.3 and newer.

The adapter injects `globalThis.__rushBridge` at document start. Calls to `emit(type, payload)` are queued in the page and delivered to Swift as one `BridgeBatch` per microtask instead of one native message per assertion. Acquiring a named session preserves its JavaScript realm until `releaseSession`; transient leases remove namespaced Rush storage and navigate back to a clean document before returning to the pool. Each realm has a process-lifetime, non-persistent website data store while all realms share a warm WebKit process pool.

```swift
import RushWKWebViewAdapter

let adapter = try await MainActor.run {
    try RushWKWebViewAdapter(configuration: .init(realmCount: 2))
}
try await adapter.start()

let lease = try await adapter.acquireRealm()
let realm = try await adapter.realm(for: lease)
try await realm.load(URLRequest(url: URL(string: "http://127.0.0.1:3000")!))
try await realm.evaluateJavaScript("runRegisteredRushSuites()")
let batch = await adapter.mailbox.nextBatch()
try await adapter.releaseRealm(lease)
```

Failure capture writes a PNG screenshot, the current document HTML, and JSON metadata through `captureFailure(for:named:)`. Artifact names are sanitized before being used as filenames.

Fast interactions should run inside the page. Trusted input is deliberately separate: use debug display mode, obtain `trustedInput(for:)`, and call its mouse or keyboard methods. Those events are posted through the Core Graphics HID event tap and require Accessibility authorization. The adapter never labels script-dispatched DOM events as trusted.

### Runtime dependencies

- macOS 13 Ventura or newer.
- Xcode 15.4 or a compatible Swift 5.9 toolchain to build the package.
- The system AppKit, ApplicationServices, and WebKit frameworks. No separately installed browser is used.
- Safari's Develop menu enabled when interactive Web Inspector access is needed.
- A logged-in macOS GUI session and Accessibility permission for the host executable when trusted native mouse or keyboard automation is requested. Hidden runs and ordinary conformance CI do not request this permission.

Grant Accessibility permission under System Settings → Privacy & Security → Accessibility. Call `TrustedInputController.requestAccessibilityAuthorization(prompt: true)` only from an interactive debugging flow; automated runs should fail with an actionable permission error instead of displaying a system prompt.

### Conformance and performance harness

Run the macOS harness from the repository root:

```sh
swift test --parallel
```

The conformance suite verifies hidden execution, persistent named sessions, transient realm reset, batched bridge delivery, failure artifacts, and the trusted-input boundary. The performance suite warms WebKit once, records ten repetitions, and checks the median host round trip against Rush's 250 ms target for 1,000 trivial assertions and 1 second target for 1,000 DOM create/query/mutate operations. GitHub Actions runs the same command on `macos-14`.
