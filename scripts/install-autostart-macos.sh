#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ] || [ "$ARCH" = "amd64" ]; then
  DEFAULT_BIN="$ROOT_DIR/dist/wispwind-darwin-amd64"
else
  DEFAULT_BIN="$ROOT_DIR/dist/wispwind-darwin-arm64"
fi
BIN_PATH="${1:-$DEFAULT_BIN}"
APP_NAME="WispWind"
LABEL="com.wispwind.app"
BUNDLE_ID="com.wispwind.desktop"
LOG_DIR="$ROOT_DIR/dist/logs"
WORK_DIR="$ROOT_DIR/dist"
APP_DIR="$HOME/Applications/${APP_NAME}.app"
APP_CONTENTS="$APP_DIR/Contents"
APP_MACOS="$APP_CONTENTS/MacOS"
APP_RESOURCES="$APP_CONTENTS/Resources"
APP_EXE="$APP_MACOS/$APP_NAME"
APP_ICON="$APP_RESOURCES/AppIcon.icns"
SYSTEM_ICON="/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/GenericApplicationIcon.icns"
ENV_SOURCE="$ROOT_DIR/dist/.env"

if [ ! -x "$BIN_PATH" ]; then
  echo "Binary not found or not executable: $BIN_PATH"
  echo "Build first: ./scripts/build.sh"
  exit 1
fi

mkdir -p "$LOG_DIR" "$HOME/Applications" "$APP_MACOS" "$APP_RESOURCES"
pkill -f "$APP_MACOS/$APP_NAME" >/dev/null 2>&1 || true
pkill -f "$APP_MACOS/${APP_NAME}-bin" >/dev/null 2>&1 || true
cp "$BIN_PATH" "$APP_EXE"
chmod +x "$APP_EXE"

if [ -f "$ENV_SOURCE" ]; then
  cp "$ENV_SOURCE" "$APP_MACOS/.env"
elif [ -f "$ROOT_DIR/.env" ]; then
  cp "$ROOT_DIR/.env" "$APP_MACOS/.env"
fi

if [ -f "$SYSTEM_ICON" ]; then
  cp "$SYSTEM_ICON" "$APP_ICON"
fi

cat > "$APP_CONTENTS/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>$APP_NAME</string>
  <key>CFBundleDisplayName</key>
  <string>$APP_NAME</string>
  <key>CFBundleIdentifier</key>
  <string>$BUNDLE_ID</string>
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>CFBundleShortVersionString</key>
  <string>1.0</string>
  <key>CFBundleExecutable</key>
  <string>$APP_NAME</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>NSMicrophoneUsageDescription</key>
  <string>WispWind needs microphone access to transcribe your voice.</string>
  <key>NSInputMonitoringUsageDescription</key>
  <string>WispWind needs input monitoring access to detect the global dictation hotkey.</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
EOF

PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
launchctl bootout "gui/$(id -u)" "$PLIST_PATH" >/dev/null 2>&1 || true
rm -f "$PLIST_PATH"

osascript <<EOF
tell application "System Events"
  if exists login item "$APP_NAME" then
    delete login item "$APP_NAME"
  end if
  make login item at end with properties {name:"$APP_NAME", path:"$APP_DIR", hidden:false}
end tell
EOF

tccutil reset Accessibility "$BUNDLE_ID" >/dev/null 2>&1 || true
tccutil reset ListenEvent "$BUNDLE_ID" >/dev/null 2>&1 || true

open "$APP_DIR"
sleep 1
open "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

echo "Autostart installed as Login Item: $APP_NAME"
echo "Application bundle: $APP_DIR"
echo "Accessibility registration was reset for: $BUNDLE_ID"
echo "Enable WispWind in Privacy & Security > Accessibility, then restart it:"
echo "  pkill -f \"$APP_EXE\" || true"
echo "  open \"$APP_DIR\""
