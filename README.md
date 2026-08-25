# Rush

Persistent WebView-native JavaScript and TypeScript test runner focused on extreme performance.

## Native adapters

The Windows adapter is implemented in `platform/webview2`. It keeps WebView2 controllers warm, leases reusable browser realms from a bounded pool, batches page bridge messages, captures rendered PNG and DOM failure artifacts, and exposes trusted Windows mouse and keyboard automation separately from fast synthetic page events.

Normal Windows runs use a hidden, off-screen host window. Debug mode shows the host and opens WebView2 DevTools. See [Windows WebView2 setup and validation](docs/windows-webview2.md).

The adapter-independent browser conformance and warm performance workloads live in `harness`, so other native adapters can run the same checks.
