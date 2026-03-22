# aws-credential-manager Specification

## 1. Document Status

- Status: Implemented baseline
- Last updated: 2026-03-22
- Target platform: macOS
- UI language: English only

## 2. Goal

Build a macOS menu bar application that manages AWS temporary credentials with 1Password as the primary secret store.

The app supports two credential generation modes:

1. AWS STS based generation from long-lived AWS credentials
2. AWS IAM Identity Center based generation from SSO login

Generated credentials are written to `~/.aws/credentials` under a user-defined AWS profile name.

## 3. External Prerequisites

- macOS
- 1Password desktop app
- 1Password local desktop integration enabled
- Access to one or more 1Password accounts and vaults
- AWS permissions for either:
  - STS `GetSessionToken` / `AssumeRole`
  - IAM Identity Center `GetRoleCredentials`

## 4. Current Product Shape

The implemented product is a macOS menu bar app with a dedicated management window.

Major characteristics:

- Native macOS shell implemented in SwiftUI/AppKit
- Go helper process for AWS and 1Password operations
- 1Password is the system of record for secrets
- Non-secret local metadata is stored for fast startup and UI rendering
- SSO reusable session state is also stored in 1Password and loaded into memory on startup and on demand

## 5. Scope

### 5.1 In scope

- macOS menu bar app
- Dedicated app window instead of a transient popover
- 1Password desktop app integration
- Config creation, edit, delete, and generate
- Import existing 1Password items
- Per-config 1Password account selection
- Per-config 1Password vault selection
- STS `GetSessionToken`
- STS `AssumeRole`
- IAM Identity Center browser-based OIDC login
- Auto refresh
- Writing `~/.aws/credentials`
- Distributable unsigned `.app` and `.zip`

### 5.2 Out of scope

- Windows support
- App Store distribution
- Keychain persistence for SSO session state
- Browser DOM scraping of the AWS portal
- Mandatory `~/.aws/config` management as the primary flow

## 6. High-Level Architecture

### 6.1 Process model

The app has two cooperating parts:

1. Swift macOS app
2. Embedded Go helper

The Swift app starts the helper as a bundled executable.

### 6.2 IPC model

Swift and Go communicate over stdio with JSON request/response messages.

Important properties:

- one request per message
- typed payloads on both sides
- helper-side request routing
- request timeout handling on the Swift side

### 6.3 Packaging

The distributable build contains:

- `AWS Credential Manager.app`
- embedded `aws-credential-manager-helper`
- app icon and resource bundle

## 7. User Experience

### 7.1 Main window

The menu bar icon opens a standard macOS window, not a click-away popover.

The window contains:

- helper status
- 1Password status
- config list
- actions:
  - `Add Config`
  - `1Password Accounts`
  - `Refresh`
  - `Quit`

### 7.2 Config list row

Each config row shows:

- setting name
- auth type badge (`STS` or `SSO`)
- profile name
- current credential expiration state
- row-level error state if the last generation failed
- `Generate`
- `Edit`
- `Delete`

For SSO rows, the list also shows:

- refresh token presence (`Loaded` / `Missing`)
- SSO session expiry if known

### 7.3 Delete behavior

Delete requires confirmation.

The current behavior removes the local config entry. It does not automatically delete the 1Password item.

### 7.4 Cancel behavior

Long-running generation can be cancelled from the list.

This is especially important for SSO browser login flows.

## 8. 1Password Account and Vault Model

### 8.1 Account management

The app keeps a user-managed list of 1Password account names in local settings.

The user selects one of these accounts when creating or editing a config.

Settings UI label:

- `1Password Accounts`

### 8.2 Vault selection

Vault is selected per config.

The app does not assume a single fixed vault anymore.

### 8.3 Managed item title

Managed item title format:

`[aws-credential-manager] <Setting Name>`

### 8.4 Import existing item flow

The app supports importing existing 1Password items with a wizard flow:

1. Select account
2. Select vault
3. Select item
4. Review and save

The item and vault lists support partial-match filtering.

## 9. Local Storage

### 9.1 Metadata store

Location:

`~/Library/Application Support/aws-credential-manager/index.json`

Purpose:

- fast startup
- UI rendering
- auto-refresh scheduling
- local mapping from config ID to 1Password item

Stored fields:

- local config ID
- setting name
- auth type
- 1Password account name
- profile name
- vault ID
- item ID
- auto-refresh enabled
- last known AWS credential expiration
- last refresh time
- last error summary

This file must not store AWS secrets or passwords.

### 9.2 Settings store

Location:

`~/Library/Application Support/aws-credential-manager/settings.json`

Stored fields:

- saved 1Password account names
- selected default 1Password account name

## 10. 1Password Data Model

### 10.1 Item category

Managed items use the `Login` category.

### 10.2 Common fields

- `setting_name`
- `account_name`
- `profile_name`
- `auth_type`
- `auto_refresh_enabled`
- `schema_version`
- `created_by`

### 10.3 STS fields

- `aws_access_key_id`
- `aws_secret_access_key`
- `mfa_arn`
- `mfa_totp`
- `role_arn`
- `role_session_name`
- `external_id`
- `session_duration`
- `sts_region`

### 10.4 SSO fields

- `sso_start_url`
- `sso_issuer_url`  
  Internal/supporting field. The current UI does not require the user to set it.
- `sso_region`
- `sso_username`
- `sso_password`
- `sso_mfa_totp`
- `sso_account_id`
- `sso_role_name`
- `session_duration`

### 10.5 Persisted SSO session-state fields

The current implementation also stores reusable SSO session state in 1Password:

- `sso_access_token`
- `sso_access_expiry`
- `sso_refresh_token`
- `sso_client_id`
- `sso_client_secret`
- `sso_client_secret_expiry`
- `sso_last_browser_url`

Purpose:

- reload SSO state on app startup
- reuse SSO refresh state on subsequent generation
- support auto-refresh across app restart as long as the stored session state is still valid

## 11. Credential Flows

## 11.1 STS flow

### 11.1.1 GetSessionToken path

Used when `role_arn` is blank.

Steps:

1. Read STS config from 1Password
2. Resolve current MFA code from 1Password
3. Call `GetSessionToken`
4. Write returned AWS credentials to `~/.aws/credentials`
5. Record expiration metadata locally

### 11.1.2 AssumeRole path

Used when `role_arn` is present.

Steps:

1. Read STS config from 1Password
2. Resolve current MFA code from 1Password
3. Call `AssumeRole`
4. Write returned AWS credentials to `~/.aws/credentials`
5. Record expiration metadata locally

## 11.2 SSO flow

### 11.2.1 Auth model

The implemented SSO flow uses:

- OIDC authorization code flow
- PKCE
- localhost callback receiver
- AWS `GetRoleCredentials`

The app opens the default browser when interactive login is needed.

### 11.2.2 Runtime behavior

On SSO `Generate`:

1. Read SSO config from 1Password
2. Prime the in-memory session cache from persisted 1Password session-state fields
3. If a valid access token exists, use it
4. Else if a refresh token exists, refresh it
5. Else start browser-based login
6. Call `GetRoleCredentials`
7. Write returned AWS credentials to `~/.aws/credentials`
8. Persist updated SSO session state back to 1Password
9. Record expiration metadata locally

### 11.2.3 App startup behavior

On helper startup:

- local metadata is loaded
- SSO configs are identified
- each SSO config item is loaded from 1Password
- persisted SSO session state is preloaded into the in-memory cache

This means the helper can reuse refresh state without waiting for the first manual generate.

## 12. Auto Refresh

### 12.1 Scheduler

Auto refresh is implemented as a background scheduler in the Go helper.

Current behavior:

- polling interval: 60 seconds
- refresh threshold: when remaining lifetime is less than 10 minutes

### 12.2 STS auto refresh

For STS configs, auto refresh re-reads the 1Password item and generates fresh temporary credentials.

### 12.3 SSO auto refresh

For SSO configs, auto refresh uses the in-memory cache that has been preloaded from 1Password.

If a valid refresh token is available, it can refresh without a full browser login.

If refresh state is invalid or expired, the next interactive login is required.

## 13. Reliability and Security Requirements

### 13.1 Secret handling

- AWS long-lived secrets are not stored in plaintext local files
- passwords and OTP seeds live in 1Password
- SSO session state is persisted in 1Password, not in local plaintext metadata
- the app must not log credentials, passwords, refresh tokens, or OTP codes

### 13.2 Credentials file writes

`~/.aws/credentials` writes must:

- preserve unrelated profiles
- update the configured profile atomically
- avoid partial corruption

### 13.3 Failure tolerance

The app should remain usable when:

- 1Password is not connected
- AWS calls fail
- browser login is cancelled

Errors must be surfaced in the UI without crashing the app.

## 14. Current UI Flows

### 14.1 New item flow

`New Item` is a wizard:

1. Select 1Password destination
2. Configure settings
3. Save

### 14.2 Import existing flow

`Import Existing` is a wizard:

1. Select account
2. Select vault
3. Select item
4. Review and save

### 14.3 Edit flow

Edit loads the linked 1Password item and opens a standard edit form.

If full 1Password loading fails, the app can fall back to locally cached summary data and preserve existing secret fields when blank.

## 15. Distribution

The current distribution target is a local development build and GitHub Release artifact.

Artifacts:

- `dist/AWS Credential Manager.app`
- `dist/aws-credential-manager-macos.zip`

Current distribution notes:

- unsigned development build
- may require manual `Open` in Finder on another Mac

### 10.3 Discussion of `~/.aws/credentials` vs `~/.aws/config`

For the target behavior, writing the resulting AWS access key ID, secret access key, and session token to `~/.aws/credentials` is sufficient for consumers that only need a named profile with temporary credentials.

Therefore:

- Phase 1 writes only `~/.aws/credentials`
- `~/.aws/config` updates are optional and not required

Future enhancement:

- Optionally write region/output defaults to `~/.aws/config`

### 10.4 SSO session persistence policy

The app intentionally does not persist IAM Identity Center login state across restarts.

Therefore:

- While the app remains running, SSO refresh may use in-memory refresh state
- After restart or crash, the user must run an interactive SSO login again
- This is an intentional security and simplicity tradeoff

## 11. Credential File Handling

### 11.1 File target

- `~/.aws/credentials`

### 11.2 Write behavior

For the configured `profile_name`, the app must upsert:

- `aws_access_key_id`
- `aws_secret_access_key`
- `aws_session_token`

Recommended metadata comment:

- Store expiration as a comment or local metadata, not as an AWS profile property relied on by external tools

### 11.3 Atomicity

Implement:

1. Read existing file
2. Merge or replace target profile
3. Write to temporary file
4. fsync
5. Rename atomically

### 11.4 Permissions

If creating the credentials file, use restrictive permissions equivalent to user-read/write only where practical on macOS.

## 12. UX Specification

### 12.1 Main menu bar popover

Top area:

- App name
- 1Password connection status
- Global refresh status

List area:

Each config row shows:

- Setting name
- Profile name
- Auth type
- Remaining time
- `Generate`
- `Edit`

Footer area:

- `Add STS Config`
- `Add SSO Config`
- `Open Settings`
- `Quit`

### 12.2 Add/Edit config screen

Shared fields:

- Setting name
- Profile name
- Auth type selector
- Auto-refresh toggle

STS fields:

- AWS access key ID
- AWS secret access key
- MFA ARN
- MFA TOTP
- Role ARN
- Advanced fields

SSO fields:

- SSO start URL
- SSO region
- Username
- Password
- MFA TOTP
- AWS account ID
- AWS role name
- Advanced fields

Buttons:

- `Save to 1Password`
- `Cancel`
- `Test Connection`

### 12.3 Status labels

Possible states:

- `Ready`
- `Expires in <duration>`
- `Expired`
- `Refreshing`
- `1Password Authorization Required`
- `Browser Login Required`
- `Error`

### 12.4 First-run UX

If no local metadata exists:

- Show onboarding sheet
- Explain 1Password desktop integration prerequisite
- Verify the `Private` vault exists
- Allow creating the first STS or SSO config

## 13. Error Handling

### 13.1 1Password errors

Examples:

- Desktop integration disabled
- 1Password app not running
- Authorization expired
- Item not found
- Private vault unavailable

Behavior:

- Show actionable message in UI
- Keep app alive
- Allow retry

### 13.2 AWS errors

Examples:

- Invalid MFA
- Invalid access key
- STS access denied
- SSO browser login timed out
- SSO account/role not assigned
- Refresh token expired

Behavior:

- Show compact error in row
- Expose detailed error in edit/detail panel
- Do not modify credentials file on failed refresh

### 13.3 File errors

Examples:

- `~/.aws` not writable
- Permission denied
- Invalid credentials file format

Behavior:

- Preserve original file
- Surface clear remediation

## 14. Scheduling and Auto-Refresh Design

### 14.1 Scheduler behavior

The Go core owns the scheduler.

It must:

- Track expiration per config
- Wake periodically, recommended every 60 seconds
- Trigger refresh when threshold is reached
- Serialize per-config refresh attempts
- Avoid duplicate overlapping refreshes

### 14.2 Auto-refresh policy

Default:

- Disabled

When enabled:

- Start refresh when `remaining_time <= 10 minutes`
- Back off after repeated errors

Recommended backoff:

- 1st failure: retry in 1 minute
- 2nd failure: retry in 3 minutes
- 3rd failure and later: retry in 5 minutes until expiration or manual intervention

### 14.3 Interaction with 1Password authorization lifetime

Because desktop app authorization is human-in-the-loop and time-bounded, auto-refresh may sometimes require re-authorization in 1Password.

UI behavior:

- Surface `1Password Authorization Required`
- Retry after the user authorizes

SSO-specific behavior:

- If the app process restarts or crashes, surface `Browser Login Required` for SSO configs until interactive login is completed again

## 15. Security Model

### 15.1 Secret storage rules

- Long-lived AWS credentials live only in 1Password
- SSO username/password/TOTP live only in 1Password
- Temporary AWS credentials live in `~/.aws/credentials` because that is the required product output
- SSO refresh token may live in memory only while the app process is alive

### 15.2 Logging rules

Never log:

- Access keys
- Secret keys
- Session tokens
- Passwords
- OTP values
- Authorization codes
- Refresh tokens

Allowed in logs:

- Setting name
- Profile name
- Auth mode
- Expiration timestamp
- Error category

### 15.3 Threat model summary

Primary risks:

- Local process compromise on the user machine
- Leakage via logs
- Corruption of `~/.aws/credentials`
- Broken automation due to brittle browser flows

Primary mitigations:

- 1Password as secret source
- No persistent local cache for SSO refresh state
- No plaintext local secret cache
- Official AWS APIs instead of UI scraping
- Atomic file writes

## 16. Implementation Plan

### Phase 1

- Create native macOS menu bar shell
- Implement local metadata index
- Implement 1Password item CRUD in Go
- Implement STS config CRUD
- Implement `GetSessionToken`
- Implement `AssumeRole`
- Implement `~/.aws/credentials` upsert
- Implement row status and remaining time

### Phase 2

- Implement SSO config CRUD
- Implement OIDC device authorization
- Implement `GetRoleCredentials`
- Implement in-memory SSO token cache
- Implement manual SSO refresh flow

### Phase 3

- Implement auto-refresh
- Add richer error surfaces
- Add onboarding and diagnostics
- Improve packaging and release scripts

## 17. Testing Strategy

### 17.1 Unit tests

- 1Password item schema mapping
- STS request construction
- SSO token state machine
- Credentials file merge/upsert behavior
- Expiration and remaining-time calculations

### 17.2 Integration tests

- 1Password SDK against a local development vault
- STS flow against a test AWS account
- SSO flow against a test IAM Identity Center environment
- In-memory SSO refresh lifecycle

### 17.3 UI tests

- First-run onboarding
- Add/edit/delete config
- Error rendering
- Menu bar row status updates

## 18. Suggested Repository Layout

```text
aws-credential-manager/
  SPEC.md
  app-macos/
    Sources/
      App/
      UI/
      IPC/
  core-go/
    cmd/helper/
    internal/onepassword/
    internal/awssts/
    internal/awssso/
    internal/credentialsfile/
    internal/sessioncache/
    internal/scheduler/
    internal/model/
  docs/
  scripts/
```

## 19. Final Design Decisions

The specification adopts these final design decisions:

1. Target macOS only for Phase 1.
2. Build a native menu bar app.
3. Use SwiftUI/AppKit for UI and Go for the credential engine.
4. Use 1Password `Private` vault as the authoritative secret store.
5. Use official AWS APIs for SSO instead of automating the AWS access portal UI.
6. Write generated credentials to `~/.aws/credentials` under the configured profile name.
7. Store only non-secret metadata locally.
8. Keep SSO refresh state in memory only; after restart or crash, require interactive login again.

## 20. Open Items for Later Discussion

These are not blockers for implementation, but may be revisited later:

- Whether to support launch-at-login
- Whether to add delete/disable actions in the menu bar list
- Whether to write optional region defaults into `~/.aws/config`
- Whether to support Windows in a later architecture split
