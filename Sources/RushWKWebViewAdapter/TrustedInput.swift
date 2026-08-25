import AppKit
import ApplicationServices
import Foundation
import WebKit

public enum MouseButton: Sendable {
    case left
    case right
    case center

    fileprivate var cgButton: CGMouseButton {
        switch self {
        case .left: return .left
        case .right: return .right
        case .center: return .center
        }
    }

    fileprivate var downEvent: CGEventType {
        switch self {
        case .left: return .leftMouseDown
        case .right: return .rightMouseDown
        case .center: return .otherMouseDown
        }
    }

    fileprivate var upEvent: CGEventType {
        switch self {
        case .left: return .leftMouseUp
        case .right: return .rightMouseUp
        case .center: return .otherMouseUp
        }
    }
}

@MainActor
public final class TrustedInputController {
    private weak var webView: WKWebView?
    private weak var window: NSWindow?

    init(webView: WKWebView, window: NSWindow?) {
        self.webView = webView
        self.window = window
    }

    public static var isAccessibilityAuthorized: Bool {
        AXIsProcessTrusted()
    }

    @discardableResult
    public static func requestAccessibilityAuthorization(prompt: Bool) -> Bool {
        let key = kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String
        return AXIsProcessTrustedWithOptions([key: prompt] as CFDictionary)
    }

    public func click(
        at point: CGPoint,
        button: MouseButton = .left,
        clickCount: Int64 = 1
    ) throws {
        guard let webView, let window, window.isVisible else {
            throw WKWebViewAdapterError.trustedInputRequiresVisibleWindow
        }
        guard Self.isAccessibilityAuthorized else {
            throw WKWebViewAdapterError.accessibilityPermissionRequired
        }

        window.makeKeyAndOrderFront(nil)
        let screenPoint = cgScreenPoint(fromWebViewPoint: point, webView: webView, window: window)
        guard let down = CGEvent(
            mouseEventSource: nil,
            mouseType: button.downEvent,
            mouseCursorPosition: screenPoint,
            mouseButton: button.cgButton
        ), let up = CGEvent(
            mouseEventSource: nil,
            mouseType: button.upEvent,
            mouseCursorPosition: screenPoint,
            mouseButton: button.cgButton
        ) else {
            throw WKWebViewAdapterError.nativeEventCreationFailed
        }
        down.setIntegerValueField(.mouseEventClickState, value: clickCount)
        up.setIntegerValueField(.mouseEventClickState, value: clickCount)
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }

    public func pressKey(
        virtualKey: CGKeyCode,
        characters: String? = nil,
        modifiers: CGEventFlags = []
    ) throws {
        guard let window, window.isVisible else {
            throw WKWebViewAdapterError.trustedInputRequiresVisibleWindow
        }
        guard Self.isAccessibilityAuthorized else {
            throw WKWebViewAdapterError.accessibilityPermissionRequired
        }

        window.makeKeyAndOrderFront(nil)
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: virtualKey, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: virtualKey, keyDown: false)
        else {
            throw WKWebViewAdapterError.nativeEventCreationFailed
        }
        down.flags = modifiers
        up.flags = modifiers
        if let characters {
            let units = Array(characters.utf16)
            units.withUnsafeBufferPointer { buffer in
                down.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: buffer.baseAddress)
                up.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: buffer.baseAddress)
            }
        }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }

    public func typeText(_ text: String) throws {
        for character in text {
            try pressKey(virtualKey: 0, characters: String(character))
        }
    }

    private func cgScreenPoint(
        fromWebViewPoint point: CGPoint,
        webView: WKWebView,
        window: NSWindow
    ) -> CGPoint {
        let windowPoint = webView.convert(point, to: nil)
        let appKitPoint = window.convertPoint(toScreen: windowPoint)
        let mainScreenTop = NSScreen.screens.first?.frame.maxY ?? 0
        return CGPoint(x: appKitPoint.x, y: mainScreenTop - appKitPoint.y)
    }
}
