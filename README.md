# Rush

Persistent WebView-native JavaScript and TypeScript test runner focused on extreme performance.

The repository is under active development. The `@rush/browser` package contains the
public, Vitest-compatible API that executes inside a real browser page. Native hosts
provide navigation, isolated sessions, and trusted input through explicit adapters;
ordinary assertions, queries, mocks, timers, snapshots, and synthetic interactions stay
in the page.
