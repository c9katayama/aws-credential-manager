#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$ROOT_DIR/scripts/build-helper.sh"

export AWS_CREDENTIAL_MANAGER_HELPER_PATH="$ROOT_DIR/core-go/bin/aws-credential-manager-helper"

swift run \
  --disable-sandbox \
  --package-path "$ROOT_DIR/app-macos" \
  AwsCredentialManagerApp
