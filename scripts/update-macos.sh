#!/usr/bin/env bash
# Rebuild and swap the binary inside the installed WispWind.app, keeping the
# Login Item and privacy permissions (unlike install-autostart-macos.sh, which
# resets them). Run install-autostart-macos.sh once first.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/macos-lib.sh"

APP_DIR="$HOME/Applications/WispWind.app"
APP_EXE="$APP_DIR/Contents/MacOS/WispWind"
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ] || [ "$ARCH" = "amd64" ]; then
  BIN_PATH="$ROOT_DIR/dist/wispwind-darwin-amd64"
else
  BIN_PATH="$ROOT_DIR/dist/wispwind-darwin-arm64"
fi

if [ ! -d "$APP_DIR" ]; then
  echo "App not installed: $APP_DIR"
  echo "Install first: ./scripts/install-autostart-macos.sh"
  exit 1
fi

(cd "$ROOT_DIR" && ./scripts/build.sh)

pkill -f "$APP_EXE" >/dev/null 2>&1 || true
for _ in $(seq 20); do
  pgrep -f "$APP_EXE" >/dev/null || break
  sleep 0.25
done

migrate_bundle_data "$APP_DIR"
cp "$BIN_PATH" "$APP_EXE"
sign_code "$APP_DIR"
codesign --verify --strict "$APP_DIR"

open "$APP_DIR"
echo "Updated and restarted: $APP_DIR"
