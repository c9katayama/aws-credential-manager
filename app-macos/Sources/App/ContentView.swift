import AppKit
import SwiftUI

struct ContentView: View {
  @ObservedObject var viewModel: AppViewModel
  let onQuit: () -> Void
  @State private var configPendingDelete: RemoteConfigSummary?
  @State private var isCreateModePickerPresented = false

  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      header

      if let lastError = viewModel.lastError {
        Text(lastError)
          .font(.footnote)
          .foregroundStyle(.red)
      }

      if viewModel.configs.isEmpty {
        VStack(spacing: 8) {
          Image(systemName: "tray")
            .font(.largeTitle)
            .foregroundStyle(.secondary)
          Text("No Configs")
            .font(.headline)
          Text("Create a config or import an existing 1Password item.")
          .font(.footnote)
          .foregroundStyle(.secondary)
          .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
      } else {
        List {
          ForEach(viewModel.configs) { config in
            ConfigRowView(
              config: config,
              isGenerating: viewModel.generatingIDs.contains(config.id),
              onGenerate: {
                Task {
                  await viewModel.generate(config)
                }
              },
              onCancelGenerate: {
                Task {
                  await viewModel.cancelGenerate(config)
                }
              },
              onEdit: { viewModel.beginEdit(config) },
              onDelete: {
                configPendingDelete = config
              }
            )
          }
        }
        .frame(minHeight: 240)
      }

      footer
    }
    .padding(16)
    .frame(width: 560, height: 460)
    .sheet(isPresented: $viewModel.isEditorPresented) {
      ConfigEditorView(
        draft: $viewModel.editorDraft,
        isImportCreateFlow: viewModel.isImportCreateFlow,
        isSaving: viewModel.isLoading,
        errorMessage: viewModel.editorError,
        onePasswordStatus: viewModel.onePasswordStatus,
        onePasswordStatusDetail: viewModel.onePasswordStatusDetail,
        onePasswordActionTitle: viewModel.onePasswordActionTitle,
        onePasswordStatusColor: viewModel.onePasswordStatusColor,
        needsReconnect: viewModel.needsOnePasswordReconnect,
        isAuthorized: viewModel.isOnePasswordAuthorized,
        vaults: viewModel.availableVaults,
        items: viewModel.availableItems,
        isLoadingOptions: viewModel.isLoadingEditorOptions,
        onConnectOnePassword: {
          NSApp.activate(ignoringOtherApps: true)
          NSWorkspace.shared.launchApplication("1Password")
          Task {
            if await viewModel.connectOnePassword(
              forceReconnect: true,
              accountName: viewModel.selectedOnePasswordAccountName
            ) {
              if !viewModel.editorDraft.vaultID.isEmpty {
                await viewModel.loadItemsForSelectedVault(viewModel.editorDraft.vaultID)
              }
            }
          }
        },
        onLoadVaults: {
          Task {
            await viewModel.loadVaults(selectedVaultID: viewModel.editorDraft.vaultID)
            if !viewModel.editorDraft.vaultID.isEmpty {
              await viewModel.loadItemsForSelectedVault(viewModel.editorDraft.vaultID)
            }
          }
        },
        onLoadItems: {
          Task {
            await viewModel.loadItemsForSelectedVault(viewModel.editorDraft.vaultID)
          }
        },
        onSelectVault: { vaultID in
          Task {
            await viewModel.selectVault(vaultID)
          }
        },
        onImportItem: { itemID in
          Task {
            await viewModel.selectExistingItem(itemID)
          }
        },
        onCancel: {
          viewModel.isEditorPresented = false
          viewModel.isImportCreateFlow = false
        },
        onSave: {
          Task {
            await viewModel.saveDraft()
          }
        }
      )
    }
    .sheet(isPresented: $viewModel.isAccountSettingsPresented) {
      AccountSettingsView(
        accounts: $viewModel.accountSettingsAccountsDraft,
        selectedAccount: $viewModel.accountSettingsSelectedAccount,
        newAccountName: $viewModel.accountSettingsNewAccount,
        isSaving: viewModel.isLoading,
        errorMessage: viewModel.settingsError,
        onAddAccount: { viewModel.addAccountToDraft() },
        onRemoveAccount: { account in viewModel.removeAccountFromDraft(account) },
        onCancel: { viewModel.isAccountSettingsPresented = false },
        onSave: {
          Task {
            await viewModel.saveAccountSettings()
          }
        }
      )
    }
    .confirmationDialog(
      "Delete Config?",
      isPresented: Binding(
        get: { configPendingDelete != nil },
        set: { isPresented in
          if !isPresented {
            configPendingDelete = nil
          }
        }
      ),
      titleVisibility: .visible
    ) {
      Button("Delete", role: .destructive) {
        guard let config = configPendingDelete else { return }
        configPendingDelete = nil
        Task {
          await viewModel.delete(config)
        }
      }
      Button("Cancel", role: .cancel) {
        configPendingDelete = nil
      }
    } message: {
      if let configPendingDelete {
        Text("Delete \"\(configPendingDelete.settingName)\" from the local config list?")
      }
    }
    .confirmationDialog(
      "Add Config",
      isPresented: $isCreateModePickerPresented,
      titleVisibility: .visible
    ) {
      Button("New Item") {
        viewModel.beginCreate(importExisting: false)
      }
      Button("Import Existing") {
        viewModel.beginCreate(importExisting: true)
      }
      Button("Cancel", role: .cancel) {}
    } message: {
      Text("Choose how to add the config.")
    }
  }

  private var header: some View {
    VStack(alignment: .leading, spacing: 6) {
      HStack(alignment: .top) {
        Text(viewModel.helperStatus)
          .font(.subheadline)
        Spacer()
        Button("Refresh") {
          Task {
            await viewModel.refresh()
          }
        }
        .disabled(viewModel.isLoading)
      }
      HStack(alignment: .top, spacing: 10) {
        VStack(alignment: .leading, spacing: 3) {
          Text("1Password: \(viewModel.onePasswordStatus)")
            .font(.caption.weight(.semibold))
            .foregroundStyle(viewModel.onePasswordStatusColor)
          if let detail = viewModel.onePasswordStatusDetail, !detail.isEmpty {
            Text(detail)
              .font(.caption2)
              .foregroundStyle(.secondary)
              .fixedSize(horizontal: false, vertical: true)
          }
        }
        Spacer()
        if viewModel.shouldShowOnePasswordAction {
          Button(viewModel.onePasswordActionTitle) {
            NSApp.activate(ignoringOtherApps: true)
            NSWorkspace.shared.launchApplication("1Password")
            Task {
              await viewModel.reconnectConfiguredOnePassword()
            }
          }
        }
      }
    }
  }

  private var footer: some View {
    HStack {
      Button("Add Config") {
        if viewModel.configuredAccounts.isEmpty {
          viewModel.beginAccountSettings()
        } else {
          isCreateModePickerPresented = true
        }
      }
      Button("1Password Accounts") {
        viewModel.beginAccountSettings()
      }
      Button("Quit", action: onQuit)

      Spacer()

      if viewModel.isLoading {
        ProgressView()
          .controlSize(.small)
      }
    }
  }
}

private struct ConfigRowView: View {
  let config: RemoteConfigSummary
  let isGenerating: Bool
  let onGenerate: () -> Void
  let onCancelGenerate: () -> Void
  let onEdit: () -> Void
  let onDelete: () -> Void

  var body: some View {
    HStack(alignment: .top, spacing: 12) {
      VStack(alignment: .leading, spacing: 4) {
        HStack {
          Text(config.settingName)
            .font(.headline)
          Text(config.authType.displayName)
            .font(.caption.bold())
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
              config.authType == .sts ? Color.blue.opacity(0.15) : Color.green.opacity(0.15)
            )
            .clipShape(Capsule())
        }
        HStack(spacing: 6) {
          Text("Profile: \(config.profileName)")
            .font(.subheadline)
          Button(action: copyProfileName) {
            Image(systemName: "doc.on.doc")
              .font(.caption)
          }
          .buttonStyle(.borderless)
          .help("Copy profile name")
        }
        if isGenerating && config.authType == .sso {
          Text("Waiting for browser sign-in...")
            .font(.caption)
            .foregroundStyle(.orange)
        }
        Text(remainingText)
          .font(.caption)
          .foregroundStyle(remainingColor)
        if config.authType == .sso {
          Text(refreshTokenText)
            .font(.caption)
            .foregroundStyle(.secondary)
          if let sessionExpiryText {
            Text(sessionExpiryText)
              .font(.caption)
              .foregroundStyle(.secondary)
          }
        }
      }

      Spacer()

      VStack(alignment: .trailing, spacing: 8) {
        HStack {
          if isGenerating {
            Button("Cancel", role: .destructive, action: onCancelGenerate)
          } else {
            Button(generateButtonTitle, action: onGenerate)
          }
          Button("Edit", action: onEdit)
          Button("Delete", role: .destructive, action: onDelete)
        }
      }
    }
    .padding(.vertical, 4)
  }

  private var remainingText: String {
    if isGenerating && config.authType == .sso {
      return "Browser authorization in progress"
    }
    guard let expiration = config.lastKnownExpiration else {
      return config.lastErrorSummary ?? "No generated credentials yet"
    }
    let remaining = expiration.timeIntervalSinceNow
    if remaining <= 0 {
      return "Expired"
    }
    let minutes = Int(remaining / 60)
    if minutes < 60 {
      return "Expires in \(minutes)m"
    }
    let hours = minutes / 60
    let remMinutes = minutes % 60
    return "Expires in \(hours)h \(remMinutes)m"
  }

  private var remainingColor: Color {
    if isGenerating && config.authType == .sso {
      return .orange
    }
    guard let expiration = config.lastKnownExpiration else {
      return config.lastErrorSummary == nil ? .secondary : .red
    }
    return expiration.timeIntervalSinceNow > 0 ? .secondary : .red
  }

  private var generateButtonTitle: String {
    if !isGenerating {
      return "Generate"
    }
    return config.authType == .sso ? "Waiting..." : "Generating..."
  }

  private var refreshTokenText: String {
    let isAvailable = config.ssoRefreshTokenAvailable ?? false
    return "Refresh token: \(isAvailable ? "Loaded" : "Missing")"
  }

  private var sessionExpiryText: String? {
    guard let expiry = config.ssoSessionExpiry else {
      return nil
    }
    let remaining = expiry.timeIntervalSinceNow
    if remaining <= 0 {
      return "Session expired"
    }
    let minutes = Int(remaining / 60)
    if minutes < 60 {
      return "Session expires in \(minutes)m"
    }
    let hours = minutes / 60
    let remMinutes = minutes % 60
    return "Session expires in \(hours)h \(remMinutes)m"
  }

  private func copyProfileName() {
    let pasteboard = NSPasteboard.general
    pasteboard.clearContents()
    pasteboard.setString(config.profileName, forType: .string)
  }
}
