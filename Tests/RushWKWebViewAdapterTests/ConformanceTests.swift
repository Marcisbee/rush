import Foundation
import XCTest
@testable import RushWKWebViewAdapter

final class ConformanceTests: XCTestCase {
    @MainActor
    func testHiddenRealmBatchesBridgeMessages() async throws {
        let adapter = try RushWKWebViewAdapter(
            configuration: .init(realmCount: 1, displayMode: .hidden)
        )
        try await adapter.start()
        defer { adapter.stop() }

        let lease = try await adapter.acquireRealm()
        let realm = try adapter.realm(for: lease)
        let result = try await realm.evaluateJavaScript(
            """
            __rushBridge.emit('assertion', { passed: true });
            __rushBridge.emit('result', { tests: 1 });
            __rushBridge.flush();
            """
        )
        XCTAssertNil(result, "JavaScript undefined must bridge to nil without trapping")

        let batch = await adapter.mailbox.nextBatch()
        XCTAssertEqual(batch.realmID, lease.realmID)
        XCTAssertEqual(batch.sequence, 0)
        XCTAssertEqual(batch.messages, [
            BridgeMessage(type: "assertion", payload: .object(["passed": .bool(true)])),
            BridgeMessage(type: "result", payload: .object(["tests": .number(1)])),
        ])
        try await adapter.releaseRealm(lease)
    }

    @MainActor
    func testNamedSessionKeepsTheSameWarmRealm() async throws {
        let adapter = try RushWKWebViewAdapter(configuration: .init(realmCount: 1))
        try await adapter.start()
        defer { adapter.stop() }

        let first = try await adapter.acquireRealm(sessionID: "realtime-client-a")
        let firstRealm = try adapter.realm(for: first)
        try await firstRealm.evaluateJavaScript("globalThis.sessionMarker = 41")
        try await adapter.releaseRealm(first)

        let second = try await adapter.acquireRealm(sessionID: "realtime-client-a")
        let secondRealm = try adapter.realm(for: second)
        XCTAssertEqual(first.realmID, second.realmID)
        XCTAssertEqual(firstRealm.generation, secondRealm.generation)
        let marker = try await secondRealm.evaluateJavaScript("globalThis.sessionMarker + 1") as? Int
        XCTAssertEqual(marker, 42)
        try await adapter.releaseRealm(second)
        try await adapter.releaseSession("realtime-client-a")
    }

    @MainActor
    func testTransientRealmIsResetBeforeReuse() async throws {
        let adapter = try RushWKWebViewAdapter(configuration: .init(realmCount: 1))
        try await adapter.start()
        defer { adapter.stop() }

        let first = try await adapter.acquireRealm()
        let firstRealm = try adapter.realm(for: first)
        try await firstRealm.evaluateJavaScript(
            "document.body.innerHTML = '<button id=old>Old</button>'; globalThis.leaked = true"
        )
        let generation = firstRealm.generation
        try await adapter.releaseRealm(first)

        let second = try await adapter.acquireRealm()
        let secondRealm = try adapter.realm(for: second)
        XCTAssertEqual(first.realmID, second.realmID)
        XCTAssertGreaterThan(secondRealm.generation, generation)
        let wasReset = try await secondRealm.evaluateJavaScript(
            "document.querySelector('#old') === null && globalThis.leaked === undefined"
        ) as? Bool
        XCTAssertEqual(wasReset, true)
        try await adapter.releaseRealm(second)
    }

    @MainActor
    func testFailureCaptureWritesScreenshotDOMAndMetadata() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("rush-artifacts-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let adapter = try RushWKWebViewAdapter(
            configuration: .init(realmCount: 1, artifactDirectory: directory)
        )
        try await adapter.start()
        defer { adapter.stop() }

        let lease = try await adapter.acquireRealm()
        let realm = try adapter.realm(for: lease)
        try await realm.evaluateJavaScript(
            "document.body.innerHTML = '<main data-rush-failure>broken state</main>'"
        )

        let artifacts = try await adapter.captureFailure(for: lease, named: "failed: test / one")
        XCTAssertTrue(FileManager.default.fileExists(atPath: artifacts.screenshot.path))
        XCTAssertTrue(
            try String(contentsOf: artifacts.domSnapshot).contains("data-rush-failure")
        )
        let metadata = try Data(contentsOf: artifacts.metadata)
        let metadataObject = try JSONSerialization.jsonObject(with: metadata) as? [String: Any]
        XCTAssertEqual(metadataObject?["realmID"] as? String, lease.realmID.uuidString)
        try await adapter.releaseRealm(lease)
    }

    @MainActor
    func testTrustedInputIsExplicitlyUnavailableForHiddenRuns() async throws {
        let adapter = try RushWKWebViewAdapter(
            configuration: .init(realmCount: 1, displayMode: .hidden)
        )
        try await adapter.start()
        defer { adapter.stop() }
        let lease = try await adapter.acquireRealm()

        XCTAssertThrowsError(try adapter.trustedInput(for: lease).click(at: .zero)) {
            XCTAssertEqual(
                $0 as? WKWebViewAdapterError,
                .trustedInputRequiresVisibleWindow
            )
        }
        try await adapter.releaseRealm(lease)
    }
}
