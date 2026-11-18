// swift-tools-version:5.9
// iOS 壳层的 SwiftPM 描述。
//
// 为什么用 SwiftPM 而不是 .xcodeproj：
//   - 仓库保持「零二进制工程文件」，改 UI 只改 .swift；
//   - CI 上 `swift build --triple arm64-apple-ios15.0` 即可交叉编译，
//     不需要 xcodegen、也不需要手写 pbxproj；
//   - gomobile 的产物是静态 framework，用 `.binaryTarget` 引用。
//
// 注意：build-ipa.sh 会把本文件拷到 ios/build/ 下再编译，因为
// `.binaryTarget` 的 path 必须与 xcframework 同级（同级就是 ios/build）。
import PackageDescription

let package = Package(
    name: "Points",
    platforms: [.iOS(.v15)],
    products: [
        .executable(name: "Points", targets: ["Points"]),
    ],
    targets: [
        // gomobile bind 的输出（ios/build/Mobile.xcframework）
        .binaryTarget(
            name: "Mobile",
            path: "Mobile.xcframework"
        ),
        .executableTarget(
            name: "Points",
            dependencies: ["Mobile"],
            path: "Sources",
            linkerSettings: [
                .linkedFramework("SwiftUI"),
                .linkedFramework("WebKit"),
                .linkedFramework("Security"),
                .linkedFramework("BackgroundTasks"),
                .linkedLibrary("resolv"),
            ]
        ),
    ]
)
