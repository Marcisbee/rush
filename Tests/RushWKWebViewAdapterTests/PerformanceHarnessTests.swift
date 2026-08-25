import Foundation
import XCTest
@testable import RushWKWebViewAdapter

final class PerformanceHarnessTests: XCTestCase {
    @MainActor
    func testWarmThousandAssertionHostOverhead() async throws {
        let median = try await medianWarmDuration(javaScript: """
            var passed = 0;
            for (let index = 0; index < 1000; index += 1) {
              if (index === index) passed += 1;
            }
            __rushBridge.emit('benchmark', { name: 'assertions', passed });
            __rushBridge.flush();
            """)

        XCTAssertLessThan(
            median,
            0.250,
            "1,000 warm trivial assertions exceeded the 250 ms Rush host-overhead target"
        )
    }

    @MainActor
    func testWarmThousandDOMOperationsHostOverhead() async throws {
        let median = try await medianWarmDuration(javaScript: """
            const root = document.createElement('main');
            document.body.replaceChildren(root);
            for (let index = 0; index < 1000; index += 1) {
              const node = document.createElement('span');
              node.dataset.index = String(index);
              root.append(node);
              if (root.querySelector(`[data-index="${index}"]`) !== node) {
                throw new Error('DOM query mismatch');
              }
              node.textContent = String(index + 1);
            }
            __rushBridge.emit('benchmark', { name: 'dom', passed: 1000 });
            __rushBridge.flush();
            """)

        XCTAssertLessThan(
            median,
            1.0,
            "1,000 warm DOM create/query/mutate operations exceeded the 1 s Rush target"
        )
    }

    @MainActor
    private func medianWarmDuration(javaScript: String) async throws -> TimeInterval {
        let adapter = try RushWKWebViewAdapter(configuration: .init(realmCount: 1))
        try await adapter.start()
        defer { adapter.stop() }
        let lease = try await adapter.acquireRealm(sessionID: "performance")
        let realm = try adapter.realm(for: lease)

        // One unmeasured iteration pays WebKit's first-evaluation and JIT startup cost.
        try await realm.evaluateJavaScript(javaScript)
        _ = await adapter.mailbox.nextBatch()

        var samples: [TimeInterval] = []
        for _ in 0..<10 {
            let started = ProcessInfo.processInfo.systemUptime
            try await realm.evaluateJavaScript(javaScript)
            _ = await adapter.mailbox.nextBatch()
            samples.append(ProcessInfo.processInfo.systemUptime - started)
        }

        try await adapter.releaseRealm(lease)
        try await adapter.releaseSession("performance")
        return samples.sorted()[samples.count / 2]
    }
}
