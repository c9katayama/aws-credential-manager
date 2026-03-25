#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
ICONSET_DIR="$DIST_DIR/AppIcon.iconset"
SOURCE_PNG="$DIST_DIR/AppIcon-1024.png"
ICNS_PATH="$DIST_DIR/AppIcon.icns"

mkdir -p "$DIST_DIR"
rm -rf "$ICONSET_DIR" "$ICNS_PATH" "$SOURCE_PNG"
mkdir -p "$ICONSET_DIR"

swift "$ROOT_DIR/scripts/render-app-icon.swift" "$SOURCE_PNG"

cp "$SOURCE_PNG" "$ICONSET_DIR/icon_512x512@2x.png"
cp "$SOURCE_PNG" "$ICONSET_DIR/icon_1024x1024.png"

sips -z 16 16 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_16x16.png" >/dev/null
sips -z 32 32 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_16x16@2x.png" >/dev/null
sips -z 32 32 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_32x32.png" >/dev/null
sips -z 64 64 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_32x32@2x.png" >/dev/null
sips -z 128 128 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_128x128.png" >/dev/null
sips -z 256 256 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_128x128@2x.png" >/dev/null
sips -z 256 256 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_256x256.png" >/dev/null
sips -z 512 512 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_256x256@2x.png" >/dev/null
sips -z 512 512 "$SOURCE_PNG" --out "$ICONSET_DIR/icon_512x512.png" >/dev/null

if ! iconutil -c icns "$ICONSET_DIR" -o "$ICNS_PATH"; then
  python3 - "$SOURCE_PNG" "$ICNS_PATH" <<'PY'
from PIL import Image
import sys

source_png, icns_path = sys.argv[1], sys.argv[2]
with Image.open(source_png) as image:
    image.save(icns_path, format="ICNS", sizes=[(16, 16), (32, 32), (64, 64), (128, 128), (256, 256), (512, 512), (1024, 1024)])
PY
fi

echo "$ICNS_PATH"
