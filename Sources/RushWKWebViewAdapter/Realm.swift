import AppKit
import Foundation
import WebKit

public struct RealmLease: Hashable, Sendable {
    public let realmID: UUID
    let index: Int
    let generation: UInt64
    let sessionID: String?
}

@MainActor
public final class WKWebViewRealm {
    public let id: UUID
    public let webView: WKWebView

    let navigator: NavigationObserver
    private(set) var generation: UInt64 = 0
    var sessionID: String?
    var isBusy = false

    init(id: UUID, webView: WKWebView, navigator: NavigationObserver) {
        self.id = id
        self.webView = webView
        self.navigator = navigator
    }

    @discardableResult
    public func evaluateJavaScript(_ source: String) async throws -> Any? {
        try await webView.evaluateJavaScript(source)
    }

    public func load(_ request: URLRequest) async throws {
        guard let navigation = webView.load(request) else {
            throw WKWebViewAdapterError.navigationDidNotStart
        }
        try await navigator.wait(for: navigation)
    }

    public func loadHTML(_ html: String, baseURL: URL? = nil) async throws {
        guard let navigation = webView.loadHTMLString(html, baseURL: baseURL) else {
            throw WKWebViewAdapterError.navigationDidNotStart
        }
        try await navigator.wait(for: navigation)
    }
}

@MainActor
final class NavigationObserver: NSObject, WKNavigationDelegate {
    private struct Pending {
        let navigation: WKNavigation
        let continuation: CheckedContinuation<Void, Error>
    }

    private var pending: Pending?

    func wait(for navigation: WKNavigation) async throws {
        if pending != nil {
            throw WKWebViewAdapterError.navigationAlreadyInProgress
        }

        try await withCheckedThrowingContinuation { continuation in
            pending = Pending(navigation: navigation, continuation: continuation)
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        guard let pending, pending.navigation === navigation else { return }
        self.pending = nil
        pending.continuation.resume()
    }

    func webView(
        _ webView: WKWebView,
        didFail navigation: WKNavigation!,
        withError error: Error
    ) {
        finishFailed(navigation, error: error)
    }

    func webView(
        _ webView: WKWebView,
        didFailProvisionalNavigation navigation: WKNavigation!,
        withError error: Error
    ) {
        finishFailed(navigation, error: error)
    }

    private func finishFailed(_ navigation: WKNavigation?, error: Error) {
        guard let pending, pending.navigation === navigation else { return }
        self.pending = nil
        pending.continuation.resume(throwing: error)
    }
}
