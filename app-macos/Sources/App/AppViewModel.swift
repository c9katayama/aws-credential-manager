import Foundation
import SwiftUI

@MainActor
final class AppViewModel: ObservableObject {
  @Published var helperStatus = "Starting helper..."
  @Published var onePasswordStatus = "Not integrated yet"
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
  @Published var accountSettingsAccountsDraft: [String] = []
  @Published var accountSettingsSelectedAccount = ""
  @Published var accountSettingsNewAccount = ""

  private let helperClient: HelperClient

  init(helperClient: HelperClient) {
    self.helperClient = helperClient
  }

  var configuredAccounts: [String] {
    settingsDraft.onePasswordAccounts
  }

  var editorAccounts: [String] {
    var accounts = settingsDraft.onePasswordAccounts
    let selected = editorDraft.onePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines)
    if !selected.isEmpty && !accounts.contains(selected) {
      accounts.append(selected)
    }
    return accounts.sorted()
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
      updateOnePasswordDisplay(
        isConfigured: !settingsDraft.selectedOnePasswordAccountName.isEmpty,
        accountName: settingsDraft.selectedOnePasswordAccountName,
        fallbackMessage: health.onePassword.message
      )
      lastError = nil
    } catch {
      lastError = error.localizedDescription
      helperStatus = "Helper error"
    }
  }

  func beginCreate(importExisting: Bool) {
    var draft = ConfigDraft()
    draft.onePasswordAccountName = settingsDraft.selectedOnePasswordAccountName
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
        let result = try await run { try helperClient.getConfig(id: config.id, timeout: 30.0) }
        isImportCreateFlow = false
        editorDraft = result.config
        if editorDraft.onePasswordAccountName.isEmpty {
          editorDraft.onePasswordAccountName = config.onePasswordAccountName
        }
        isEditorPresented = true
        lastError = nil
        _ = await ensureOnePasswordAuthorized(accountName: editorDraft.onePasswordAccountName)
        await loadVaults(selectedVaultID: result.config.vaultID)
        await selectVault(result.config.vaultID)
      } catch {
        isImportCreateFlow = false
        editorDraft = ConfigDraft(config: config)
        isEditorPresented = true
        editorError =
          "Editing with local summary only. Existing secret fields will be preserved if left blank. Underlying error: \(error.localizedDescription)"
        _ = await ensureOnePasswordAuthorized(accountName: editorDraft.onePasswordAccountName)
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
      let draft = editorDraft
      if let id = draft.id, !id.isEmpty {
        _ = try await run { try helperClient.updateConfig(draft) }
      } else {
        _ = try await run { try helperClient.createConfig(draft) }
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
    accountSettingsSelectedAccount = settingsDraft.selectedOnePasswordAccountName
    accountSettingsNewAccount = ""
    settingsError = nil
    isAccountSettingsPresented = true
  }

  func addAccountToDraft() {
    let trimmed = accountSettingsNewAccount.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return }
    guard !accountSettingsAccountsDraft.contains(trimmed) else {
      accountSettingsNewAccount = ""
      return
    }
    accountSettingsAccountsDraft.append(trimmed)
    accountSettingsAccountsDraft.sort()
    if accountSettingsSelectedAccount.isEmpty {
      accountSettingsSelectedAccount = trimmed
    }
    accountSettingsNewAccount = ""
  }

  func removeAccountFromDraft(_ account: String) {
    accountSettingsAccountsDraft.removeAll(where: { $0 == account })
    if accountSettingsSelectedAccount == account {
      accountSettingsSelectedAccount = accountSettingsAccountsDraft.first ?? ""
    }
  }

  func saveAccountSettings() async {
    var next = settingsDraft
    next.onePasswordAccounts = accountSettingsAccountsDraft
    next.selectedOnePasswordAccountName = accountSettingsSelectedAccount
    next.normalize()
    let requestSettings = next

    isLoading = true
    defer { isLoading = false }
    do {
      let helperClient = self.helperClient
      let response = try await run { try helperClient.updateSettings(requestSettings) }
      settingsDraft = response.settings
      settingsDraft.normalize()
      settingsPath = response.path
      settingsError = nil
      isAccountSettingsPresented = false
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = false
      updateOnePasswordDisplay(
        isConfigured: !settingsDraft.selectedOnePasswordAccountName.isEmpty,
        accountName: settingsDraft.selectedOnePasswordAccountName,
        fallbackMessage: "1Password account is not configured"
      )
    } catch {
      settingsError = error.localizedDescription
    }
  }

  func selectEditorAccount(_ accountName: String) {
    editorDraft.onePasswordAccountName = accountName
    editorDraft.vaultID = ""
    editorDraft.itemID = ""
    editorDraft.existingItemID = ""
    availableVaults = []
    availableItems = []
    isOnePasswordAuthorized = false
    needsOnePasswordReconnect = false
  }

  func connectOnePassword(forceReconnect: Bool = false, accountName: String? = nil) async -> Bool {
    let resolvedAccount = (accountName ?? editorDraft.onePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    guard await persistSelectedAccountIfNeeded(resolvedAccount) else {
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = false
      onePasswordStatus = "Not configured"
      editorError = "1Password account is not configured."
      return false
    }

    if forceReconnect {
      isOnePasswordAuthorized = false
    }

    isLoadingEditorOptions = true
    defer { isLoadingEditorOptions = false }

    do {
      let helperClient = self.helperClient
      if forceReconnect {
        let reconnectResponse = try await run {
          try helperClient.onePasswordReconnect(accountName: resolvedAccount, timeout: 180.0)
        }
        let reconnectStatus = reconnectResponse.status
        if !reconnectStatus.connected {
          isOnePasswordAuthorized = false
          needsOnePasswordReconnect = true
          onePasswordStatus = "Reconnect required (\(reconnectStatus.message))"
          editorError = reconnectStatus.message
          return false
        }
      }
      let vaultsResponse = try await run {
        try helperClient.onePasswordVaults(accountName: resolvedAccount, timeout: 180.0)
      }
      availableVaults = sortVaults(vaultsResponse.vaults)
      isOnePasswordAuthorized = true
      needsOnePasswordReconnect = false
      onePasswordStatus = "Connected (\(resolvedAccount))"
      editorError = nil
      lastError = nil
      return true
    } catch {
      availableVaults = []
      availableItems = []
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = shouldRequireReconnect(error)
      let statusPrefix = needsOnePasswordReconnect ? "Reconnect required" : "Unavailable"
      onePasswordStatus = "\(statusPrefix) (\(error.localizedDescription))"
      editorError = error.localizedDescription
      return false
    }
  }

  func ensureOnePasswordAuthorized(accountName: String? = nil, forceReconnect: Bool = false) async -> Bool {
    let resolvedAccount = (accountName ?? editorDraft.onePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    if isOnePasswordAuthorized && !forceReconnect && resolvedAccount == currentOnePasswordAccountName {
      return true
    }
    return await connectOnePassword(forceReconnect: forceReconnect || needsOnePasswordReconnect, accountName: resolvedAccount)
  }

  func loadVaults(selectedVaultID: String? = nil) async {
    let accountName = editorDraft.onePasswordAccountName
    guard await ensureOnePasswordAuthorized(accountName: accountName) else { return }

    isLoadingEditorOptions = true
    defer { isLoadingEditorOptions = false }
    do {
      let helperClient = self.helperClient
      let vaultsResponse = try await run {
        try helperClient.onePasswordVaults(accountName: accountName, timeout: 180.0)
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
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = shouldRequireReconnect(error)
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
    let accountName = editorDraft.onePasswordAccountName
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
      let itemsResponse = try await run {
        try helperClient.onePasswordItems(
          accountName: accountName,
          vaultID: resolvedVaultID,
          timeout: 180.0
        )
      }
      availableItems = itemsResponse.items.sorted(by: {
        let lhs = $0.settingName.isEmpty ? $0.title : $0.settingName
        let rhs = $1.settingName.isEmpty ? $1.title : $1.settingName
        return lhs.localizedCaseInsensitiveCompare(rhs) == .orderedAscending
      })
      editorError = nil
    } catch {
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = shouldRequireReconnect(error)
      availableItems = []
      editorError = error.localizedDescription
    }
  }

  func selectExistingItem(_ itemID: String) async {
    let vaultID = editorDraft.vaultID
    let accountName = editorDraft.onePasswordAccountName
    guard !itemID.isEmpty, !vaultID.isEmpty else { return }
    guard await ensureOnePasswordAuthorized(accountName: accountName) else { return }
    do {
      let helperClient = self.helperClient
      let result = try await run {
        try helperClient.onePasswordItemConfig(
          accountName: accountName,
          vaultID: vaultID,
          itemID: itemID
        )
      }
      var imported = result.config
      imported.id = editorDraft.id
      imported.existingItemID = itemID
      imported.onePasswordAccountName = accountName
      editorDraft = imported
      editorError = nil
    } catch {
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = shouldRequireReconnect(error)
      editorError = error.localizedDescription
    }
  }

  private func run<T: Sendable>(_ work: @escaping @Sendable () throws -> T) async throws -> T {
    try await Task.detached(priority: .userInitiated, operation: work).value
  }

  private var currentOnePasswordAccountName: String {
    let account = settingsDraft.selectedOnePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines)
    return account.isEmpty ? "no account selected" : account
  }

  private func updateOnePasswordDisplay(isConfigured: Bool, accountName: String?, fallbackMessage: String) {
    let account = (accountName ?? settingsDraft.selectedOnePasswordAccountName).trimmingCharacters(in: .whitespacesAndNewlines)
    if !isConfigured && account.isEmpty {
      isOnePasswordAuthorized = false
      needsOnePasswordReconnect = false
      onePasswordStatus = "Not configured (\(fallbackMessage))"
      return
    }
    if isOnePasswordAuthorized {
      onePasswordStatus = "Connected (\(account.isEmpty ? currentOnePasswordAccountName : account))"
      return
    }
    if needsOnePasswordReconnect {
      onePasswordStatus = "Reconnect required (\(account.isEmpty ? currentOnePasswordAccountName : account))"
      return
    }
    onePasswordStatus = "Configured (\(account.isEmpty ? currentOnePasswordAccountName : account))"
  }

  private func sortVaults(_ vaults: [OnePasswordVaultOption]) -> [OnePasswordVaultOption] {
    vaults.sorted(by: { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending })
  }

  private func shouldRequireReconnect(_ error: Error) -> Bool {
    let message = error.localizedDescription.lowercased()
    return message.contains("denied authorization")
      || message.contains("connection channel is closed")
      || message.contains("request timed out")
      || message.contains("timed out")
  }

  private func persistSelectedAccountIfNeeded(_ accountName: String) async -> Bool {
    let trimmed = accountName.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return false }
    guard settingsDraft.onePasswordAccounts.contains(trimmed) else {
      settingsError = "Selected 1Password account is not saved in Accounts."
      editorError = settingsError
      return false
    }

    if settingsDraft.selectedOnePasswordAccountName == trimmed {
      return true
    }

    isLoading = true
    defer { isLoading = false }

    do {
      let helperClient = self.helperClient
      var nextSettings = settingsDraft
      nextSettings.selectedOnePasswordAccountName = trimmed
      nextSettings.normalize()
      let requestSettings = nextSettings
      let response = try await run { try helperClient.updateSettings(requestSettings) }
      settingsDraft = response.settings
      settingsDraft.normalize()
      settingsPath = response.path
      settingsError = nil
      return true
    } catch {
      settingsError = error.localizedDescription
      editorError = error.localizedDescription
      return false
    }
  }
}
