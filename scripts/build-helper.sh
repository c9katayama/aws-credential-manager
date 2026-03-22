#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="$ROOT_DIR/core-go/bin"
mkdir -p "$OUTPUT_DIR"

(
  cd "$ROOT_DIR/core-go"
  go build -o "$OUTPUT_DIR/aws-credential-manager-helper" ./cmd/helper
)
