// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "Rush",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .library(name: "RushWKWebViewAdapter", targets: ["RushWKWebViewAdapter"]),
    ],
    targets: [
        .target(
            name: "RushWKWebViewAdapter",
            path: "Sources/RushWKWebViewAdapter"
        ),
        .testTarget(
            name: "RushWKWebViewAdapterTests",
            dependencies: ["RushWKWebViewAdapter"],
            path: "Tests/RushWKWebViewAdapterTests"
        ),
    ]
)
