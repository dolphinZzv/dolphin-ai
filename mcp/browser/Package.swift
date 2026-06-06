// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "BrowserMCP",
    platforms: [.macOS(.v15)],
    targets: [
        .target(
            name: "BrowserMCPCore",
            path: "Sources/Core"
        ),
        .executableTarget(
            name: "BrowserMCP",
            dependencies: ["BrowserMCPCore"],
            path: "Sources/BrowserMCP"
        ),
        .testTarget(
            name: "BrowserMCPTests",
            dependencies: ["BrowserMCPCore"],
            path: "Tests"
        ),
        .testTarget(
            name: "BrowserMCPUITests",
            dependencies: ["BrowserMCPCore"],
            path: "UITests"
        ),
    ]
)
