# aws-credential-manager Specification

## 1. Document Status

- Status: Draft for implementation
- Last updated: 2026-03-14
- Target platform: macOS
- UI language: English only

## 2. Goal

Build a macOS menu bar application that manages AWS temporary credentials using 1Password as the primary secret store.

The app must support two credential generation modes:

1. Credential-based AWS STS
2. AWS IAM Identity Center (AWS SSO)-based temporary credential retrieval

For both modes, the app stores the configuration as a 1Password item in the `Private` vault with title:

`[aws-credential-manager] <Setting Name>`

The app shows configured items from the menu bar, lets the user generate or refresh credentials, and writes the resulting credentials to `~/.aws/credentials` under the configured AWS profile name.

## 2.1 External prerequisites

- 1Password desktop app installed on macOS
- 1Password desktop SDK integration enabled
- Access to the `Private` vault
- AWS account permissions for the selected STS flow
- AWS IAM Identity Center enabled for SSO configurations

## 3. Background and Baseline

The existing `credentials-watchdog` application already provides these baseline behaviors:

- Desktop resident app behavior via tray icon
- Local configuration management
- MFA-assisted AWS temporary credential generation
- Writing AWS temporary credentials into `~/.aws/credentials`

The new app keeps those user-visible goals but changes the architecture substantially:

- Use 1Password as the system of record for secret configuration
- Add AWS IAM Identity Center support
- Move to a native macOS menu bar UX
- Use Go for credential logic

## 4. High-Level Product Decision

### 4.1 Recommended implementation

Use a hybrid architecture:

- Native shell: Swift + SwiftUI + AppKit
- Core credential engine: Go

### 4.2 Why this architecture

This is the best fit for the constraints:

- macOS menu bar UX is much better with AppKit/SwiftUI than with Java
- 1Password SDK has first-class support for Go desktop integrations
- AWS STS and IAM Identity Center flows are straightforward in Go
- The app can remain native on macOS while still satisfying the requirement to implement the credential engine in Go

### 4.3 Rejected alternatives

#### Java desktop app

Rejected for Phase 1 because:

- The required 1Password SDK integration is better aligned with Go-based implementation
- Native macOS menu bar behavior, security prompts, and packaging are simpler with Swift/AppKit
- Cross-platform support is not required for the first release

#### Browser scraping of the AWS access portal

Rejected because:

- It is brittle against AWS UI changes
- It depends on DOM automation and login-page details
- Official IAM Identity Center APIs can return the same short-lived AWS credentials more reliably

## 5. Scope

### 5.1 In scope

- macOS menu bar app
- 1Password desktop app integration
- 1Password item create/read/update for app-managed settings
- Credential-based temporary credentials via STS
- IAM Identity Center temporary credentials via official AWS APIs
- Writing credentials to `~/.aws/credentials`
- Manual refresh
- Optional auto-refresh
- Remaining-session display in the menu UI

### 5.2 Out of scope

- Windows support
- App Store distribution
- Enterprise device management
- Browser DOM automation for AWS portal pages
- Editing `~/.aws/config` as a required path
- Multi-user/shared secret storage beyond the signed-in local macOS user

## 6. Functional Requirements

### 6.1 App startup

On launch, the app must:

1. Start as a single-instance macOS menu bar app
2. Initialize local metadata storage
3. Initialize the Go core service
4. Attempt a lightweight connection to the 1Password desktop app using local app integration
5. Verify that the `Private` vault is reachable
6. Load locally cached non-secret metadata and render the menu

If 1Password desktop integration is unavailable, the app must stay running and show a clear error state in the menu bar popover.

### 6.2 Menu bar behavior

The app must run primarily from the macOS menu bar.

Clicking the menu bar icon opens a popover or panel that lists configured items.

Each row must display:

- Setting name
- Remaining lifetime of the current AWS temporary credentials
- `Generate` button
- `Edit` button

Recommended additions:

- Auth type badge: `STS` or `SSO`
- Auto-refresh status
- Last refresh time
- Error indicator for expired or failed state

### 6.3 Settings supported by the app

The app must support two configuration types.

#### 6.3.1 Credential-based AWS STS configuration

Required fields:

- Setting name
- Profile name
- AWS credential for AssumeRole access key id
- AWS credential for AssumeRole secret access key
- MFA ARN
- MFA TOTP field

Additional field added by design:

- Role ARN (optional, but required when AssumeRole mode is used)

Recommended advanced fields:

- Role session name
- External ID
- Session duration minutes
- STS region override
- Auto-refresh enabled

Behavior:

- If `Role ARN` is empty, generate temporary credentials with `GetSessionToken`
- If `Role ARN` is present, generate temporary credentials with `AssumeRole`

#### 6.3.2 AWS IAM Identity Center configuration

Required fields from the request:

- Setting name
- Profile name
- SSO start URL
- SSO region
- Username
- Password
- MFA TOTP field

Additional required fields added by design:

- AWS account ID
- AWS role name

Recommended advanced fields:

- Session duration minutes
- Auto-refresh enabled
- Reuse in-memory refresh state while app is running
- Login URL override if different from start URL

Behavior:

- The app must use official IAM Identity Center OIDC and SSO APIs to acquire AWS temporary credentials
- The app must not depend on scraping the AWS access portal UI
- Username/password/TOTP are stored in 1Password because they are still useful for human login via browser autofill during the interactive device authorization flow

### 6.4 1Password item management

For either config type, the app must create one item in the `Private` vault.

Item title format:

`[aws-credential-manager] <Setting Name>`

The app must:

- Create the item if it does not exist
- Update the item if the setting already exists
- Store the item ID locally after creation to avoid vault-wide scans on every startup

### 6.5 Credential generation

When the user clicks `Generate`, the app must:

1. Read the target item from 1Password using its stored item ID
2. Read secrets and settings from that item
3. Retrieve the MFA/TOTP code from the 1Password TOTP field
4. Execute the selected auth flow
5. Write the resulting credentials to `~/.aws/credentials` under the configured profile name
6. Update the in-memory and locally cached expiration metadata
7. Refresh the menu UI

### 6.6 Auto-refresh

Manual refresh is required.

Auto-refresh is optional and disabled by default.

When enabled, the app should refresh credentials before expiry using a configurable threshold. Recommended default:

- Refresh when remaining lifetime is less than 10 minutes

Auto-refresh expectations by mode:

- Credential-based STS: supported by re-reading secrets and TOTP from 1Password; the user may need to re-authorize 1Password access if the authorization window has expired
- SSO-based: supported only while the app process remains alive and can retain IAM Identity Center refresh state in memory

After app restart, crash, or explicit sign-out, SSO auto-refresh must be unavailable until the user completes a fresh interactive login.

## 7. Non-Functional Requirements

### 7.1 Security

- No AWS long-lived secrets may be stored in plaintext local files
- 1Password is the source of truth for stored credentials and user login secrets
- Local metadata file may contain only non-secret data and 1Password IDs
- SSO access and refresh tokens must exist in memory only and must never be persisted locally
- The app must never log access keys, secret keys, session tokens, passwords, or OTP values

### 7.2 Reliability

- `~/.aws/credentials` writes must be atomic
- Existing unrelated profiles in `~/.aws/credentials` must be preserved
- Partial failures must not corrupt the credentials file
- The menu bar app must survive transient 1Password or AWS failures

### 7.3 Performance

- Startup should not scan all 1Password items if local metadata contains item IDs
- Item resolution should be O(number of locally indexed configs), not O(vault size)
- Refresh operations should run asynchronously and not block the UI thread

### 7.4 UX

- English-only UI
- Menu-driven primary UX
- Clear status and last error display
- Minimal required prompts

## 8. Detailed Architecture

## 8.1 Process model

Use two cooperating parts:

1. macOS app shell
2. Go core service

Recommended packaging for Phase 1:

- Swift app bundle
- Embedded Go helper binary started by the app bundle
- Local IPC over Unix domain socket or XPC-like request bridge

Reason:

- Keeps the menu bar shell native
- Allows the credential engine to remain pure Go
- Is simpler than binding large Go packages directly into Swift for the first release

Important implication:

- 1Password authorizes access on a per-process basis, so the embedded helper binary name must be stable and clearly attributable to this app in the authorization prompt

### 8.2 Module breakdown

#### Swift shell

Responsibilities:

- NSStatusItem / menu bar icon
- Popover and settings windows
- User interaction
- App lifecycle
- Triggering actions in Go core
- Reading non-secret local metadata for fast startup

#### Go core

Responsibilities:

- 1Password SDK integration
- AWS STS integration
- AWS IAM Identity Center integration
- Credential file mutation
- Expiration computation
- Auto-refresh scheduler
- Structured logging

### 8.3 Local storage

#### Non-secret metadata store

Location:

`~/Library/Application Support/aws-credential-manager/index.json`

Contains:

- Local config ID
- Setting name
- Auth type
- Profile name
- 1Password vault ID
- 1Password item ID
- Auto-refresh enabled
- Last known expiration
- Last refresh time
- Last error summary

This file must not contain:

- Access key ID
- Secret access key
- Session token
- Password
- OTP seed
- IAM Identity Center access token
- IAM Identity Center refresh token

#### Secure local store

No persistent secure token store is used in Phase 1.

Rules:

- IAM Identity Center access tokens may exist in memory only for the lifetime of the process
- IAM Identity Center refresh tokens may exist in memory only for the lifetime of the process
- On process termination, restart, or crash, the app discards all cached SSO token state

## 9. 1Password Data Model

### 9.1 Vault

- Fixed vault name: `Private`

### 9.2 Item title

- `[aws-credential-manager] <Setting Name>`

### 9.3 Item category

Recommended category:

- `Login`

Reason:

- Works well for SSO username/password/TOTP use cases
- Supports concealed and TOTP fields cleanly
- Still allows custom fields for AWS-specific values

### 9.4 Item fields

Use a consistent schema with custom fields.

#### Common metadata fields

- `setting_name` (text)
- `profile_name` (text)
- `auth_type` (menu or text: `sts` / `sso`)
- `auto_refresh_enabled` (text or menu)
- `created_by` (text, fixed value: `aws-credential-manager`)
- `schema_version` (text)

#### STS fields

- `aws_access_key_id` (concealed)
- `aws_secret_access_key` (concealed)
- `mfa_arn` (text)
- `mfa_totp` (totp)
- `role_arn` (text, optional)
- `role_session_name` (text, optional)
- `external_id` (concealed, optional)
- `session_duration_minutes` (text, optional)
- `sts_region` (text, optional)

#### SSO fields

- `sso_start_url` (url or text)
- `sso_region` (text)
- `sso_username` (text)
- `sso_password` (concealed)
- `sso_mfa_totp` (totp)
- `sso_account_id` (text)
- `sso_role_name` (text)
- `session_duration_minutes` (text, optional)

### 9.5 Local metadata to item mapping

The app must map each local config entry to:

- Vault ID
- Item ID
- Expected schema version

If item lookup by ID fails, the app must fall back to title search for:

`[aws-credential-manager] <Setting Name>`

If both lookup methods fail, the UI must show the config as broken and prompt the user to repair it.

## 10. AWS Credential Flows

### 10.1 STS flow

#### 10.1.1 GetSessionToken path

Conditions:

- Config type is `sts`
- `role_arn` is empty

Steps:

1. Read `aws_access_key_id`, `aws_secret_access_key`, `mfa_arn`, `mfa_totp`
2. Request current OTP code from 1Password
3. Call STS `GetSessionToken`
4. Receive temporary credentials and expiration
5. Write credentials to `~/.aws/credentials` under `profile_name`

#### 10.1.2 AssumeRole path

Conditions:

- Config type is `sts`
- `role_arn` is present

Steps:

1. Read `aws_access_key_id`, `aws_secret_access_key`, `mfa_arn`, `mfa_totp`, `role_arn`
2. Request current OTP code from 1Password
3. Call STS `AssumeRole`
4. Receive temporary credentials and expiration
5. Write credentials to `~/.aws/credentials` under `profile_name`

Notes:

- `external_id` and `role_session_name` are optional inputs
- `session_duration_minutes` is best-effort; AWS role policy limits still apply

### 10.2 SSO flow

#### 10.2.1 Recommended official flow

Use:

- IAM Identity Center OIDC authorization flow
- IAM Identity Center SSO API `GetRoleCredentials`

Do not use:

- Portal HTML scraping
- Simulated clicking of the portal `Access keys` button

#### 10.2.2 SSO generation steps

1. Read `sso_start_url`, `sso_region`, `sso_account_id`, `sso_role_name`
2. Check in-memory state for reusable OIDC client registration and refresh token
3. If a valid in-memory refresh token is available, refresh the access token
4. Otherwise start device authorization
5. Open the system browser to the authorization URL
6. The user completes login in the browser
7. 1Password browser autofill can fill username/password/TOTP if the user has that setup
8. Poll token completion through the OIDC API
9. Call `GetRoleCredentials`
10. Receive AWS access key ID, secret access key, session token, and expiration
11. Write credentials to `~/.aws/credentials` under `profile_name`
12. If auto-refresh is enabled, retain refresh state in memory only until process exit

Required OIDC scope:

- `sso:account:access`

#### 10.2.3 Why this flow

It satisfies the user goal more robustly than portal automation:

- The resulting output is the same kind of temporary AWS credential triple
- It uses stable APIs
- It avoids coupling to AWS portal UI changes
- It enables future auto-refresh

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
