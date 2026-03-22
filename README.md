# AWS Credential Manager

AWS Credential Manager is a macOS menu bar app for managing temporary AWS credentials with 1Password.

It supports two credential flows:

- `STS`
  - Uses long-lived AWS credentials and MFA to generate temporary credentials.
- `SSO`
  - Uses AWS IAM Identity Center and stores reusable session state in 1Password.

Generated credentials are written to `~/.aws/credentials` under the profile name you choose.

## What This App Does

- Runs as a menu bar app on macOS
- Stores configuration and secrets in 1Password
- Generates temporary AWS credentials and writes them to `~/.aws/credentials`
- Supports `GetSessionToken` and `AssumeRole`
- Supports AWS IAM Identity Center based login
- Can auto-refresh credentials before expiration

## Requirements

- macOS
- [1Password desktop app](https://1password.com/downloads/mac/)
- `Integrate with other apps` enabled in 1Password
- Access to an AWS account using either:
  - AWS access key + MFA
  - AWS IAM Identity Center

## Install

Download the latest `aws-credential-manager-macos.zip` from the GitHub Releases page, then unzip it and launch `AWS Credential Manager.app`.

If macOS blocks the app because it is an unsigned development build, open it from Finder with `Control + click` -> `Open`.

## First-time Setup

### 1. Start 1Password

Open the 1Password desktop app and make sure:

- you are signed in to the account you want to use
- `Settings` -> `Developer` -> `Integrate with other apps` is enabled

### 2. Open AWS Credential Manager

Launch the app and click the key icon in the macOS menu bar.

### 3. Register 1Password accounts

Open `1Password Accounts` and add the 1Password account names you want this app to use.

Examples:

- `AKIO KATAYAMA`
- `soracom`

After saving, those accounts become selectable when creating or editing configs.

## Create a Config

Click `Add Config`.

You can choose:

- `New Item`
  - Create a new managed item in 1Password
- `Import Existing`
  - Import an existing 1Password item and save it as a managed config

## STS Config

Use `STS` when you want to generate temporary credentials from AWS access keys and MFA.

Fields:

- `Setting Name`
- `Profile Name`
- `Auto Refresh`
- `AWS Access Key ID`
- `AWS Secret Access Key`
- `MFA ARN`
- `MFA TOTP URI or Code`
- `Role ARN`
- `Role Session Name`
- `External ID`
- `Session Duration Minutes`
- `STS Region`

Behavior:

- If `Role ARN` is blank, the app uses `GetSessionToken`
- If `Role ARN` is set, the app uses `AssumeRole`

## SSO Config

Use `SSO` when you want to generate credentials from AWS IAM Identity Center.

Fields:

- `Setting Name`
- `Profile Name`
- `Auto Refresh`
- `SSO Start URL`
- `SSO Region`
- `Username`
- `Password`
- `MFA TOTP URI or Code`
- `AWS Account ID`
- `AWS Role Name`
- `Session Duration Minutes`

Notes:

- `SSO Start URL` should be the URL shown by AWS IAM Identity Center, for example `https://<tenant>.awsapps.com/start`
- The app opens a browser for sign-in when needed
- The app stores reusable SSO session state in 1Password and loads it into memory at startup and when generating credentials

## Generate Credentials

Click `Generate` on a config.

The app will:

1. Read the config from 1Password
2. Read MFA or session state from 1Password
3. Generate temporary AWS credentials
4. Update `~/.aws/credentials`

The generated profile name is the value in `Profile Name`.

## Auto Refresh

If `Auto Refresh` is set to `On`, the app checks configs periodically and refreshes credentials before they expire.

For `SSO` configs:

- refresh token and related session state are stored in 1Password
- the app loads them into memory on startup
- the app updates the stored session state after successful SSO generation

This means SSO auto refresh can continue after app restart without forcing a full browser login every time, as long as the saved session state is still valid.

## 1Password Storage

For managed items created by this app:

- item title format is `[aws-credential-manager] <Setting Name>`
- the selected 1Password account and vault are used
- secrets are stored in 1Password, not in local metadata

The local app metadata only keeps non-secret summary information used for the UI and scheduling.

## Import Existing

`Import Existing` uses a step-by-step flow:

1. Select 1Password account
2. Select vault
3. Select item
4. Review and save

You can search by partial match in both vault and item lists.

## Config List

The config list shows:

- setting name
- auth type
- profile name
- credential expiration
- for SSO:
  - whether a refresh token is loaded
  - current SSO session expiration if available

## Troubleshooting

### 1Password does not connect

Check:

- 1Password desktop app is running
- `Integrate with other apps` is enabled
- the correct 1Password account is selected

If needed, use `Connect 1Password` again.

### Generate is waiting during SSO

The app may be waiting for browser sign-in. If you want to stop that flow, press `Cancel`.

### Credentials are not written

Check:

- the target `Profile Name`
- file permissions for `~/.aws/credentials`
- whether the AWS or SSO settings are valid

## Current Limitations

- macOS only
- unsigned development build by default
- depends on the 1Password desktop app integration

## For Developers

Main directories:

- [app-macos](/Users/yaman/git/aws-credential-manager/app-macos)
- [core-go](/Users/yaman/git/aws-credential-manager/core-go)
- [scripts](/Users/yaman/git/aws-credential-manager/scripts)

Build a distributable app:

```bash
./scripts/build-distributable.sh
```

Output:

- `dist/AWS Credential Manager.app`
- `dist/aws-credential-manager-macos.zip`
