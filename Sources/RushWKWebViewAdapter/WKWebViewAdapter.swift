import AppKit
import Foundation
import WebKit

public enum WKWebViewDisplayMode: Sendable {
    case hidden
    case debug
}

public struct WKWebViewAdapterConfiguration: Sendable {
    public var realmCount: Int
    public var displayMode: WKWebViewDisplayMode
    public var artifactDirectory: URL
    public var bridgeHandlerName: String
    public var storageNamespace: String
    public var viewportSize: CGSize

    public init(
        realmCount: Int = 2,
        displayMode: WKWebViewDisplayMode = .hidden,
        artifactDirectory: URL = URL(fileURLWithPath: ".rush/artifacts", isDirectory: true),
        bridgeHandlerName: String = "rushBridge",
        storageNamespace: String = "__rush__:",
        viewportSize: CGSize = CGSize(width: 1280, height: 800)
    ) {
        self.realmCount = realmCount
        self.displayMode = displayMode
        self.artifactDirectory = artifactDirectory
        self.bridgeHandlerName = bridgeHandlerName
        self.storageNamespace = storageNamespace
        self.viewportSize = viewportSize
    }
}

public enum WKWebViewAdapterError: LocalizedError, Equatable {
    case invalidRealmCount
    case adapterNotStarted
    case adapterAlreadyStopped
    case navigationDidNotStart
    case navigationAlreadyInProgress
    case staleLease
    case unknownRealm
    case sessionAlreadyActive(String)
    case screenshotCaptureFailed
    case screenshotEncodingFailed
    case trustedInputRequiresVisibleWindow
    case accessibilityPermissionRequired
    case nativeEventCreationFailed

    public var errorDescription: String? {
        switch self {
        case .invalidRealmCount: return "realmCount must be greater than zero"
        case .adapterNotStarted: return "the WKWebView adapter has not been started"
        case .adapterAlreadyStopped: return "the WKWebView adapter has already been stopped"
        case .navigationDidNotStart: return "WKWebView did not start the requested navigation"
        case .navigationAlreadyInProgress: return "the realm already has a navigation in progress"
        case .staleLease: return "the realm lease is no longer current"
        case .unknownRealm: return "the realm does not belong to this adapter"
        case let .sessionAlreadyActive(id): return "session \(id) already has an active lease"
        case .screenshotCaptureFailed: return "WKWebView did not return a failure screenshot"
        case .screenshotEncodingFailed: return "the failure screenshot could not be encoded as PNG"
        case .trustedInputRequiresVisibleWindow:
            return "trusted native input requires the adapter's debug display mode"
        case .accessibilityPermissionRequired:
            return "trusted native input requires macOS Accessibility permission"
        case .nativeEventCreationFailed: return "Core Graphics could not create a native input event"
        }
    }
}

@MainActor
public final class RushWKWebViewAdapter {
    private struct PendingLease {
        let sessionID: String?
        let continuation: CheckedContinuation<RealmLease, Error>
    }

    public let configuration: WKWebViewAdapterConfiguration
    public let mailbox: BridgeMailbox

    private var realms: [WKWebViewRealm] = []
    private var windows: [UUID: NSWindow] = [:]
    private var messageHandlers: [BridgeScriptMessageHandler] = []
    private var realmWaiters: [PendingLease] = []
    private var stopped = false
    private let processPool = WKProcessPool()

    public init(configuration: WKWebViewAdapterConfiguration = .init()) throws {
        guard configuration.realmCount > 0 else {
            throw WKWebViewAdapterError.invalidRealmCount
        }
        self.configuration = configuration
        self.mailbox = BridgeMailbox()
    }

    public func start() async throws {
        guard realms.isEmpty else { return }
        guard !stopped else { throw WKWebViewAdapterError.adapterAlreadyStopped }

        if case .debug = configuration.displayMode {
            NSApplication.shared.setActivationPolicy(.regular)
        }

        for _ in 0..<configuration.realmCount {
            let realm = makeRealm()
            realms.append(realm)
            try await reset(realm)
        }
    }

    public func stop() {
        guard !stopped else { return }
        stopped = true
        realmWaiters.forEach {
            $0.continuation.resume(throwing: WKWebViewAdapterError.adapterAlreadyStopped)
        }
        realmWaiters.removeAll()
        for realm in realms {
            realm.webView.stopLoading()
            realm.webView.configuration.userContentController.removeScriptMessageHandler(
                forName: configuration.bridgeHandlerName
            )
        }
        windows.values.forEach { $0.close() }
        windows.removeAll()
        messageHandlers.removeAll()
        realms.removeAll()
    }

    public func acquireRealm(sessionID: String? = nil) async throws -> RealmLease {
        guard !realms.isEmpty else { throw WKWebViewAdapterError.adapterNotStarted }

        if let sessionID,
           let index = realms.firstIndex(where: { $0.sessionID == sessionID }) {
            guard !realms[index].isBusy else {
                throw WKWebViewAdapterError.sessionAlreadyActive(sessionID)
            }
            return lease(index: index, sessionID: sessionID)
        }

        if let index = realms.firstIndex(where: { !$0.isBusy && $0.sessionID == nil }) {
            return lease(index: index, sessionID: sessionID)
        }

        return try await withCheckedThrowingContinuation {
            realmWaiters.append(PendingLease(sessionID: sessionID, continuation: $0))
        }
    }

    public func realm(for lease: RealmLease) throws -> WKWebViewRealm {
        guard realms.indices.contains(lease.index) else {
            throw WKWebViewAdapterError.unknownRealm
        }
        let realm = realms[lease.index]
        guard realm.id == lease.realmID,
              realm.generation == lease.generation,
              realm.isBusy,
              realm.sessionID == lease.sessionID
        else {
            throw WKWebViewAdapterError.staleLease
        }
        return realm
    }

    public func releaseRealm(_ lease: RealmLease) async throws {
        let realm = try realm(for: lease)
        realm.isBusy = false

        if lease.sessionID == nil {
            try await reset(realm)
            handRealmToWaiterIfPossible(realmIndex: lease.index)
        }
    }

    public func releaseSession(_ sessionID: String) async throws {
        guard let index = realms.firstIndex(where: { $0.sessionID == sessionID }) else { return }
        let realm = realms[index]
        guard !realm.isBusy else {
            throw WKWebViewAdapterError.sessionAlreadyActive(sessionID)
        }
        realm.sessionID = nil
        try await reset(realm)
        handRealmToWaiterIfPossible(realmIndex: index)
    }

    public func captureFailure(for lease: RealmLease, named name: String) async throws -> FailureArtifacts {
        try await FailureArtifactCollector.capture(
            realm: realm(for: lease),
            name: name,
            directory: configuration.artifactDirectory
        )
    }

    public func trustedInput(for lease: RealmLease) throws -> TrustedInputController {
        let realm = try realm(for: lease)
        return TrustedInputController(webView: realm.webView, window: windows[realm.id])
    }

    private func lease(index: Int, sessionID: String?) -> RealmLease {
        let realm = realms[index]
        realm.isBusy = true
        if let sessionID {
            realm.sessionID = sessionID
        }
        return RealmLease(
            realmID: realm.id,
            index: index,
            generation: realm.generation,
            sessionID: sessionID
        )
    }

    private func makeRealm() -> WKWebViewRealm {
        let id = UUID()
        let userContentController = WKUserContentController()
        let handler = BridgeScriptMessageHandler(mailbox: mailbox)
        messageHandlers.append(handler)
        userContentController.add(handler, name: configuration.bridgeHandlerName)
        userContentController.addUserScript(
            WKUserScript(
                source: BridgeBootstrap.script(
                    handlerName: configuration.bridgeHandlerName,
                    realmID: id
                ),
                injectionTime: .atDocumentStart,
                forMainFrameOnly: false
            )
        )

        let webConfiguration = WKWebViewConfiguration()
        webConfiguration.userContentController = userContentController
        webConfiguration.websiteDataStore = .nonPersistent()
        webConfiguration.processPool = processPool
        let frame = CGRect(origin: .zero, size: configuration.viewportSize)
        let webView = WKWebView(frame: frame, configuration: webConfiguration)
        let navigator = NavigationObserver()
        webView.navigationDelegate = navigator

        if #available(macOS 13.3, *) {
            webView.isInspectable = true
        }

        if case .debug = configuration.displayMode {
            let window = NSWindow(
                contentRect: frame,
                styleMask: [.titled, .closable, .resizable, .miniaturizable],
                backing: .buffered,
                defer: false
            )
            window.title = "Rush — \(id.uuidString.prefix(8))"
            window.contentView = webView
            window.makeKeyAndOrderFront(nil)
            windows[id] = window
        }

        return WKWebViewRealm(id: id, webView: webView, navigator: navigator)
    }

    private func reset(_ realm: WKWebViewRealm) async throws {
        if realm.generation > 0 {
            let namespace = Self.javascriptString(configuration.storageNamespace)
            _ = try? await realm.evaluateJavaScript(
                """
                (() => {
                  globalThis.__rushRuntimeReset?.();
                  for (const storage of [globalThis.localStorage, globalThis.sessionStorage]) {
                    const keys = [];
                    for (let index = 0; index < storage.length; index += 1) {
                      const key = storage.key(index);
                      if (key?.startsWith(\(namespace))) keys.push(key);
                    }
                    for (const key of keys) storage.removeItem(key);
                  }
                })()
                """
            )
        }
        realm.generation &+= 1
        try await realm.loadHTML(
            "<!doctype html><html><head><meta charset=\"utf-8\"></head><body></body></html>"
        )
    }

    private static func javascriptString(_ value: String) -> String {
        let data = try! JSONEncoder().encode(value)
        return String(decoding: data, as: UTF8.self)
    }

    private func handRealmToWaiterIfPossible(realmIndex: Int) {
        guard !realmWaiters.isEmpty,
              !realms[realmIndex].isBusy,
              realms[realmIndex].sessionID == nil
        else { return }
        let waiter = realmWaiters.removeFirst()
        waiter.continuation.resume(returning: lease(index: realmIndex, sessionID: waiter.sessionID))
    }
}
