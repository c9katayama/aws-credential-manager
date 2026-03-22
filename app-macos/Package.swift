// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "app-macos",
  platforms: [
    .macOS(.v13)
  ],
  products: [
    .executable(name: "AwsCredentialManagerApp", targets: ["App"])
  ],
  targets: [
    .executableTarget(
      name: "App",
      path: "Sources/App",
      resources: [
        .process("Resources")
      ]
    )
  ]
)
