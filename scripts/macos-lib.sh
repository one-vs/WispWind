# Shared macOS helpers (code signing, app data location); source this file, don't execute it.

# Where the app keeps .env, logs, history and usage when run from a .app bundle
# (see storage.AppDir). Data must never live inside the bundle: it breaks the seal.
DATA_DIR="$HOME/Library/Application Support/WispWind"

# write_app_bundle <app bundle> <binary> [version]: assemble WispWind.app
# (executable, icon, Info.plist). Signing is left to the caller.
write_app_bundle() {
    local app="$1" bin="$2" version="${3:-1.0}"
    local contents="$app/Contents"
    local system_icon="/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/GenericApplicationIcon.icns"
    mkdir -p "$contents/MacOS" "$contents/Resources"
    cp "$bin" "$contents/MacOS/WispWind"
    chmod +x "$contents/MacOS/WispWind"
    if [ -f "$system_icon" ]; then
        cp "$system_icon" "$contents/Resources/AppIcon.icns"
    fi
    cat > "$contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>WispWind</string>
  <key>CFBundleDisplayName</key>
  <string>WispWind</string>
  <key>CFBundleIdentifier</key>
  <string>$BUNDLE_ID</string>
  <key>CFBundleVersion</key>
  <string>$version</string>
  <key>CFBundleShortVersionString</key>
  <string>$version</string>
  <key>CFBundleExecutable</key>
  <string>WispWind</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSMicrophoneUsageDescription</key>
  <string>WispWind needs microphone access to transcribe your voice.</string>
  <key>NSInputMonitoringUsageDescription</key>
  <string>WispWind needs input monitoring access to detect the global dictation hotkey.</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
EOF
}

# migrate_bundle_data <app bundle>: move data left in Contents/MacOS by older versions.
migrate_bundle_data() {
    local macos="$1/Contents/MacOS"
    mkdir -p "$DATA_DIR"
    for item in .env logs history usage; do
        if [ -e "$macos/$item" ]; then
            if [ -e "$DATA_DIR/$item" ]; then
                local backup="$DATA_DIR/$item.old.$(date +%s)"
                echo "Keeping existing $DATA_DIR/$item; bundle copy moved to $backup"
                mv "$macos/$item" "$backup"
            else
                mv "$macos/$item" "$DATA_DIR/$item"
            fi
        fi
    done
    rm -f "$macos/WispWind-bin"
}
#
# A stable certificate identity keeps macOS privacy grants (Accessibility,
# Input Monitoring) valid across rebuilds; an ad-hoc signature changes with
# every build and makes macOS forget them.
# Override with CODESIGN_IDENTITY="<name or SHA-1>", or CODESIGN_IDENTITY=none to skip.

BUNDLE_ID="com.wispwind.desktop"
SIGN_IDENTITY="${CODESIGN_IDENTITY:-}"
if [ -z "$SIGN_IDENTITY" ]; then
    SIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null \
        | grep -E '"(Developer ID Application|Apple Development)' \
        | head -1 | awk '{print $2}')
fi

# sign_code <binary or .app bundle>
sign_code() {
    local target="$1"
    if [ -z "$SIGN_IDENTITY" ] || [ "$SIGN_IDENTITY" = "none" ]; then
        echo "Skipping code signing (no identity); privacy permissions will reset after each rebuild."
        return 0
    fi
    echo "Signing $target with identity $SIGN_IDENTITY..."
    codesign --force --timestamp=none --identifier "$BUNDLE_ID" --sign "$SIGN_IDENTITY" "$target"
}
