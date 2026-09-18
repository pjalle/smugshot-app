// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "Smugshot",
    platforms: [.macOS(.v15)],
    targets: [
        .executableTarget(
            name: "Smugshot",
            path: "Sources/Smugshot",
            swiftSettings: [.swiftLanguageMode(.v5)]
        )
    ]
)
