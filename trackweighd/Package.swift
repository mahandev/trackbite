// swift-tools-version:5.9
//
// trackweighd — a tiny Swift binary that turns the MacBook's Force Touch
// trackpad into a weighing scale and prints grams to stdout.
//
// Why a separate binary instead of doing this inside Go?
//   The trackpad's raw pressure comes from Apple's private
//   `MultitouchSupport.framework`. Calling that from Go would mean cgo +
//   Objective-C and hand-managing private-symbol resolution. Writing a
//   ~100-line Swift program that depends on the community-maintained
//   `OpenMultitouchSupport` wrapper is dramatically simpler and easier
//   to read — which matters because this code is part of a learning
//   project. The Go side just spawns this binary and reads grams from
//   its stdout, one float per line.
//
import PackageDescription

let package = Package(
    name: "trackweighd",
    platforms: [.macOS(.v12)],
    dependencies: [
        // Aladdin Free Paul's Swift wrapper around MultitouchSupport.
        // Pinned to a known-good tag; bump intentionally.
        .package(
            url: "https://github.com/Kyome22/OpenMultitouchSupport.git",
            from: "1.2.0"
        )
    ],
    targets: [
        .executableTarget(
            name: "trackweighd",
            dependencies: [
                .product(name: "OpenMultitouchSupport", package: "OpenMultitouchSupport")
            ]
        )
    ]
)
