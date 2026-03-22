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
      Text("1Password Accounts")
        .font(.title3.bold())

      if let errorMessage, !errorMessage.isEmpty {
        Text(errorMessage)
          .font(.footnote)
          .foregroundStyle(.red)
      }

      VStack(alignment: .leading, spacing: 8) {
        Text("Saved Accounts")
          .font(.headline)

        if accounts.isEmpty {
          Text("No accounts saved yet.")
            .font(.footnote)
            .foregroundStyle(.secondary)
        } else {
          ScrollView {
            LazyVStack(alignment: .leading, spacing: 8) {
              ForEach(accounts, id: \.self) { account in
                HStack(spacing: 10) {
                  Image(systemName: selectedAccount == account ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(selectedAccount == account ? Color.accentColor : Color.secondary)
                  Text(account)
                    .frame(maxWidth: .infinity, alignment: .leading)
                  Button("Remove", role: .destructive) {
                    onRemoveAccount(account)
                  }
                  .buttonStyle(.borderless)
                }
                .contentShape(Rectangle())
                .onTapGesture {
                  selectedAccount = account
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(
                  RoundedRectangle(cornerRadius: 8)
                    .fill(
                      selectedAccount == account
                        ? Color.accentColor.opacity(0.15)
                        : Color.secondary.opacity(0.08)
                    )
                )
              }
            }
          }
          .frame(minHeight: 120, maxHeight: 200)
        }
      }

      VStack(alignment: .leading, spacing: 8) {
        Text("Default Account")
          .font(.headline)
        Picker("Default Account", selection: $selectedAccount) {
          Text("Select Account").tag("")
          ForEach(accounts, id: \.self) { account in
            Text(account).tag(account)
          }
        }
        .pickerStyle(.menu)
        .disabled(accounts.isEmpty)
      }

      VStack(alignment: .leading, spacing: 8) {
        Text("Add Account")
          .font(.headline)
        HStack {
          TextField("1Password account name", text: $newAccountName)
            .textFieldStyle(.roundedBorder)
            .autocorrectionDisabled()
          Button("Add", action: onAddAccount)
            .disabled(newAccountName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
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
