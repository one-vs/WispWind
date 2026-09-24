# Shared macOS helpers (code signing, app data location); source this file, don't execute it.

# Where the app keeps .env, logs, history and usage when run from a .app bundle
# (see storage.AppDir). Data must never live inside the bundle: it breaks the seal.
DATA_DIR="$HOME/Library/Application Support/WispWind"

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
