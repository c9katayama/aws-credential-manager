# aws-credential-manager Tasks

## 1. Document Status

- Status: Ready for implementation
- Based on: [SPEC.md](/Users/yaman/git/aws-credential-manager/SPEC.md)
- Last updated: 2026-03-14

## 2. Delivery Strategy

Implementation proceeds in this order:

1. Bootstrap the repository and app shell
2. Implement local metadata and IPC contracts
3. Implement 1Password integration
4. Implement STS flow end to end
5. Implement SSO flow end to end
6. Add auto-refresh and polish
7. Add tests and developer tooling

## 3. Milestones

### M1. App Shell and Project Bootstrap

Goal:

- Launch a macOS menu bar app and a Go helper process locally

Tasks:

- Create `app-macos/` Xcode project for a menu bar app
- Create `core-go/` Go module for the helper process
- Decide and implement IPC transport between Swift and Go
- Add development run scripts for launching both components together
- Add single-instance guard for the macOS app
- Add basic structured logging on both sides

Done when:

- The menu bar icon appears
- The app can start and stop the Go helper
- The Swift shell can make a health check request to the Go helper

### M2. Core Domain Models and Contracts

Goal:

- Define stable request/response contracts and config models before feature work

Tasks:

- Define shared config model for `sts` and `sso` item metadata
- Define IPC payloads for:
  - health check
  - list configs
  - create config
  - update config
  - generate credentials
  - delete config
  - refresh status
- Define local metadata schema for `index.json`
- Add schema versioning for local metadata and 1Password items
- Add error model shared by Swift UI and Go helper

Done when:

- The app and helper exchange typed payloads without ad hoc JSON
- `index.json` format is fixed and versioned

### M3. Local Metadata Store

Goal:

- Persist only non-secret config metadata for fast startup

Tasks:

- Implement `index.json` store in `~/Library/Application Support/aws-credential-manager/`
- Store:
  - local config ID
  - setting name
  - auth type
  - profile name
  - vault ID
  - item ID
  - auto-refresh enabled
  - last known expiration
  - last refresh time
  - last error summary
- Add load, save, update, remove operations
- Add migration path for future schema versions
- Add corruption handling and safe fallback behavior

Done when:

- The app starts and renders using local metadata without scanning all 1Password items
- Metadata survives app restart

### M4. 1Password Integration

Goal:

- Use 1Password as the system of record for secrets and config payloads

Tasks:

- Add 1Password Go SDK dependency
- Implement desktop app integration bootstrap
- Implement connection health check and authorization status reporting
- Resolve the `Private` vault by name
- Implement item create/update/read by item ID
- Implement fallback item lookup by title
- Implement item schema builders for:
  - common fields
  - STS fields
  - SSO fields
- Implement TOTP retrieval for:
  - STS MFA
  - SSO MFA
- Implement retryable error handling for authorization expiry

Done when:

- The app can create a `[aws-credential-manager] <Setting Name>` item in the `Private` vault
- The helper can read it back by item ID
- The UI can show 1Password connection status

### M5. macOS UI Foundation

Goal:

- Build the core menu bar UX and config editing flows

Tasks:

- Create menu bar popover shell
- Add top status area:
  - app name
  - 1Password connection status
  - global refresh status
- Add config list view with row model
- Add row actions:
  - `Generate`
  - `Edit`
- Add footer actions:
  - `Add STS Config`
  - `Add SSO Config`
  - `Open Settings`
  - `Quit`
- Create add/edit config form for STS
- Create add/edit config form for SSO
- Add validation and inline error rendering
- Add loading and disabled states for long-running operations

Done when:

- A user can create, edit, and list config rows from the menu bar UI
- Validation prevents incomplete or invalid submissions

### M6. Credentials File Writer

Goal:

- Safely update `~/.aws/credentials` without corrupting unrelated profiles

Tasks:

- Implement INI-style parser/updater for `~/.aws/credentials`
- Support upsert by `profile_name`
- Preserve unrelated profiles and formatting as much as practical
- Write:
  - `aws_access_key_id`
  - `aws_secret_access_key`
  - `aws_session_token`
- Implement atomic temp-file write + fsync + rename
- Create file and parent directory when missing
- Set restrictive permissions on newly created files
- Add expiration tracking in local metadata

Done when:

- Generated credentials appear under the target profile
- Existing profiles remain intact after repeated writes

### M7. STS Flow

Goal:

- Deliver credential-based AWS temporary credential generation end to end

Tasks:

- Add AWS SDK for Go dependency for STS
- Implement STS config validation
- Implement `GetSessionToken` path when `role_arn` is empty
- Implement `AssumeRole` path when `role_arn` is present
- Support optional:
  - `role_session_name`
  - `external_id`
  - `session_duration_minutes`
  - `sts_region`
- Retrieve current MFA code from 1Password TOTP at request time
- Return expiration and status to the UI
- Write resulting credentials to `~/.aws/credentials`

Done when:

- `Generate` succeeds for both STS modes
- The menu shows remaining credential lifetime after generation

### M8. STS UX and Error Handling

Goal:

- Make STS generation usable and debuggable from the menu bar

Tasks:

- Show row-level loading state during generation
- Show success timestamp and expiration after generation
- Show compact row-level error summaries
- Add detail panel or expandable message for full error information
- Handle:
  - invalid MFA
  - invalid access key
  - access denied
  - credentials file permission errors

Done when:

- A failed STS generation does not corrupt local state or the credentials file
- The user can understand the cause of the failure from the UI

### M9. SSO OIDC Foundation

Goal:

- Implement the IAM Identity Center login state machine using official APIs

Tasks:

- Add AWS SDK for Go dependencies for:
  - SSOOIDC
  - SSO
- Implement SSO config validation
- Implement OIDC client registration flow
- Request scope:
  - `sso:account:access`
- Implement device authorization start
- Open the system browser for user login
- Poll token completion
- Store access token and refresh token in memory only
- Invalidate in-memory state on sign-out, shutdown, or crash

Done when:

- A user can complete interactive browser login from the app
- The helper holds reusable in-memory SSO refresh state while the app remains running

### M10. SSO Credential Retrieval

Goal:

- Retrieve AWS temporary credentials for a chosen IAM Identity Center account and role

Tasks:

- Implement `GetRoleCredentials`
- Use:
  - `sso_start_url`
  - `sso_region`
  - `sso_account_id`
  - `sso_role_name`
- Return AWS access key ID, secret access key, session token, and expiration
- Write resulting credentials to `~/.aws/credentials`
- Update local metadata expiration and last refresh time
- Detect expired in-memory SSO refresh state and require interactive login again

Done when:

- `Generate` works for SSO configs
- Restarting the app clears SSO login state and correctly requires a new interactive login

### M11. SSO UX and Error Handling

Goal:

- Expose the SSO flow clearly in the native UI

Tasks:

- Add SSO-specific states:
  - `Browser Login Required`
  - `Refreshing`
  - `Expired`
  - `Error`
- Show browser login progress
- Show timeout and retry messages
- Handle:
  - login timeout
  - invalid account ID
  - invalid role name
  - account/role not assigned
  - refresh token expired
- Make clear that SSO auto-refresh is available only while the app process remains alive

Done when:

- A user can understand whether a config needs browser login, is refreshable, or has failed

### M12. Auto-Refresh Scheduler

Goal:

- Refresh credentials automatically before expiration

Tasks:

- Implement scheduler loop in Go
- Wake every 60 seconds
- Track expiration per config
- Trigger refresh when remaining lifetime is 10 minutes or less
- Serialize refresh attempts per config
- Prevent overlapping refreshes
- Back off after repeated failures:
  - 1 minute
  - 3 minutes
  - 5 minutes
- Re-read STS secrets and TOTP from 1Password at refresh time
- Reuse in-memory SSO refresh state while available
- Surface `1Password Authorization Required` when desktop authorization has expired
- Surface `Browser Login Required` when SSO memory state is gone

Done when:

- STS configs can auto-refresh while 1Password authorization is valid
- SSO configs can auto-refresh only until app restart or loss of in-memory refresh state

### M13. Onboarding and Settings

Goal:

- Make first-run and recovery flows understandable

Tasks:

- Add first-run onboarding sheet
- Explain prerequisites:
  - 1Password app installed
  - desktop integration enabled
  - `Private` vault available
- Add settings screen for app-level options
- Add diagnostics view for:
  - helper health
  - 1Password availability
  - metadata store path
  - credentials file path
- Add repair flow for missing 1Password items

Done when:

- A new user can reach a working first config without external documentation

### M14. Delete, Disable, and Repair Operations

Goal:

- Support full lifecycle management of config entries

Tasks:

- Add `Delete` action for local config metadata
- Decide and implement whether delete also removes the 1Password item
- Add `Disable` action that hides config from auto-refresh but keeps metadata
- Add `Repair Link` action to relink broken local metadata to an existing 1Password item
- Confirm destructive actions in UI

Done when:

- Users can cleanly remove or repair broken entries without editing files manually

### M15. Testing

Goal:

- Cover critical behavior before wider usage

Tasks:

- Add Go unit tests for:
  - metadata store
  - 1Password item mapping
  - STS request construction
  - SSO token state machine
  - credentials file merge/upsert
  - expiration calculations
- Add Swift UI or view-model tests for menu bar row state
- Add integration tests for:
  - helper health checks
  - 1Password item CRUD in a development vault
  - STS flow against a test AWS account
  - SSO flow against a test IAM Identity Center environment
  - in-memory SSO refresh lifecycle

Done when:

- Core flows have automated coverage
- Regressions in credentials writing or token refresh are detectable

### M16. Developer Experience

Goal:

- Make the project runnable by one developer machine with minimal friction

Tasks:

- Add `README.md` for development setup
- Document required tools:
  - Xcode
  - Go
  - 1Password desktop app
- Add scripts for:
  - build helper
  - run app locally
  - run tests
- Add sample screenshots or screen recording plan for future docs
- Add logging guidance and secret redaction rules for contributors

Done when:

- A developer can clone the repo and run the app locally with clear setup steps

## 4. Recommended Execution Order

Implement in this order:

1. M1 App Shell and Project Bootstrap
2. M2 Core Domain Models and Contracts
3. M3 Local Metadata Store
4. M4 1Password Integration
5. M5 macOS UI Foundation
6. M6 Credentials File Writer
7. M7 STS Flow
8. M8 STS UX and Error Handling
9. M9 SSO OIDC Foundation
10. M10 SSO Credential Retrieval
11. M11 SSO UX and Error Handling
12. M12 Auto-Refresh Scheduler
13. M13 Onboarding and Settings
14. M14 Delete, Disable, and Repair Operations
15. M15 Testing
16. M16 Developer Experience

## 5. MVP Definition

The first usable MVP should include:

- M1 App Shell and Project Bootstrap
- M2 Core Domain Models and Contracts
- M3 Local Metadata Store
- M4 1Password Integration
- M5 macOS UI Foundation
- M6 Credentials File Writer
- M7 STS Flow
- M8 STS UX and Error Handling

MVP outcome:

- A user can create an STS config stored in 1Password
- A user can generate AWS temporary credentials from the menu bar
- The app writes those credentials into `~/.aws/credentials`

## 6. Post-MVP Definition

Post-MVP begins with:

- M9 SSO OIDC Foundation
- M10 SSO Credential Retrieval
- M11 SSO UX and Error Handling
- M12 Auto-Refresh Scheduler

Outcome:

- The app supports both STS and SSO
- Auto-refresh works within the constraints defined in `SPEC.md`
