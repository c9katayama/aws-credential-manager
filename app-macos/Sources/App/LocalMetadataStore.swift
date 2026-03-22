import Foundation

final class LocalMetadataStore {
  private let fileManager: FileManager
  private let encoder: JSONEncoder
  private let decoder: JSONDecoder

  init(fileManager: FileManager = .default) {
    self.fileManager = fileManager
    self.encoder = JSONEncoder()
    self.decoder = JSONDecoder()
    self.encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    self.encoder.dateEncodingStrategy = .iso8601
    self.decoder.dateDecodingStrategy = .iso8601
  }

  func load() throws -> MetadataIndex {
    let url = try indexURL()
    guard fileManager.fileExists(atPath: url.path) else {
      return .empty
    }

    let data = try Data(contentsOf: url)
    return try decoder.decode(MetadataIndex.self, from: data)
  }

  func save(_ index: MetadataIndex) throws {
    let url = try indexURL()
    let directoryURL = url.deletingLastPathComponent()
    try fileManager.createDirectory(at: directoryURL, withIntermediateDirectories: true)

    let data = try encoder.encode(index)
    let tempURL = directoryURL.appendingPathComponent(".index.json.tmp")
    try data.write(to: tempURL, options: .atomic)

    if fileManager.fileExists(atPath: url.path) {
      try fileManager.removeItem(at: url)
    }
    try fileManager.moveItem(at: tempURL, to: url)
  }

  func ensureInitialized() throws -> MetadataIndex {
    let index = try load()
    if index.schemaVersion != MetadataIndex.currentSchemaVersion {
      throw NSError(
        domain: "AwsCredentialManager.MetadataStore",
        code: 1,
        userInfo: [
          NSLocalizedDescriptionKey: "Unsupported metadata schema version: \(index.schemaVersion)"
        ]
      )
    }

    let url = try indexURL()
    if !fileManager.fileExists(atPath: url.path) {
      try save(index)
    }
    return index
  }

  private func indexURL() throws -> URL {
    guard
      let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask)
        .first
    else {
      throw NSError(
        domain: "AwsCredentialManager.MetadataStore",
        code: 2,
        userInfo: [NSLocalizedDescriptionKey: "Application Support directory is not available."]
      )
    }
    return
      appSupport
      .appendingPathComponent("aws-credential-manager", isDirectory: true)
      .appendingPathComponent("index.json")
  }
}
