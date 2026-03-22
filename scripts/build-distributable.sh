#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_PACKAGE_DIR="$ROOT_DIR/app-macos"
DIST_DIR="$ROOT_DIR/dist"
APP_NAME="AWS Credential Manager.app"
APP_DIR="$DIST_DIR/$APP_NAME"
APP_EXECUTABLE_NAME="AwsCredentialManagerApp"
HELPER_NAME="aws-credential-manager-helper"
RESOURCE_BUNDLE_NAME="app-macos_App.bundle"
ZIP_PATH="$DIST_DIR/aws-credential-manager-macos.zip"
VERSION="${VERSION:-0.1.3}"

echo "==> Building Go helper"
"$ROOT_DIR/scripts/build-helper.sh"

echo "==> Building app icon"
ICON_PATH="$("$ROOT_DIR/scripts/build-app-icon.sh")"

echo "==> Building macOS app (release)"
swift build --package-path "$APP_PACKAGE_DIR" --configuration release

APP_EXECUTABLE_PATH="$(find "$APP_PACKAGE_DIR/.build" -path "*/release/$APP_EXECUTABLE_NAME" | head -n 1)"
RESOURCE_BUNDLE_PATH="$(find "$APP_PACKAGE_DIR/.build" -path "*/release/$RESOURCE_BUNDLE_NAME" | head -n 1)"
HELPER_PATH="$ROOT_DIR/core-go/bin/$HELPER_NAME"

if [[ -z "$APP_EXECUTABLE_PATH" || ! -x "$APP_EXECUTABLE_PATH" ]]; then
  echo "Release app executable not found." >&2
  exit 1
fi

if [[ -z "$RESOURCE_BUNDLE_PATH" || ! -d "$RESOURCE_BUNDLE_PATH" ]]; then
  echo "Release resource bundle not found." >&2
  exit 1
fi

if [[ ! -x "$HELPER_PATH" ]]; then
  echo "Helper executable not found." >&2
  exit 1
fi

echo "==> Assembling app bundle"
rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"

cat >"$APP_DIR/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>AWS Credential Manager</string>
  <key>CFBundleExecutable</key>
  <string>$APP_EXECUTABLE_NAME</string>
  <key>CFBundleIdentifier</key>
  <string>com.c9katayama.aws-credential-manager</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleName</key>
  <string>AWS Credential Manager</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$VERSION</string>
  <key>CFBundleVersion</key>
  <string>$VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>13.0</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
EOF

cp "$APP_EXECUTABLE_PATH" "$APP_DIR/Contents/MacOS/$APP_EXECUTABLE_NAME"
cp "$HELPER_PATH" "$APP_DIR/Contents/Resources/$HELPER_NAME"
cp -R "$RESOURCE_BUNDLE_PATH" "$APP_DIR/Contents/Resources/$RESOURCE_BUNDLE_NAME"
cp "$ICON_PATH" "$APP_DIR/Contents/Resources/AppIcon.icns"

chmod +x "$APP_DIR/Contents/MacOS/$APP_EXECUTABLE_NAME"
chmod +x "$APP_DIR/Contents/Resources/$HELPER_NAME"

plutil -lint "$APP_DIR/Contents/Info.plist" >/dev/null

echo "==> Applying ad-hoc bundle signature"
codesign --force --deep --sign - "$APP_DIR"
codesign --verify --deep --strict "$APP_DIR"

echo "==> Creating zip archive"
rm -f "$ZIP_PATH"
ditto -c -k --keepParent "$APP_DIR" "$ZIP_PATH"

echo "Built:"
echo "  App bundle: $APP_DIR"
echo "  Zip archive: $ZIP_PATH"
