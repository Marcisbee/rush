import Foundation
import XCTest
@testable import RushWKWebViewAdapter

final class BridgeTests: XCTestCase {
    func testJSONValueRoundTripsNestedPayloads() throws {
        let value = JSONValue.object([
            "passed": .bool(true),
            "duration": .number(1.25),
            "names": .array([.string("one"), .null]),
        ])

        let encoded = try JSONEncoder().encode(value)
        XCTAssertEqual(try JSONDecoder().decode(JSONValue.self, from: encoded), value)
    }

    func testMailboxPreservesBatchOrder() async {
        let mailbox = BridgeMailbox()
        let first = BridgeBatch(
            realmID: UUID(),
            sequence: 3,
            messages: [BridgeMessage(type: "test", payload: .number(1))]
        )
        let second = BridgeBatch(
            realmID: UUID(),
            sequence: 4,
            messages: [BridgeMessage(type: "test", payload: .number(2))]
        )

        await mailbox.receive(first)
        await mailbox.receive(second)

        let receivedFirst = await mailbox.nextBatch()
        let receivedSecond = await mailbox.nextBatch()
        XCTAssertEqual(receivedFirst, first)
        XCTAssertEqual(receivedSecond, second)
    }

    func testMailboxDropsEmptyBatches() async {
        let mailbox = BridgeMailbox()
        await mailbox.receive(BridgeBatch(realmID: UUID(), sequence: 0, messages: []))
        let drained = await mailbox.drain()
        XCTAssertTrue(drained.isEmpty)
    }
}
