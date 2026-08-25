import Foundation
import WebKit

public enum JSONValue: Codable, Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: JSONValue].self))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null:
            try container.encodeNil()
        case let .bool(value):
            try container.encode(value)
        case let .number(value):
            try container.encode(value)
        case let .string(value):
            try container.encode(value)
        case let .array(value):
            try container.encode(value)
        case let .object(value):
            try container.encode(value)
        }
    }
}

public struct BridgeMessage: Codable, Equatable, Sendable {
    public let type: String
    public let payload: JSONValue?

    public init(type: String, payload: JSONValue? = nil) {
        self.type = type
        self.payload = payload
    }
}

public struct BridgeBatch: Codable, Equatable, Sendable {
    public let realmID: UUID
    public let sequence: UInt64
    public let messages: [BridgeMessage]

    public init(realmID: UUID, sequence: UInt64, messages: [BridgeMessage]) {
        self.realmID = realmID
        self.sequence = sequence
        self.messages = messages
    }
}

public actor BridgeMailbox {
    private var buffered: [BridgeBatch] = []
    private var waiters: [CheckedContinuation<BridgeBatch, Never>] = []

    public init() {}

    public func receive(_ batch: BridgeBatch) {
        guard !batch.messages.isEmpty else { return }
        if waiters.isEmpty {
            buffered.append(batch)
        } else {
            waiters.removeFirst().resume(returning: batch)
        }
    }

    public func nextBatch() async -> BridgeBatch {
        if !buffered.isEmpty {
            return buffered.removeFirst()
        }
        return await withCheckedContinuation { waiters.append($0) }
    }

    public func drain() -> [BridgeBatch] {
        defer { buffered.removeAll(keepingCapacity: true) }
        return buffered
    }
}

@MainActor
final class BridgeScriptMessageHandler: NSObject, @preconcurrency WKScriptMessageHandler {
    private let mailbox: BridgeMailbox

    init(mailbox: BridgeMailbox) {
        self.mailbox = mailbox
    }

    func userContentController(
        _ userContentController: WKUserContentController,
        didReceive message: WKScriptMessage
    ) {
        guard JSONSerialization.isValidJSONObject(message.body),
              let data = try? JSONSerialization.data(withJSONObject: message.body),
              let batch = try? JSONDecoder().decode(BridgeBatch.self, from: data)
        else { return }

        Task { await mailbox.receive(batch) }
    }
}

enum BridgeBootstrap {
    static func script(handlerName: String, realmID: UUID) -> String {
        let encodedHandler = javascriptString(handlerName)
        let encodedRealm = javascriptString(realmID.uuidString.lowercased())

        return """
        (() => {
          const handlerName = \(encodedHandler);
          const realmID = \(encodedRealm);
          let sequence = 0;
          let scheduled = false;
          let queue = [];

          const flush = () => {
            scheduled = false;
            if (queue.length === 0) return;
            const messages = queue;
            queue = [];
            window.webkit.messageHandlers[handlerName].postMessage({
              realmID,
              sequence: sequence++,
              messages,
            });
          };

          Object.defineProperty(globalThis, "__rushBridge", {
            configurable: false,
            enumerable: false,
            value: Object.freeze({
              emit(type, payload) {
                queue.push(payload === undefined ? { type } : { type, payload });
                if (!scheduled) {
                  scheduled = true;
                  queueMicrotask(flush);
                }
              },
              flush,
            }),
          });
        })();
        """
    }

    private static func javascriptString(_ value: String) -> String {
        let data = try! JSONEncoder().encode(value)
        return String(decoding: data, as: UTF8.self)
    }
}
