import Foundation

enum AuthType: String, Codable, CaseIterable, Identifiable, Sendable {
  case sts
  case sso

  var id: String { rawValue }

  var displayName: String {
    switch self {
    case .sts:
      return "STS"
    case .sso:
      return "SSO"
    }
  }
}

struct LocalConfigSummary: Codable, Identifiable, Equatable, Sendable {
  let id: String
  var settingName: String
  var authType: AuthType
  var onePasswordAccountName: String
  var profileName: String
  var vaultID: String
  var itemID: String
  var autoRefreshEnabled: Bool
  var lastKnownExpiration: Date?
  var lastRefreshTime: Date?
  var lastErrorSummary: String?
}

struct MetadataIndex: Codable, Sendable {
  var schemaVersion: Int
  var configs: [LocalConfigSummary]

  static let currentSchemaVersion = 1
  static let empty = MetadataIndex(schemaVersion: currentSchemaVersion, configs: [])
}

struct HelperListResponse: Decodable, Sendable {
  let schemaVersion: Int
  let configs: [RemoteConfigSummary]
  let path: String
}

struct HelperHealth: Decodable, Sendable {
  let ok: Bool
  let version: String
  let pid: Int
  let time: String
  let onePassword: OnePasswordStatus
  let onePasswordReady: Bool
}

struct OnePasswordStatusResponse: Decodable, Sendable {
  let status: OnePasswordStatus
}

struct OnePasswordStatus: Decodable, Sendable {
  let configured: Bool
  let connected: Bool
  let accountName: String?
  let message: String
}

struct RemoteConfigSummary: Codable, Identifiable, Equatable, Sendable {
  let id: String
  var settingName: String
  var authType: AuthType
  var onePasswordAccountName: String
  var profileName: String
  var vaultID: String
  var itemID: String
  var autoRefreshEnabled: Bool
  var lastKnownExpiration: Date?
  var lastRefreshTime: Date?
  var lastErrorSummary: String?
  var ssoRefreshTokenAvailable: Bool?
  var ssoSessionExpiry: Date?
}

struct ConfigMutationResult: Decodable, Sendable {
  let config: RemoteConfigSummary
}

struct GetConfigResult: Decodable, Sendable {
  let config: ConfigDraft
}

struct DeleteConfigResult: Decodable, Sendable {
  let deletedID: String
}

struct OnePasswordVaultOption: Codable, Identifiable, Equatable, Sendable {
  let id: String
  let title: String

  var displayTitle: String {
    switch title.lowercased() {
    case "employee":
      return "従業員 (Employee)"
    case "private":
      return "Employee"
    case "personal":
      return "個人用 (Personal)"
    default:
      return title
    }
  }
}

struct OnePasswordVaultsResponse: Decodable, Sendable {
  let vaults: [OnePasswordVaultOption]
}

struct OnePasswordItemOption: Codable, Identifiable, Equatable, Sendable {
  var id: String { itemID }
  let vaultID: String
  let itemID: String
  let title: String
  let settingName: String
  let authType: String

  private enum CodingKeys: String, CodingKey {
    case vaultID = "vaultId"
    case itemID = "itemId"
    case title
    case settingName
    case authType
  }

  init(from decoder: Decoder) throws {
    let container = try decoder.container(keyedBy: CodingKeys.self)
    vaultID = try container.decodeIfPresent(String.self, forKey: .vaultID) ?? ""
    itemID = try container.decodeIfPresent(String.self, forKey: .itemID) ?? ""
    title = try container.decodeIfPresent(String.self, forKey: .title) ?? ""
    settingName = try container.decodeIfPresent(String.self, forKey: .settingName) ?? ""
    authType = try container.decodeIfPresent(String.self, forKey: .authType) ?? ""
  }
}

struct OnePasswordItemsResponse: Decodable, Sendable {
  let items: [OnePasswordItemOption]
}

struct GenerateConfigResponse: Decodable, Sendable {
  let result: GenerateConfigResult
}

struct GenerateConfigResult: Decodable, Sendable {
  let configId: String
  let profileName: String
  let authType: String
  let expiration: Date
  let lastRefreshTime: Date
  let browserUrl: String?
  let summary: RemoteConfigSummary
}

struct AppSettings: Codable, Equatable, Sendable {
  var schemaVersion = 1
  var onePasswordAccounts: [String] = []
  var selectedOnePasswordAccountName = ""
  var onePasswordAccountName = ""
  var onePasswordAccountConfigured = false

  private enum CodingKeys: String, CodingKey {
    case schemaVersion
    case onePasswordAccounts
    case selectedOnePasswordAccountName
    case onePasswordAccountName
    case onePasswordAccountConfigured
  }

  init() {}

  init(from decoder: Decoder) throws {
    let container = try decoder.container(keyedBy: CodingKeys.self)
    schemaVersion = try container.decodeIfPresent(Int.self, forKey: .schemaVersion) ?? 1
    onePasswordAccounts = try container.decodeIfPresent([String].self, forKey: .onePasswordAccounts) ?? []
    selectedOnePasswordAccountName = try container.decodeIfPresent(String.self, forKey: .selectedOnePasswordAccountName) ?? ""
    onePasswordAccountName = try container.decodeIfPresent(String.self, forKey: .onePasswordAccountName) ?? ""
    onePasswordAccountConfigured = try container.decodeIfPresent(Bool.self, forKey: .onePasswordAccountConfigured) ?? false
    normalize()
  }

  mutating func normalize() {
    if onePasswordAccounts.isEmpty && onePasswordAccountConfigured && !onePasswordAccountName.isEmpty {
      onePasswordAccounts = [onePasswordAccountName]
    }
    var seen = Set<String>()
    onePasswordAccounts = onePasswordAccounts.compactMap { account in
      let trimmed = account.trimmingCharacters(in: .whitespacesAndNewlines)
      guard !trimmed.isEmpty, !seen.contains(trimmed) else { return nil }
      seen.insert(trimmed)
      return trimmed
    }
    if selectedOnePasswordAccountName.isEmpty, let first = onePasswordAccounts.first {
      selectedOnePasswordAccountName = first
    }
    if !selectedOnePasswordAccountName.isEmpty && !onePasswordAccounts.contains(selectedOnePasswordAccountName) {
      selectedOnePasswordAccountName = onePasswordAccounts.first ?? ""
    }
    if !selectedOnePasswordAccountName.isEmpty {
      onePasswordAccounts = [selectedOnePasswordAccountName]
    } else {
      onePasswordAccounts = []
    }
    onePasswordAccountName = ""
    onePasswordAccountConfigured = false
  }
}

struct SettingsResponse: Decodable, Sendable {
  let settings: AppSettings
  let path: String
}

struct ConfigDraft: Codable, Equatable, Sendable {
  var id: String?
  var settingName = ""
  var authType: AuthType = .sts
  var onePasswordAccountName = ""
  var profileName = ""
  var vaultID = ""
  var itemID = ""
  var existingItemID = ""
  var autoRefreshEnabled = false

  var awsAccessKeyId = ""
  var awsSecretAccessKey = ""
  var mfaArn = ""
  var mfaTotp = ""
  var roleArn = ""
  var roleSessionName = ""
  var externalId = ""
  var sessionDuration = ""
  var stsRegion = ""

  var ssoStartUrl = ""
  var ssoIssuerUrl = ""
  var ssoRegion = ""
  var ssoUsername = ""
  var ssoPassword = ""
  var ssoMfaTotp = ""
  var ssoAccountId = ""
  var ssoRoleName = ""

  init() {}

  private enum CodingKeys: String, CodingKey {
    case id
    case settingName
    case authType
    case onePasswordAccountName
    case profileName
    case vaultID
    case itemID
    case existingItemID
    case autoRefreshEnabled
    case awsAccessKeyId
    case awsSecretAccessKey
    case mfaArn
    case mfaTotp
    case roleArn
    case roleSessionName
    case externalId
    case sessionDuration
    case stsRegion
    case ssoStartUrl
    case ssoIssuerUrl
    case ssoRegion
    case ssoUsername
    case ssoPassword
    case ssoMfaTotp
    case ssoAccountId
    case ssoRoleName
  }

  init(from decoder: Decoder) throws {
    let container = try decoder.container(keyedBy: CodingKeys.self)
    id = try container.decodeIfPresent(String.self, forKey: .id)
    settingName = try container.decodeIfPresent(String.self, forKey: .settingName) ?? ""
    authType = try container.decodeIfPresent(AuthType.self, forKey: .authType) ?? .sts
    onePasswordAccountName = try container.decodeIfPresent(String.self, forKey: .onePasswordAccountName) ?? ""
    profileName = try container.decodeIfPresent(String.self, forKey: .profileName) ?? ""
    vaultID = try container.decodeIfPresent(String.self, forKey: .vaultID) ?? ""
    itemID = try container.decodeIfPresent(String.self, forKey: .itemID) ?? ""
    existingItemID = try container.decodeIfPresent(String.self, forKey: .existingItemID) ?? ""
    autoRefreshEnabled = try container.decodeIfPresent(Bool.self, forKey: .autoRefreshEnabled) ?? false
    awsAccessKeyId = try container.decodeIfPresent(String.self, forKey: .awsAccessKeyId) ?? ""
    awsSecretAccessKey = try container.decodeIfPresent(String.self, forKey: .awsSecretAccessKey) ?? ""
    mfaArn = try container.decodeIfPresent(String.self, forKey: .mfaArn) ?? ""
    mfaTotp = try container.decodeIfPresent(String.self, forKey: .mfaTotp) ?? ""
    roleArn = try container.decodeIfPresent(String.self, forKey: .roleArn) ?? ""
    roleSessionName = try container.decodeIfPresent(String.self, forKey: .roleSessionName) ?? ""
    externalId = try container.decodeIfPresent(String.self, forKey: .externalId) ?? ""
    sessionDuration = try container.decodeIfPresent(String.self, forKey: .sessionDuration) ?? ""
    stsRegion = try container.decodeIfPresent(String.self, forKey: .stsRegion) ?? ""
    ssoStartUrl = try container.decodeIfPresent(String.self, forKey: .ssoStartUrl) ?? ""
    ssoIssuerUrl = try container.decodeIfPresent(String.self, forKey: .ssoIssuerUrl) ?? ""
    ssoRegion = try container.decodeIfPresent(String.self, forKey: .ssoRegion) ?? ""
    ssoUsername = try container.decodeIfPresent(String.self, forKey: .ssoUsername) ?? ""
    ssoPassword = try container.decodeIfPresent(String.self, forKey: .ssoPassword) ?? ""
    ssoMfaTotp = try container.decodeIfPresent(String.self, forKey: .ssoMfaTotp) ?? ""
    ssoAccountId = try container.decodeIfPresent(String.self, forKey: .ssoAccountId) ?? ""
    ssoRoleName = try container.decodeIfPresent(String.self, forKey: .ssoRoleName) ?? ""
  }

  init(config: RemoteConfigSummary) {
    id = config.id
    settingName = config.settingName
    authType = config.authType
    onePasswordAccountName = config.onePasswordAccountName
    profileName = config.profileName
    vaultID = config.vaultID
    itemID = config.itemID
    existingItemID = config.itemID
    autoRefreshEnabled = config.autoRefreshEnabled
  }
}
