#!/usr/bin/env bash
# Build a self-contained WispWind.app for distribution and zip it:
#   dist/release/WispWind-macos-arm64.zip
# PortAudio is linked statically, so the app runs without Homebrew.
# Requires: brew install portaudio pkg-config
# Usage: ./scripts/package-macos.sh [version]   (CI passes the tag, e.g. 0.2.0)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/macos-lib.sh"

VERSION="${1:-1.0}"
ARCH=$(uname -m)
[ "$ARCH" = "x86_64" ] && ARCH="amd64"
OUT_DIR="$ROOT_DIR/dist/release"
APP_DIR="$OUT_DIR/WispWind.app"
ZIP_PATH="$OUT_DIR/WispWind-macos-$ARCH.zip"

PA_PREFIX="$(brew --prefix portaudio)"
if [ ! -f "$PA_PREFIX/lib/libportaudio.a" ]; then
  echo "Static PortAudio not found: $PA_PREFIX/lib/libportaudio.a (brew install portaudio)"
  exit 1
fi

# A private pkg-config file that points at the static archive instead of the
# Homebrew dylib.
PC_DIR="$(mktemp -d)"
trap 'rm -rf "$PC_DIR"' EXIT
cat > "$PC_DIR/portaudio-2.0.pc" <<EOF
Name: PortAudio
Description: Portable audio I/O (static)
Version: 19
Cflags: -I$PA_PREFIX/include
Libs: $PA_PREFIX/lib/libportaudio.a -framework CoreAudio -framework AudioToolbox -framework AudioUnit -framework CoreFoundation -framework CoreServices
EOF

rm -rf "$APP_DIR" "$ZIP_PATH"
mkdir -p "$OUT_DIR"

echo "Building static binary ($ARCH, version $VERSION)..."
BIN="$OUT_DIR/wispwind-darwin-$ARCH"
# -a: the Go build cache doesn't key on pkg-config output, so force a rebuild
# to be sure the static PortAudio is linked.
(cd "$ROOT_DIR" && PKG_CONFIG_LIBDIR="$PC_DIR" CGO_ENABLED=1 \
  go build -a -ldflags "-s -w" -o "$BIN" ./cmd/app)

if otool -L "$BIN" | grep -q "libportaudio"; then
  echo "PortAudio is still linked dynamically:"
  otool -L "$BIN"
  exit 1
fi

write_app_bundle "$APP_DIR" "$BIN" "$VERSION"
rm -f "$BIN"
sign_code "$APP_DIR"
codesign --verify --strict "$APP_DIR"

ditto -c -k --keepParent "$APP_DIR" "$ZIP_PATH"
echo "Packaged: $ZIP_PATH"
