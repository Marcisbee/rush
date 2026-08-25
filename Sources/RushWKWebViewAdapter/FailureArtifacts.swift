import AppKit
import Foundation
import WebKit

public struct FailureArtifacts: Equatable, Sendable {
    public let screenshot: URL
    public let domSnapshot: URL
    public let metadata: URL
}

struct FailureMetadata: Codable {
    let realmID: UUID
    let generation: UInt64
    let pageURL: String?
    let capturedAt: Date
}

@MainActor
enum FailureArtifactCollector {
    static func capture(
        realm: WKWebViewRealm,
        name: String,
        directory: URL
    ) async throws -> FailureArtifacts {
        let fileManager = FileManager.default
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        let stem = sanitized(name)
        let screenshotURL = directory.appendingPathComponent("\(stem).png")
        let domURL = directory.appendingPathComponent("\(stem).html")
        let metadataURL = directory.appendingPathComponent("\(stem).json")

        let image = try await snapshot(of: realm.webView)
        guard let tiff = image.tiffRepresentation,
              let bitmap = NSBitmapImageRep(data: tiff),
              let png = bitmap.representation(using: .png, properties: [:])
        else {
            throw WKWebViewAdapterError.screenshotEncodingFailed
        }
        try png.write(to: screenshotURL, options: .atomic)

        let rawDOM = try await realm.evaluateJavaScript(
            "document.documentElement ? document.documentElement.outerHTML : ''"
        )
        let dom = (rawDOM as? String) ?? ""
        try Data(dom.utf8).write(to: domURL, options: .atomic)

        let metadata = FailureMetadata(
            realmID: realm.id,
            generation: realm.generation,
            pageURL: realm.webView.url?.absoluteString,
            capturedAt: Date()
        )
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        try encoder.encode(metadata).write(to: metadataURL, options: .atomic)

        return FailureArtifacts(
            screenshot: screenshotURL,
            domSnapshot: domURL,
            metadata: metadataURL
        )
    }

    private static func snapshot(of webView: WKWebView) async throws -> NSImage {
        try await withCheckedThrowingContinuation { continuation in
            webView.takeSnapshot(with: nil) { image, error in
                if let image {
                    continuation.resume(returning: image)
                } else {
                    continuation.resume(
                        throwing: error ?? WKWebViewAdapterError.screenshotCaptureFailed
                    )
                }
            }
        }
    }

    private static func sanitized(_ value: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_"))
        let scalars = value.unicodeScalars.map { allowed.contains($0) ? Character(String($0)) : "-" }
        let result = String(scalars).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return result.isEmpty ? "failure" : String(result.prefix(120))
    }
}
