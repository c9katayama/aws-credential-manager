import SwiftUI

struct ConfigEditorView: View {
  @Binding var draft: ConfigDraft
  let accounts: [String]
  let isImportCreateFlow: Bool
  let isSaving: Bool
  let errorMessage: String?
  let onePasswordStatus: String
  let needsReconnect: Bool
  let isAuthorized: Bool
  let vaults: [OnePasswordVaultOption]
  let items: [OnePasswordItemOption]
  let isLoadingOptions: Bool
  let onSelectAccount: (String) -> Void
  let onConnectOnePassword: () -> Void
  let onLoadVaults: () -> Void
  let onLoadItems: () -> Void
  let onSelectVault: (String) -> Void
  let onImportItem: (String) -> Void
  let onCancel: () -> Void
  let onSave: () -> Void
  @State private var vaultFilter = ""
  @State private var itemFilter = ""

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 16) {
        Text(draft.id == nil ? "Add Config" : "Edit Config")
          .font(.title3.bold())

        if let errorMessage, !errorMessage.isEmpty {
          Text(errorMessage)
            .font(.footnote)
            .foregroundStyle(.red)
        }

        if showImportPicker {
          importWizard
          if hasImportedExistingItem {
            importReviewStep
          }
          editorActions
        } else if showNewItemWizard {
          newItemWizard
        } else {
          if showVaultControls {
            regularVaultControls
          }
          configForm
          editorActions
        }
      }
      .padding(20)
      .frame(width: 520, alignment: .leading)
    }
    .onChange(of: draft.vaultID) { _ in
      itemFilter = ""
    }
  }

  private var vaultPicker: some View {
    VStack(alignment: .leading, spacing: 6) {
      if vaults.isEmpty {
        Text(isLoadingOptions ? "Loading vaults..." : "No vaults loaded.")
          .font(.caption)
          .foregroundStyle(.secondary)
      } else {
        filterField("Search vaults", text: $vaultFilter)
        Text("\(filteredVaults.count) / \(vaults.count) vaults")
          .font(.caption)
          .foregroundStyle(.secondary)
        ScrollView {
          LazyVStack(alignment: .leading, spacing: 8) {
            ForEach(Array(filteredVaults.enumerated()), id: \.element.id) { _, vault in
              vaultRow(vault)
                .buttonStyle(.plain)
            }
          }
        }
        .frame(minHeight: 120, maxHeight: 180)
      }
      if !vaults.isEmpty && !isAuthorized {
        Text("Connect 1Password before loading items.")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
    }
  }

  private var existingItemPicker: some View {
    VStack(alignment: .leading, spacing: 6) {
      if isLoadingOptions {
        Text("Loading items...")
          .font(.caption)
          .foregroundStyle(.secondary)
      } else if items.isEmpty {
        Text("No items found in the selected vault.")
          .font(.caption)
          .foregroundStyle(.secondary)
      } else {
        filterField("Search items", text: $itemFilter)
        Text("\(filteredItems.count) / \(items.count) items")
          .font(.caption)
          .foregroundStyle(.secondary)
        ScrollView {
          LazyVStack(alignment: .leading, spacing: 8) {
            ForEach(Array(filteredItems.enumerated()), id: \.element.id) { _, item in
              existingItemRow(item)
              .buttonStyle(.plain)
            }
          }
        }
        .frame(minHeight: 140, maxHeight: 220)
      }
    }
  }

  private var regularVaultControls: some View {
    VStack(alignment: .leading, spacing: 12) {
      accountPicker
      HStack(spacing: 10) {
        Text("1Password: \(onePasswordStatus)")
          .font(.footnote)
          .foregroundStyle(.secondary)
        Spacer()
        Button(needsReconnect ? "Reconnect 1Password" : "Connect 1Password", action: onConnectOnePassword)
          .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
        Button("Load Vaults", action: onLoadVaults)
          .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
      }
      vaultPicker
      if showImportPicker {
        existingItemPicker
      }
    }
  }

  private var importWizard: some View {
    VStack(alignment: .leading, spacing: 16) {
      wizardSection(
        step: 1,
        title: "Select Account"
      ) {
        VStack(alignment: .leading, spacing: 12) {
          accountPicker
          HStack(spacing: 10) {
            Text("1Password: \(onePasswordStatus)")
              .font(.footnote)
              .foregroundStyle(.secondary)
            Spacer()
            Button(needsReconnect ? "Reconnect 1Password" : "Connect 1Password", action: onConnectOnePassword)
              .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
          }
        }
      }

      if hasAuthorizedAccount {
        wizardSection(
          step: 2,
          title: "Select Vault",
          trailingAction: {
            Button(action: onLoadVaults) {
              Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
            .help("Reload vaults")
          }
        ) {
          vaultPicker
        }
      }

      if hasSelectedVault {
        wizardSection(
          step: 3,
          title: "Select Item",
          trailingAction: {
            Button(action: {
              itemFilter = ""
              onLoadItems()
            }) {
              Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .disabled(isLoadingOptions || draft.vaultID.isEmpty)
            .help("Reload items")
          }
        ) {
          existingItemPicker
        }
      }

      if hasImportedExistingItem {
        importReviewStep
      }
    }
  }

  private var importReviewStep: some View {
    wizardSection(
      step: 4,
      title: "Review and Save"
    ) {
      VStack(alignment: .leading, spacing: 16) {
        configForm
      }
    }
  }

  private var newItemWizard: some View {
    VStack(alignment: .leading, spacing: 16) {
      wizardSection(
        step: 1,
        title: "Select 1Password Destination",
        trailingAction: {
          if hasAuthorizedAccount {
            reloadButton(action: onLoadVaults, help: "Reload vaults")
              .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
          }
        }
      ) {
        VStack(alignment: .leading, spacing: 12) {
          accountPicker
          HStack(spacing: 10) {
            Text("1Password: \(onePasswordStatus)")
              .font(.footnote)
              .foregroundStyle(.secondary)
            Spacer()
            Button(needsReconnect ? "Reconnect 1Password" : "Connect 1Password", action: onConnectOnePassword)
              .disabled(isSaving || isLoadingOptions || draft.onePasswordAccountName.isEmpty)
          }
          if hasAuthorizedAccount {
            vaultPicker
          }
        }
      }

      if hasSelectedVault {
        wizardSection(
          step: 2,
          title: "Configure Settings"
        ) {
          createConfigForm
        }

        wizardSection(
          step: 3,
          title: "Save"
        ) {
          VStack(alignment: .leading, spacing: 12) {
            summaryRow("Account", value: draft.onePasswordAccountName)
            summaryRow("Vault", value: selectedVaultTitle)
            summaryRow("Setting Name", value: draft.settingName)
            summaryRow("Profile Name", value: draft.profileName)
            summaryRow("Auth Type", value: draft.authType.displayName)
            editorActions
          }
        }
      }
    }
  }

  private var configForm: some View {
    VStack(alignment: .leading, spacing: 16) {
      if showManualForm {
        Picker("Auth Type", selection: $draft.authType) {
          ForEach(AuthType.allCases) { authType in
            Text(authType.displayName).tag(authType)
          }
        }
        .pickerStyle(.segmented)
      }

      Group {
        if showSettingField {
          labeledField("Setting Name", text: $draft.settingName)
        }
        if showProfileField {
          labeledField("Profile Name", text: $draft.profileName)
        }
        if showAutoRefreshField {
          Toggle("Enable Auto Refresh", isOn: $draft.autoRefreshEnabled)
        }
        if showVaultControls && !showImportPicker {
          vaultPicker
        }
        if !draft.vaultID.isEmpty {
          labeledField("1Password Vault ID", text: $draft.vaultID, editable: false)
        }
        if !draft.itemID.isEmpty {
          labeledField("1Password Item ID", text: $draft.itemID, editable: false)
        }
      }

      if showManualForm {
        if draft.authType == .sts {
          stsFields
        } else {
          ssoFields
        }
      }
    }
  }

  private var createConfigForm: some View {
    VStack(alignment: .leading, spacing: 16) {
      Picker("Auth Type", selection: $draft.authType) {
        ForEach(AuthType.allCases) { authType in
          Text(authType.displayName).tag(authType)
        }
      }
      .pickerStyle(.segmented)

      labeledField("Setting Name", text: $draft.settingName)
      labeledField("Profile Name", text: $draft.profileName)
      Toggle("Enable Auto Refresh", isOn: $draft.autoRefreshEnabled)

      if draft.authType == .sts {
        stsFields
      } else {
        ssoFields
      }
    }
  }

  private var editorActions: some View {
    HStack {
      Spacer()
      Button("Cancel", action: onCancel)
        .keyboardShortcut(.cancelAction)
      if (!showImportPicker || hasImportedExistingItem) && (!showNewItemWizard || hasSelectedVault) {
        Button("Save to 1Password", action: onSave)
          .keyboardShortcut(.defaultAction)
          .disabled(isSaving || !isValid)
      }
    }
  }

  private func wizardSection<Content: View>(
    step: Int,
    title: String,
    @ViewBuilder trailingAction: () -> some View = { EmptyView() },
    @ViewBuilder content: () -> Content
  ) -> some View {
    VStack(alignment: .leading, spacing: 10) {
      HStack(spacing: 8) {
        Text("\(step)")
          .font(.caption.bold())
          .foregroundStyle(.white)
          .frame(width: 20, height: 20)
          .background(Circle().fill(Color.accentColor))
        Text(title)
          .font(.headline)
        Spacer()
        trailingAction()
      }
      content()
    }
    .padding(12)
    .background(
      RoundedRectangle(cornerRadius: 10)
        .fill(Color.secondary.opacity(0.08))
    )
  }

  private var hasSelectedAccount: Bool {
    !draft.onePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
  }

  private var hasAuthorizedAccount: Bool {
    hasSelectedAccount && isAuthorized
  }

  private var hasSelectedVault: Bool {
    !draft.vaultID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
  }

  private var showNewItemWizard: Bool {
    !isEditing && !isImportCreateFlow
  }

  private var selectedVaultTitle: String {
    guard let vault = vaults.first(where: { $0.id == draft.vaultID }) else {
      return draft.vaultID
    }
    return vault.displayTitle
  }

  private var accountPicker: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text("1Password Account")
        .font(.subheadline.weight(.medium))
      Picker("1Password Account", selection: $draft.onePasswordAccountName) {
        Text("Select Account").tag("")
        ForEach(accounts, id: \.self) { account in
          Text(account).tag(account)
        }
      }
      .pickerStyle(.menu)
      .onChange(of: draft.onePasswordAccountName) { account in
        onSelectAccount(account)
      }
      if accounts.isEmpty {
        Text("Configure at least one account in Accounts first.")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
    }
  }

  private func filterField(_ placeholder: String, text: Binding<String>) -> some View {
    HStack(spacing: 8) {
      Image(systemName: "magnifyingglass")
        .foregroundStyle(.secondary)
      TextField(placeholder, text: text)
        .textFieldStyle(.plain)
        .autocorrectionDisabled()
    }
    .padding(.horizontal, 12)
    .padding(.vertical, 10)
    .background(
      RoundedRectangle(cornerRadius: 10)
        .fill(Color.black.opacity(0.12))
    )
    .overlay(
      RoundedRectangle(cornerRadius: 10)
        .stroke(Color.white.opacity(0.08), lineWidth: 1)
    )
  }

  private func reloadButton(action: @escaping () -> Void, help: String) -> some View {
    Button(action: action) {
      Image(systemName: "arrow.clockwise")
    }
    .buttonStyle(.borderless)
    .help(help)
  }

  private func summaryRow(_ label: String, value: String) -> some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(label)
        .font(.caption)
        .foregroundStyle(.secondary)
      Text(value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "-" : value)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(
          RoundedRectangle(cornerRadius: 8)
            .fill(Color.secondary.opacity(0.08))
        )
    }
  }

  private var isEditing: Bool {
    draft.id != nil
  }

  private var showImportPicker: Bool {
    !isEditing && isImportCreateFlow
  }

  private var showVaultControls: Bool {
    true
  }

  private var showManualForm: Bool {
    isEditing || !isImportCreateFlow || hasImportedExistingItem
  }

  private var hasImportedExistingItem: Bool {
    !draft.existingItemID.isEmpty
  }

  private var showProfileField: Bool {
    !showImportPicker || hasImportedExistingItem
  }

  private var showSettingField: Bool {
    !showImportPicker || hasImportedExistingItem
  }

  private var showAutoRefreshField: Bool {
    !showImportPicker
  }

  private var filteredItems: [OnePasswordItemOption] {
    let needle = itemFilter.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !needle.isEmpty else {
      return items
    }
    let normalizedNeedle = normalizeFilterToken(needle)

    return items.filter { item in
      let name = item.settingName.isEmpty ? item.title : item.settingName
      return matchesFilter(name, needle: needle, normalizedNeedle: normalizedNeedle)
        || matchesFilter(item.title, needle: needle, normalizedNeedle: normalizedNeedle)
        || matchesFilter(item.itemID, needle: needle, normalizedNeedle: normalizedNeedle)
        || matchesFilter(item.authType, needle: needle, normalizedNeedle: normalizedNeedle)
    }
  }

  private var filteredVaults: [OnePasswordVaultOption] {
    let needle = vaultFilter.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !needle.isEmpty else {
      return vaults
    }
    let normalizedNeedle = normalizeFilterToken(needle)

    return vaults.filter { vault in
      matchesFilter(vault.displayTitle, needle: needle, normalizedNeedle: normalizedNeedle)
        || matchesFilter(vault.title, needle: needle, normalizedNeedle: normalizedNeedle)
        || matchesFilter(vault.id, needle: needle, normalizedNeedle: normalizedNeedle)
    }
  }

  private func matchesFilter(_ candidate: String, needle: String, normalizedNeedle: String) -> Bool {
    if candidate.localizedCaseInsensitiveContains(needle) {
      return true
    }
    return normalizeFilterToken(candidate).contains(normalizedNeedle)
  }

  private func normalizeFilterToken(_ value: String) -> String {
    let lowered = value.lowercased()
    let scalars = lowered.unicodeScalars.filter { scalar in
      CharacterSet.alphanumerics.contains(scalar)
    }
    return String(String.UnicodeScalarView(scalars))
  }

  private func existingItemRow(_ item: OnePasswordItemOption) -> some View {
    Button {
      draft.existingItemID = item.itemID
      onImportItem(item.itemID)
    } label: {
      HStack {
        VStack(alignment: .leading, spacing: 2) {
          Text(item.title)
            .foregroundStyle(.primary)
            .frame(maxWidth: .infinity, alignment: .leading)
          if !item.settingName.isEmpty && item.settingName != item.title {
            Text(item.settingName)
              .font(.caption2)
              .foregroundStyle(.secondary)
          }
          if !item.authType.isEmpty {
            Text(item.authType.uppercased())
              .font(.caption2)
              .foregroundStyle(.secondary)
          }
        }
        if draft.existingItemID == item.itemID {
          Image(systemName: "checkmark.circle.fill")
            .foregroundStyle(Color.accentColor)
        }
      }
      .padding(.horizontal, 10)
      .padding(.vertical, 8)
      .background(
        RoundedRectangle(cornerRadius: 8)
          .fill(
            draft.existingItemID == item.itemID
              ? Color.accentColor.opacity(0.15)
              : Color.secondary.opacity(0.08)
          )
      )
    }
  }

  private func vaultRow(_ vault: OnePasswordVaultOption) -> some View {
    Button {
      itemFilter = ""
      draft.vaultID = vault.id
      onSelectVault(vault.id)
    } label: {
      HStack {
        VStack(alignment: .leading, spacing: 2) {
          Text(vault.displayTitle)
            .foregroundStyle(.primary)
            .frame(maxWidth: .infinity, alignment: .leading)
          Text(vault.id)
            .font(.caption2)
            .foregroundStyle(.secondary)
        }
        if draft.vaultID == vault.id {
          Image(systemName: "checkmark.circle.fill")
            .foregroundStyle(Color.accentColor)
        }
      }
      .padding(.horizontal, 10)
      .padding(.vertical, 8)
      .background(
        RoundedRectangle(cornerRadius: 8)
          .fill(
            draft.vaultID == vault.id
              ? Color.accentColor.opacity(0.15)
              : Color.secondary.opacity(0.08)
          )
      )
    }
  }

  private var stsFields: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("STS Configuration")
        .font(.headline)
      labeledField("AWS Access Key ID", text: $draft.awsAccessKeyId, monospaced: true)
      labeledField(
        "AWS Secret Access Key",
        text: $draft.awsSecretAccessKey,
        secure: true,
        monospaced: true
      )
      labeledField("MFA ARN", text: $draft.mfaArn)
      labeledField("MFA TOTP URI or Code", text: $draft.mfaTotp, secure: true, monospaced: true)
      labeledField("Role ARN", text: $draft.roleArn)
      labeledField("Role Session Name", text: $draft.roleSessionName)
      labeledField("External ID", text: $draft.externalId, secure: true)
      labeledField("Session Duration Minutes", text: $draft.sessionDuration)
      labeledField("STS Region", text: $draft.stsRegion)
    }
  }

  private var ssoFields: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("SSO Configuration")
        .font(.headline)
      labeledField("SSO Start URL", text: $draft.ssoStartUrl, monospaced: true)
      labeledField("SSO Region", text: $draft.ssoRegion)
      labeledField("Username", text: $draft.ssoUsername)
      labeledField("Password", text: $draft.ssoPassword, secure: true, monospaced: true)
      labeledField("MFA TOTP URI or Code", text: $draft.ssoMfaTotp, secure: true, monospaced: true)
      labeledField("AWS Account ID", text: $draft.ssoAccountId)
      labeledField("AWS Role Name", text: $draft.ssoRoleName)
      labeledField("Session Duration Minutes", text: $draft.sessionDuration)
    }
  }

  @ViewBuilder
  private func labeledField(
    _ title: String,
    text: Binding<String>,
    secure: Bool = false,
    editable: Bool = true,
    monospaced: Bool = false
  ) -> some View {
    VStack(alignment: .leading, spacing: 6) {
      Text(title)
        .font(.subheadline.weight(.medium))
      if secure {
        SecureField(title, text: text)
          .textFieldStyle(.roundedBorder)
          .font(monospaced ? .system(.body, design: .monospaced) : .body)
          .autocorrectionDisabled()
          .disabled(!editable)
      } else {
        TextField(title, text: text)
          .textFieldStyle(.roundedBorder)
          .font(monospaced ? .system(.body, design: .monospaced) : .body)
          .autocorrectionDisabled()
          .disabled(!editable)
      }
    }
  }

  private var isValid: Bool {
    let hasAccountName = !draft.onePasswordAccountName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    guard hasAccountName else {
      return false
    }

    if showImportPicker && !hasImportedExistingItem {
      return false
    }

    let hasSettingName = !draft.settingName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    let hasProfileName = !draft.profileName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    let commonValid = hasSettingName && hasProfileName
    let isEditing = draft.id != nil

    if showImportPicker {
      return hasSettingName
    }

    switch draft.authType {
    case .sts:
      if isEditing {
        return commonValid
      }
      return commonValid
        && !draft.awsAccessKeyId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.awsSecretAccessKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.mfaArn.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.mfaTotp.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    case .sso:
      if isEditing {
        return commonValid
      }
      return commonValid
        && !draft.ssoStartUrl.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoRegion.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoUsername.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoPassword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoMfaTotp.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoAccountId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        && !draft.ssoRoleName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
  }
}
