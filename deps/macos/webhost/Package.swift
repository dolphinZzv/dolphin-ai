// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "WebHost",
    platforms: [
        .macOS(.v13)
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-nio", from: "2.60.0"),
        .package(url: "https://github.com/apple/swift-log", from: "1.5.0")
    ],
    targets: [
        .executableTarget(
            name: "WebHost",
            dependencies: [
                .product(name: "NIO", package: "swift-nio"),
                .product(name: "NIOHTTP1", package: "swift-nio"),
                .product(name: "Logging", package: "swift-log")
            ],
            path: "Sources"
        ),
        .testTarget(
            name: "WebHostTests",
            dependencies: ["WebHost"],
            path: "Tests"
        )
    ]
)