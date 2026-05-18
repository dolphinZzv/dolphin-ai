// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "WebHost",
    platforms: [
        .macOS(.v13)
    ],
    dependencies: [],
    targets: [
        .executableTarget(
            name: "WebHost",
            dependencies: [],
            path: "Sources"
        ),
        .testTarget(
            name: "WebHostTests",
            dependencies: ["WebHost"],
            path: "Tests"
        )
    ]
)