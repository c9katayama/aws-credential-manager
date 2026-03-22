#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$ROOT_DIR/scripts/build-helper.sh" >/dev/null

printf '{"id":"healthcheck","method":"health.check"}\n' | "$ROOT_DIR/core-go/bin/aws-credential-manager-helper" serve
