import Foundation
import SwiftUI

@MainActor
final class AppViewModel: ObservableObject {
  struct OnePasswordConnectError: LocalizedError {
    let message: String

    var errorDescription: String? {
      message
    }
  }

  enum OnePasswordStatusTone {
    case neutral
    case success
    case warning
    case error
  }

  @Published var helperStatus = "Starting helper..."
  @Published var onePasswordStatus = "Not integrated yet"
  @Published var onePasswordStatusDetail: String?
  @Published var onePasswordStatusTone: OnePasswordStatusTone = .neutral
  @Published var metadataPath = "Loading..."
  @Published var settingsPath = "Loading..."
  @Published var configs: [RemoteConfigSummary] = []
  @Published var lastError: String?
  @Published var isLoading = false
  @Published var isEditorPresented = false
  @Published var isAccountSettingsPresented = false
  @Published var editorDraft = ConfigDraft()
  @Published var settingsDraft = AppSettings()
  @Published var generatingIDs: Set<String> = []
  @Published var editorError: String?
  @Published var settingsError: String?
  @Published var availableVaults: [OnePasswordVaultOption] = []
  @Published var availableItems: [OnePasswordItemOption] = []
  @Published var isLoadingEditorOptions = false
  @Published var isImportCreateFlow = false
  @Published var isOnePasswordAuthorized = false
  @Published var needsOnePasswordReconnect = false
  @Published var needsOnePasswordAuthorization = false
  @Published var accountSettingsAccountsDraft: [String] = []
  @Published var accountSettingsSelectedAccount = ""
  @Published var accountSettingsNewAccount = ""

  private let helperClient: HelperClient
  private let onePasswordStatusTimeout: TimeInterval = 30.0
  private let onePasswordInteractiveTimeout: TimeInterval = 180.0
  private let onePasswordInteractiveAttemptTimeout: TimeInterval = 10.0
  private let onePasswordHeartbeatIntervalNanoseconds: UInt64 = 5 * 60 * 1_000_000_000
  private var onePasswordHeartbeatTask: Task<Void, Never>?

  init(helperClient: HelperClient) {
    self.helperClient = helperClient
  }

  var configuredAccounts: [String] {
    settingsDraft.onePasswordAccounts
  }

  var onePasswordActionTitle: String {
    if needsOnePasswordAuthorization {
      return "Authorize 1Password"
    }
    if needsOnePasswordReconnect {
      return "Reconnect 1Password"
    }
    return "Connect 1Password"
  }

  var shouldShowOnePasswordAction: Bool {
    !settingsDraft.selectedOnePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
      && (needsOnePasswordAuthorization || needsOnePasswordReconnect)
  }

  var onePasswordStatusColor: Color {
    switch onePasswordStatusTone {
    case .neutral:
      return .secondary
    case .success:
      return .green
    case .warning:
      return .orange
    case .error:
      return .red
    }
  }

  func bootstrap() {
    Task { await refresh() }
  }

  func refresh() async {
    isLoading = true
    defer { isLoading = false }

    do {
      let helperClient = self.helperClient
      let health = try await run { try helperClient.healthCheck() }
      helperStatus = "Helper ok (\(health.version))"

      let response = try await run { try helperClient.listConfigs() }
      metadataPath = response.path
      configs = response.configs.sorted(by: {
        $0.settingName.localizedCaseInsensitiveCompare($1.settingName) == .orderedAscending
      })

      let settings = try await run { try helperClient.getSettings() }
      settingsDraft = settings.settings
      settingsDraft.normalize()
      settingsPath = settings.path
      let selectedAccount = settingsDraft.selectedOnePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines)
      if selectedAccount.isEmpty {
        updateOnePasswordDisplay(
          isConfigured: false,
          accountName: settingsDraft.selectedOnePasswordAccountName,
          fallbackMessage: health.onePassword.message
        )
      } else {
        do {
          let statusTimeout = onePasswordStatusTimeout
          let statusResponse = try await run {
            try helperClient.onePasswordStatus(accountName: selectedAccount, timeout: statusTimeout)
          }
          applyOnePasswordStatus(statusResponse.status, fallbackAccountName: selectedAccount)
        } catch {
          applyOnePasswordError(error, accountName: selectedAccount)
        }
      }
      lastError = nil
    } catch {
      lastError = error.localizedDescription
      helperStatus = "Helper error"
    }
  }

  func beginCreate(importExisting: Bool) {
    var draft = ConfigDraft()
    draft.onePasswordAccountName = selectedOnePasswordAccountName
    editorDraft = draft
    isImportCreateFlow = importExisting
    availableVaults = []
    availableItems = []
    editorError = nil
    isEditorPresented = true
  }

  func beginEdit(_ config: RemoteConfigSummary) {
    editorError = nil
    isLoading = true
    Task {
      defer { isLoading = false }
      do {
        let helperClient = self.helperClient
        let timeout = onePasswordInteractiveTimeout
        let result = try await run {
          try helperClient.getConfig(id: config.id, timeout: timeout)
        }
        isImportCreateFlow = false
        editorDraft = result.config
        editorDraft.onePasswordAccountName = selectedOnePasswordAccountName
        isEditorPresented = true
        lastError = nil
        _ = await ensureOnePasswordAuthorized()
        await loadVaults(selectedVaultID: result.config.vaultID)
        await selectVault(result.config.vaultID)
      } catch {
        isImportCreateFlow = false
        editorDraft = ConfigDraft(config: config)
        editorDraft.onePasswordAccountName = selectedOnePasswordAccountName
        isEditorPresented = true
        editorError =
          "Editing with local summary only. Existing secret fields will be preserved if left blank. Underlying error: \(error.localizedDescription)"
        _ = await ensureOnePasswordAuthorized()
        await loadVaults(selectedVaultID: config.vaultID)
        await selectVault(config.vaultID)
      }
    }
  }

  func saveDraft() async {
    isLoading = true
    defer { isLoading = false }

    do {
      let helperClient = self.helperClient
      var draft = editorDraft
      draft.onePasswordAccountName = selectedOnePasswordAccountName
      let requestDraft = draft
      let timeout = onePasswordInteractiveTimeout
      if let id = draft.id, !id.isEmpty {
        _ = try await run {
          try helperClient.updateConfig(requestDraft, timeout: timeout)
        }
      } else {
        _ = try await run {
          try helperClient.createConfig(requestDraft, timeout: timeout)
        }
      }
      isEditorPresented = false
      isImportCreateFlow = false
      editorDraft = ConfigDraft()
      editorError = nil
      await refresh()
    } catch {
      editorError = error.localizedDescription
    }
  }

  func delete(_ config: RemoteConfigSummary) async {
    isLoading = true
    defer { isLoading = false }

    do {
      let helperClient = self.helperClient
      _ = try await run { try helperClient.deleteConfig(id: config.id) }
      configs.removeAll(where: { $0.id == config.id })
      lastError = nil
    } catch {
      lastError = error.localizedDescription
    }
  }

  func generate(_ config: RemoteConfigSummary) async {
    generatingIDs.insert(config.id)
    defer { generatingIDs.remove(config.id) }

    do {
      let helperClient = self.helperClient
      _ = try await run { try helperClient.generateConfig(id: config.id) }
      await refresh()
    } catch {
      applyOnePasswordError(error, accountName: config.onePasswordAccountName)
      lastError = error.localizedDescription
      await refresh()
    }
  }

  func cancelGenerate(_ config: RemoteConfigSummary) async {
    do {
      let helperClient = self.helperClient
      _ = try await run { try helperClient.cancelGenerate(id: config.id) }
      lastError = "Cancelled generation for \(config.settingName)."
    } catch {
      lastError = error.localizedDescription
    }
  }

  func beginAccountSettings() {
    accountSettingsAccountsDraft = settingsDraft.onePasswordAccounts
    accountSettingsSelectedAccount = selectedOnePasswordAccountName
    accountSettingsNewAccount = ""
    settingsError = nil
    isAccountSettingsPresented = true
  }

  func addAccountToDraft() {
    let trimmed = accountSettingsNewAccount.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return }
    accountSettingsAccountsDraft = [trimmed]
    accountSettingsSelectedAccount = trimmed
    accountSettingsNewAccount = ""
  }

  func removeAccountFromDraft(_ account: String) {
    accountSettingsAccountsDraft.removeAll(where: { $0 == account })
    if accountSettingsSelectedAccount == account {
      accountSettingsSelectedAccount = accountSettingsAccountsDraft.first ?? ""
    }
  }

  func saveAccountSettings() async {
    let previousAccount = selectedOnePasswordAccountName
    var next = settingsDraft
    let selectedAccount = accountSettingsSelectedAccount.trimmingCharacters(in: .whitespacesAndNewlines)
    next.onePasswordAccounts = selectedAccount.isEmpty ? [] : [selectedAccount]
    next.selectedOnePasswordAccountName = selectedAccount
    next.normalize()
    let requestSettings = next

    isLoading = true
    defer { isLoading = false }
    do {
      let helperClient = self.helperClient
      let response = try await run { try helperClient.updateSettings(requestSettings) }
      let accountChanged = previousAccount != response.settings.selectedOnePasswordAccountName
      if accountChanged {
        try await run { try helperClient.restart() }
      }
      settingsDraft = response.settings
      settingsDraft.normalize()
      settingsPath = response.path
      if accountChanged {
        isOnePasswordAuthorized = false
        needsOnePasswordReconnect = false
        needsOnePasswordAuthorization = false
        onePasswordStatusDetail = nil
      }
      if isEditorPresented {
        editorDraft.onePasswordAccountName = selectedOnePasswordAccountName
        editorDraft.vaultID = ""
        editorDraft.itemID = ""
        editorDraft.existingItemID = ""
        availableVaults = []
        availableItems = []
      }
      settingsError = nil
      isAccountSettingsPresented = false
      updateOnePasswordDisplay(
        isConfigured: !settingsDraft.selectedOnePasswordAccountName.isEmpty,
        accountName: settingsDraft.selectedOnePasswordAccountName,
        fallbackMessage: "1Password account is not configured"
      )
    } catch {
      settingsError = error.localizedDescription
    }
  }

  func reconnectConfiguredOnePassword() async {
    let accountName = selectedOnePasswordAccountName
    guard !accountName.isEmpty else { return }
    _ = await connectOnePassword(forceReconnect: true, accountName: accountName)
  }

  func connectOnePassword(forceReconnect: Bool = false, accountName: String? = nil) async -> Bool {
    let resolvedAccount = (accountName ?? selectedOnePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    guard !resolvedAccount.isEmpty else {
      updateOnePasswordDisplay(
        isConfigured: false,
        accountName: resolvedAccount,
        fallbackMessage: "1Password account is not configured"
      )
      editorError = "1Password account is not configured."
      return false
    }

    if forceReconnect {
      isOnePasswordAuthorized = false
      stopOnePasswordHeartbeat()
      availableVaults = []
      availableItems = []
    }

    isLoadingEditorOptions = true
    defer { isLoadingEditorOptions = false }

    do {
      let vaultsResponse = try await waitForOnePasswordConnection(
        accountName: resolvedAccount,
        forceReconnect: forceReconnect
      )
      availableVaults = sortVaults(vaultsResponse.vaults)
      setOnePasswordDisplay(
        title: "Connected (\(resolvedAccount))",
        detail: "1Password desktop app is reachable.",
        tone: .success,
        isAuthorized: true,
        needsReconnect: false,
        needsAuthorization: false
      )
      if forceReconnect {
        await clearReconnectErrorsAndRefresh()
      }
      editorError = nil
      lastError = nil
      return true
    } catch {
      availableVaults = []
      availableItems = []
      applyOnePasswordError(error, accountName: resolvedAccount)
      editorError = error.localizedDescription
      return false
    }
  }

  func ensureOnePasswordAuthorized(accountName: String? = nil, forceReconnect: Bool = false) async -> Bool {
    let resolvedAccount = (accountName ?? selectedOnePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    if isOnePasswordAuthorized && !forceReconnect && resolvedAccount == currentOnePasswordAccountName {
      return true
    }
    return await connectOnePassword(forceReconnect: forceReconnect || needsOnePasswordReconnect, accountName: resolvedAccount)
  }

  func loadVaults(selectedVaultID: String? = nil) async {
    let accountName = selectedOnePasswordAccountName
    guard await ensureOnePasswordAuthorized(accountName: accountName) else { return }

    isLoadingEditorOptions = true
    defer { isLoadingEditorOptions = false }
    do {
      let helperClient = self.helperClient
      let timeout = onePasswordInteractiveTimeout
      let vaultsResponse = try await run {
        try helperClient.onePasswordVaults(accountName: accountName, timeout: timeout)
      }
      availableVaults = sortVaults(vaultsResponse.vaults)

      let resolvedVaultID: String
      if let selectedVaultID, !selectedVaultID.isEmpty {
        resolvedVaultID = selectedVaultID
      } else if !editorDraft.vaultID.isEmpty {
        resolvedVaultID = editorDraft.vaultID
      } else {
        resolvedVaultID = ""
      }

      if !resolvedVaultID.isEmpty && editorDraft.vaultID.isEmpty {
        editorDraft.vaultID = resolvedVaultID
      }
      editorError = nil
    } catch {
      applyOnePasswordError(error, accountName: accountName)
      editorError = error.localizedDescription
    }
  }

  func selectVault(_ vaultID: String) async {
    editorDraft.vaultID = vaultID
    editorDraft.existingItemID = ""
    availableItems = []
    await loadItemsForSelectedVault(vaultID)
  }

  func loadItemsForSelectedVault(_ vaultID: String?) async {
    let resolvedVaultID = (vaultID ?? editorDraft.vaultID).trimmingCharacters(in: .whitespacesAndNewlines)
    let accountName = selectedOnePasswordAccountName
    guard !resolvedVaultID.isEmpty else {
      availableItems = []
      editorDraft.existingItemID = ""
      return
    }
    guard await ensureOnePasswordAuthorized(accountName: accountName) else { return }

    isLoadingEditorOptions = true
    defer { isLoadingEditorOptions = false }
    availableItems = []
    editorDraft.existingItemID = ""
    do {
      let helperClient = self.helperClient
      let timeout = onePasswordInteractiveTimeout
      let itemsResponse = try await run {
        try helperClient.onePasswordItems(
          accountName: accountName,
          vaultID: resolvedVaultID,
          timeout: timeout
        )
      }
      availableItems = itemsResponse.items.sorted(by: {
        let lhs = $0.settingName.isEmpty ? $0.title : $0.settingName
        let rhs = $1.settingName.isEmpty ? $1.title : $1.settingName
        return lhs.localizedCaseInsensitiveCompare(rhs) == .orderedAscending
      })
      editorError = nil
    } catch {
      applyOnePasswordError(error, accountName: accountName)
      availableItems = []
      editorError = error.localizedDescription
    }
  }

  func selectExistingItem(_ itemID: String) async {
    let vaultID = editorDraft.vaultID
    let accountName = selectedOnePasswordAccountName
    guard !itemID.isEmpty, !vaultID.isEmpty else { return }
    guard await ensureOnePasswordAuthorized(accountName: accountName) else { return }
    do {
      let helperClient = self.helperClient
      let timeout = onePasswordInteractiveTimeout
      let result = try await run {
        try helperClient.onePasswordItemConfig(
          accountName: accountName,
          vaultID: vaultID,
          itemID: itemID,
          timeout: timeout
        )
      }
      var imported = result.config
      imported.id = editorDraft.id
      imported.existingItemID = itemID
      imported.onePasswordAccountName = accountName
      editorDraft = imported
      editorError = nil
    } catch {
      applyOnePasswordError(error, accountName: accountName)
      editorError = error.localizedDescription
    }
  }

  private func run<T: Sendable>(_ work: @escaping @Sendable () throws -> T) async throws -> T {
    try await Task.detached(priority: .userInitiated, operation: work).value
  }

  private var currentOnePasswordAccountName: String {
    let account = selectedOnePasswordAccountName
    return account.isEmpty ? "no account selected" : account
  }

  var selectedOnePasswordAccountName: String {
    settingsDraft.selectedOnePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines)
  }

  private func setOnePasswordDisplay(
    title: String,
    detail: String?,
    tone: OnePasswordStatusTone,
    isAuthorized: Bool,
    needsReconnect: Bool,
    needsAuthorization: Bool
  ) {
    onePasswordStatus = title
    onePasswordStatusDetail = detail
    onePasswordStatusTone = tone
    isOnePasswordAuthorized = isAuthorized
    needsOnePasswordReconnect = needsReconnect
    needsOnePasswordAuthorization = needsAuthorization
    syncOnePasswordHeartbeat(isAuthorized: isAuthorized)
  }

  private func applyOnePasswordStatus(_ status: OnePasswordStatus, fallbackAccountName: String?) {
    let account = (status.accountName ?? fallbackAccountName ?? settingsDraft.selectedOnePasswordAccountName)
      .trimmingCharacters(in: .whitespacesAndNewlines)
    let resolvedAccount = account.isEmpty ? currentOnePasswordAccountName : account

    if !status.configured && account.isEmpty {
      updateOnePasswordDisplay(isConfigured: false, accountName: account, fallbackMessage: status.message)
      return
    }
    if status.connected {
      setOnePasswordDisplay(
        title: "Connected (\(resolvedAccount))",
        detail: "1Password desktop app is reachable.",
        tone: .success,
        isAuthorized: true,
        needsReconnect: false,
        needsAuthorization: false
      )
      return
    }

    let guidance = onePasswordGuidance(message: status.message)
    if isAuthorizationRequired(status.message) {
      setOnePasswordDisplay(
        title: "Authorization required (\(resolvedAccount))",
        detail: guidance,
        tone: .error,
        isAuthorized: false,
        needsReconnect: false,
        needsAuthorization: true
      )
      return
    }
    if isReconnectRequired(status.message) {
      setOnePasswordDisplay(
        title: "Reconnect required (\(resolvedAccount))",
        detail: guidance,
        tone: .warning,
        isAuthorized: false,
        needsReconnect: true,
        needsAuthorization: false
      )
      return
    }

    setOnePasswordDisplay(
      title: "Unavailable (\(resolvedAccount))",
      detail: guidance,
      tone: .error,
      isAuthorized: false,
      needsReconnect: false,
      needsAuthorization: false
    )
  }

  private func applyOnePasswordError(_ error: Error, accountName: String?) {
    let message = error.localizedDescription
    let status = OnePasswordStatus(
      configured: !(accountName ?? settingsDraft.selectedOnePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
      connected: false,
      accountName: accountName,
      message: message
    )
    applyOnePasswordStatus(status, fallbackAccountName: accountName)
  }

  private func updateOnePasswordDisplay(isConfigured: Bool, accountName: String?, fallbackMessage: String) {
    let account = (accountName ?? settingsDraft.selectedOnePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    if !isConfigured && account.isEmpty {
      setOnePasswordDisplay(
        title: "Not configured",
        detail: fallbackMessage,
        tone: .neutral,
        isAuthorized: false,
        needsReconnect: false,
        needsAuthorization: false
      )
      return
    }
    if isOnePasswordAuthorized {
      setOnePasswordDisplay(
        title: "Connected (\(account.isEmpty ? currentOnePasswordAccountName : account))",
        detail: "1Password desktop app is reachable.",
        tone: .success,
        isAuthorized: true,
        needsReconnect: false,
        needsAuthorization: false
      )
      return
    }
    if needsOnePasswordAuthorization {
      setOnePasswordDisplay(
        title: "Authorization required (\(account.isEmpty ? currentOnePasswordAccountName : account))",
        detail: "Open 1Password and approve access for this app, then try again.",
        tone: .error,
        isAuthorized: false,
        needsReconnect: false,
        needsAuthorization: true
      )
      return
    }
    if needsOnePasswordReconnect {
      setOnePasswordDisplay(
        title: "Reconnect required (\(account.isEmpty ? currentOnePasswordAccountName : account))",
        detail: "Open 1Password, make sure desktop integration is enabled, then reconnect.",
        tone: .warning,
        isAuthorized: false,
        needsReconnect: true,
        needsAuthorization: false
      )
      return
    }
    setOnePasswordDisplay(
      title: "Configured (\(account.isEmpty ? currentOnePasswordAccountName : account))",
      detail: "Connection has not been checked yet.",
      tone: .neutral,
      isAuthorized: false,
      needsReconnect: false,
      needsAuthorization: false
    )
  }

  private func sortVaults(_ vaults: [OnePasswordVaultOption]) -> [OnePasswordVaultOption] {
    vaults.sorted(by: { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending })
  }

  private func clearReconnectErrorsAndRefresh() async {
    do {
      let helperClient = self.helperClient
      try await run {
        try helperClient.clearConfigErrorSummaries(timeout: 5.0)
      }
      await refresh()
    } catch {
      // Reconnect should still succeed even if stale error cleanup fails.
    }
  }

  private func shouldRequireReconnect(_ error: Error) -> Bool {
    isAuthorizationRequired(error.localizedDescription) || isReconnectRequired(error.localizedDescription)
  }

  private func shouldRetryInteractiveConnection(_ error: Error) -> Bool {
    shouldRequireReconnect(error) || error.localizedDescription.lowercased().contains("not found")
  }

  private func isAuthorizationRequired(_ message: String) -> Bool {
    message.lowercased().contains("denied authorization")
  }

  private func isReconnectRequired(_ message: String) -> Bool {
    let normalized = message.lowercased()
    let retryableInitError = normalized.contains("error initializing client")
      && (normalized.contains("return code: -2")
        || normalized.contains("return code: -3")
        || normalized.contains("return code: -7"))
    return normalized.contains("connection channel is closed")
      || normalized.contains("connection was unexpectedly dropped by the desktop app")
      || normalized.contains("desktop application not found")
      || normalized.contains("request timed out")
      || normalized.contains("timed out")
      || retryableInitError
  }

  private func onePasswordGuidance(message: String) -> String {
    let normalized = message
      .replacingOccurrences(of: "requestfailed: ", with: "", options: .caseInsensitive)
      .trimmingCharacters(in: .whitespacesAndNewlines)
    if isAuthorizationRequired(normalized) {
      return "Open 1Password and approve access for this app, then click Authorize 1Password."
    }
    if normalized.lowercased().contains("desktop application not found") {
      return "Start the 1Password desktop app, then reconnect."
    }
    if normalized.lowercased().contains("timed out") {
      return "1Password did not respond in time. Reopen 1Password and try again."
    }
    if isReconnectRequired(normalized) {
      return "1Password desktop integration was interrupted. Reopen 1Password and reconnect."
    }
    return normalized
  }

  private func waitForOnePasswordConnection(accountName: String, forceReconnect: Bool) async throws -> OnePasswordVaultsResponse {
    let helperClient = self.helperClient
    let timeout = onePasswordInteractiveTimeout
    if !forceReconnect {
      return try await run {
        try helperClient.onePasswordVaults(accountName: accountName, timeout: timeout)
      }
    }

    let deadline = Date().addingTimeInterval(timeout)

    setOnePasswordDisplay(
      title: "Connecting (\(accountName))",
      detail: "Waiting for 1Password to launch and authorize this app.",
      tone: .neutral,
      isAuthorized: false,
      needsReconnect: false,
      needsAuthorization: false
    )

    var lastError: Error?
    while Date() < deadline {
      do {
        let remaining = max(1.0, deadline.timeIntervalSinceNow)
        let attemptTimeout = min(onePasswordInteractiveAttemptTimeout, remaining)
        try await run {
          try helperClient.restart()
        }
        let vaultsResponse = try await run {
          try helperClient.onePasswordVaults(accountName: accountName, timeout: attemptTimeout)
        }
        return vaultsResponse
      } catch {
        lastError = error
        if !shouldRetryInteractiveConnection(error) {
          throw error
        }
        try? await Task.sleep(nanoseconds: 1_000_000_000)
      }
    }

    throw lastError ?? OnePasswordConnectError(message: "Timed out while waiting for 1Password authorization.")
  }

  private func startOnePasswordHeartbeat() {
    guard onePasswordHeartbeatTask == nil else { return }
    onePasswordHeartbeatTask = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(nanoseconds: self?.onePasswordHeartbeatIntervalNanoseconds ?? 0)
        guard let self else { return }
        await self.performOnePasswordHeartbeat()
      }
    }
  }

  private func stopOnePasswordHeartbeat() {
    onePasswordHeartbeatTask?.cancel()
    onePasswordHeartbeatTask = nil
  }

  private func syncOnePasswordHeartbeat(isAuthorized: Bool) {
    if isAuthorized {
      startOnePasswordHeartbeat()
    } else {
      stopOnePasswordHeartbeat()
    }
  }

  private func performOnePasswordHeartbeat() async {
    let accountName = selectedOnePasswordAccountName
    guard isOnePasswordAuthorized, !accountName.isEmpty else { return }

    do {
      let helperClient = self.helperClient
      let timeout = onePasswordStatusTimeout
      let statusResponse = try await run {
        try helperClient.onePasswordStatus(accountName: accountName, timeout: timeout)
      }
      applyOnePasswordStatus(statusResponse.status, fallbackAccountName: accountName)
    } catch {
      applyOnePasswordError(error, accountName: accountName)
    }
  }
}
