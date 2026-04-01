import Foundation

final class HelperClient: @unchecked Sendable {
  enum HelperError: LocalizedError {
    case missingHelperPath
    case startupFailed(String)
    case requestTimeout
    case requestFailed(String)

    var errorDescription: String? {
      switch self {
      case .missingHelperPath:
        return "AWS credential manager helper could not be found."
      case .startupFailed(let message):
        return "Failed to start helper: \(message)"
      case .requestTimeout:
        return "The helper request timed out."
      case .requestFailed(let message):
        return message
      }
    }
  }

  private struct Request<Params: Encodable>: Encodable {
    let id: String
    let method: String
    let params: Params?
  }

  private struct ResponseEnvelope<ResultType: Decodable>: Decodable {
    let id: String
    let result: ResultType?
    let error: ResponseError?
  }

  private struct ResponseError: Decodable {
    let code: String
    let message: String
  }

  private struct EmptyParams: Encodable {}
  private struct EmptyResult: Decodable {}
  private struct DeleteParams: Encodable { let id: String }
  private struct GetParams: Encodable { let id: String }
  private struct AccountParams: Encodable { let accountName: String }
  private struct VaultParams: Encodable { let accountName: String; let vaultId: String }
  private struct ItemParams: Encodable { let accountName: String; let vaultId: String; let itemId: String }
  private struct CancelGenerateResponse: Decodable { let cancelled: Bool }

  private var process: Process?
  private var inputPipe: Pipe?
  private var outputPipe: Pipe?
  private let bufferQueue = DispatchQueue(label: "AwsCredentialManager.HelperClient.buffer")
  private let lifecycleCondition = NSCondition()
  private var buffer = Data()
  private var waiters: [String: (Result<Data, Error>) -> Void] = [:]
  private var activeRequests = 0
  private var isRestarting = false

  func start() throws {
    guard let helperPath = resolveHelperPath() else {
      throw HelperError.missingHelperPath
    }
    if let process, process.isRunning {
      return
    }

    let process = Process()
    let inputPipe = Pipe()
    let outputPipe = Pipe()

    process.executableURL = URL(fileURLWithPath: helperPath)
    process.arguments = ["serve"]
    process.standardInput = inputPipe
    process.standardOutput = outputPipe
    process.standardError = FileHandle.standardError

    outputPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
      let chunk = handle.availableData
      guard !chunk.isEmpty else { return }
      self?.consume(chunk)
    }

    do {
      try process.run()
      self.process = process
      self.inputPipe = inputPipe
      self.outputPipe = outputPipe
    } catch {
      throw HelperError.startupFailed(error.localizedDescription)
    }
  }

  private func resolveHelperPath() -> String? {
    if
      let helperPath = ProcessInfo.processInfo.environment["AWS_CREDENTIAL_MANAGER_HELPER_PATH"],
      !helperPath.isEmpty
    {
      return helperPath
    }

    if let bundledPath = Bundle.main.path(forResource: "aws-credential-manager-helper", ofType: nil) {
      return bundledPath
    }

    if
      let executableURL = Bundle.main.executableURL,
      let resourcesURL = executableURL.deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("Resources", isDirectory: true) as URL?
    {
      let candidate = resourcesURL.appendingPathComponent("aws-credential-manager-helper").path
      if FileManager.default.isExecutableFile(atPath: candidate) {
        return candidate
      }
    }

    return nil
  }

  func stop() {
    outputPipe?.fileHandleForReading.readabilityHandler = nil
    if let process, process.isRunning {
      process.terminate()
      process.waitUntilExit()
    }
    process = nil
    inputPipe = nil
    outputPipe = nil
  }

  func restart() throws {
    lifecycleCondition.lock()
    isRestarting = true
    while activeRequests > 0 {
      lifecycleCondition.wait()
    }
    lifecycleCondition.unlock()
    defer {
      lifecycleCondition.lock()
      isRestarting = false
      lifecycleCondition.broadcast()
      lifecycleCondition.unlock()
    }

    stop()
    bufferQueue.sync {
      buffer.removeAll(keepingCapacity: false)
      waiters.removeAll()
    }
    try start()
  }

  func healthCheck(timeout: TimeInterval = 5.0) throws -> HelperHealth {
    try send(method: "health.check", params: Optional<EmptyParams>.none, timeout: timeout)
  }

  func listConfigs(timeout: TimeInterval = 5.0) throws -> HelperListResponse {
    try send(method: "configs.list", params: Optional<EmptyParams>.none, timeout: timeout)
  }

  func syncConfigs(timeout: TimeInterval = 60.0) throws -> HelperListResponse {
    try send(method: "configs.sync", params: Optional<EmptyParams>.none, timeout: timeout)
  }

  func clearConfigErrorSummaries(timeout: TimeInterval = 5.0) throws {
    let _: EmptyResult = try send(method: "configs.errors.clear", params: Optional<EmptyParams>.none, timeout: timeout)
  }

  func onePasswordStatus(accountName: String, timeout: TimeInterval = 60.0) throws -> OnePasswordStatusResponse {
    try send(method: "onepassword.status", params: AccountParams(accountName: accountName), timeout: timeout)
  }

  func onePasswordReconnect(accountName: String, timeout: TimeInterval = 60.0) throws -> OnePasswordStatusResponse {
    try send(method: "onepassword.reconnect", params: AccountParams(accountName: accountName), timeout: timeout)
  }

  func onePasswordVaults(accountName: String, timeout: TimeInterval = 60.0) throws -> OnePasswordVaultsResponse {
    try send(method: "onepassword.vaults.list", params: AccountParams(accountName: accountName), timeout: timeout)
  }

  func onePasswordItems(accountName: String, vaultID: String, timeout: TimeInterval = 60.0) throws -> OnePasswordItemsResponse {
    try send(method: "onepassword.items.list", params: VaultParams(accountName: accountName, vaultId: vaultID), timeout: timeout)
  }

  func onePasswordItemConfig(accountName: String, vaultID: String, itemID: String, timeout: TimeInterval = 60.0) throws -> GetConfigResult {
    try send(method: "onepassword.items.getConfig", params: ItemParams(accountName: accountName, vaultId: vaultID, itemId: itemID), timeout: timeout)
  }

  func createConfig(_ draft: ConfigDraft, timeout: TimeInterval = 5.0) throws
    -> ConfigMutationResult
  {
    try send(method: "configs.create", params: draft, timeout: timeout)
  }

  func getConfig(id: String, timeout: TimeInterval = 5.0) throws -> GetConfigResult {
    try send(method: "configs.get", params: GetParams(id: id), timeout: timeout)
  }

  func updateConfig(_ draft: ConfigDraft, timeout: TimeInterval = 5.0) throws
    -> ConfigMutationResult
  {
    try send(method: "configs.update", params: draft, timeout: timeout)
  }

  func deleteConfig(id: String, timeout: TimeInterval = 5.0) throws -> DeleteConfigResult {
    try send(method: "configs.delete", params: DeleteParams(id: id), timeout: timeout)
  }

  func generateConfig(id: String, timeout: TimeInterval = 600.0) throws -> GenerateConfigResponse {
    try send(method: "configs.generate", params: GetParams(id: id), timeout: timeout)
  }

  func cancelGenerate(id: String, timeout: TimeInterval = 5.0) throws -> Bool {
    let response: CancelGenerateResponse = try send(
      method: "configs.generate.cancel",
      params: GetParams(id: id),
      timeout: timeout
    )
    return response.cancelled
  }

  func getSettings(timeout: TimeInterval = 5.0) throws -> SettingsResponse {
    try send(method: "settings.get", params: Optional<EmptyParams>.none, timeout: timeout)
  }

  func updateSettings(_ settings: AppSettings, timeout: TimeInterval = 5.0) throws
    -> SettingsResponse
  {
    try send(method: "settings.update", params: settings, timeout: timeout)
  }

  private func send<Params: Encodable, ResultType: Decodable>(
    method: String,
    params: Params?,
    timeout: TimeInterval
  ) throws -> ResultType {
    let writer = try beginRequest()
    defer { endRequest() }

    let requestID = UUID().uuidString
    let semaphore = DispatchSemaphore(value: 0)
    var finalResult: Result<Data, Error> = .failure(HelperError.requestTimeout)

    bufferQueue.sync {
      waiters[requestID] = { result in
        finalResult = result
        semaphore.signal()
      }
    }

    let request = Request(id: requestID, method: method, params: params)
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    let payload = try encoder.encode(request) + Data([0x0A])
    writer.write(payload)

    if semaphore.wait(timeout: .now() + timeout) == .timedOut {
      _ = bufferQueue.sync {
        waiters.removeValue(forKey: requestID)
      }
      throw HelperError.requestTimeout
    }

    switch finalResult {
    case .success(let data):
      let decoder = JSONDecoder()
      decoder.dateDecodingStrategy = .iso8601
      let envelope = try decoder.decode(ResponseEnvelope<ResultType>.self, from: data)
      if let error = envelope.error {
        throw HelperError.requestFailed("\(error.code): \(error.message)")
      }
      guard let result = envelope.result else {
        throw HelperError.requestFailed("Helper returned an empty response.")
      }
      return result
    case .failure(let error):
      throw error
    }
  }

  private func consume(_ chunk: Data) {
    bufferQueue.async {
      self.buffer.append(chunk)

      while let newline = self.buffer.firstIndex(of: 0x0A) {
        let line = self.buffer.prefix(upTo: newline)
        self.buffer.removeSubrange(...newline)
        self.handleLine(Data(line))
      }
    }
  }

  private func handleLine(_ line: Data) {
    guard !line.isEmpty else { return }

    do {
      let envelope = try JSONDecoder().decode(ResponseEnvelope<EmptyResult>.self, from: line)
      let callback = waiters.removeValue(forKey: envelope.id)
      callback?(.success(line))
    } catch {
      let pending = waiters
      waiters.removeAll()
      for callback in pending.values {
        callback(.failure(error))
      }
    }
  }

  private func beginRequest() throws -> FileHandle {
    lifecycleCondition.lock()
    while isRestarting {
      lifecycleCondition.wait()
    }
    guard let inputPipe else {
      lifecycleCondition.unlock()
      throw HelperError.startupFailed("Helper is not running.")
    }
    activeRequests += 1
    let writer = inputPipe.fileHandleForWriting
    lifecycleCondition.unlock()
    return writer
  }

  private func endRequest() {
    lifecycleCondition.lock()
    activeRequests = max(0, activeRequests - 1)
    if activeRequests == 0 {
      lifecycleCondition.broadcast()
    }
    lifecycleCondition.unlock()
  }
}
