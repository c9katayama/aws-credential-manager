import SwiftUI

struct AccountSettingsView: View {
  @Binding var accounts: [String]
  @Binding var selectedAccount: String
  @Binding var newAccountName: String
  let isSaving: Bool
  let errorMessage: String?
  let onAddAccount: () -> Void
  let onRemoveAccount: (String) -> Void
  let onCancel: () -> Void
  let onSave: () -> Void

  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("1Password Account")
        .font(.title3.bold())

      if let errorMessage, !errorMessage.isEmpty {
        Text(errorMessage)
          .font(.footnote)
          .foregroundStyle(.red)
      }

      VStack(alignment: .leading, spacing: 8) {
        Text("Configured Account")
          .font(.headline)

        if accounts.isEmpty {
          Text("No account saved yet.")
            .font(.footnote)
            .foregroundStyle(.secondary)
        } else {
          if let account = accounts.first {
            HStack(spacing: 10) {
              Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(Color.accentColor)
              Text(account)
                .frame(maxWidth: .infinity, alignment: .leading)
              Button("Remove", role: .destructive) {
                onRemoveAccount(account)
              }
              .buttonStyle(.borderless)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .background(
              RoundedRectangle(cornerRadius: 8)
                .fill(Color.accentColor.opacity(0.15))
            )
          }
        }
      }

      VStack(alignment: .leading, spacing: 8) {
        Text(accounts.isEmpty ? "Add Account" : "Replace Account")
          .font(.headline)
        HStack {
          TextField("1Password account name", text: $newAccountName)
            .textFieldStyle(.roundedBorder)
            .autocorrectionDisabled()
          Button("Add", action: onAddAccount)
            .disabled(newAccountName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        Text("Only one 1Password account can be configured at a time.")
          .font(.caption)
          .foregroundStyle(.secondary)
      }

      HStack {
        Spacer()
        Button("Cancel", action: onCancel)
          .keyboardShortcut(.cancelAction)
        Button("Save", action: onSave)
          .keyboardShortcut(.defaultAction)
          .disabled(isSaving)
      }
    }
    .padding(20)
    .frame(width: 460, alignment: .leading)
  }
}
