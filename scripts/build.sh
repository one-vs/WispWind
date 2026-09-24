#!/bin/bash
# Auto-detect architecture to only build the native binary by default, avoiding unnecessary errors/logs.
# Pass --all to build for both architectures.

APP_NAME="wispwind"
OUTPUT_DIR="dist"
BUILD_FLAGS="-ldflags=-s -w"

mkdir -p $OUTPUT_DIR

BUILD_ALL=false
for arg in "$@"; do
    if [ "$arg" = "--all" ] || [ "$arg" = "-all" ]; then
        BUILD_ALL=true
    fi
done

# Code signing: a stable certificate identity keeps macOS privacy grants
# (Accessibility, Input Monitoring) valid across rebuilds; the linker's ad-hoc
# signature changes with every build and makes macOS forget them.
# Override with CODESIGN_IDENTITY="<name or SHA-1>", or CODESIGN_IDENTITY=none to skip.
BUNDLE_ID="com.wispwind.desktop"
SIGN_IDENTITY="${CODESIGN_IDENTITY:-}"
if [ -z "$SIGN_IDENTITY" ]; then
    SIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null \
        | grep -E '"(Developer ID Application|Apple Development)' \
        | head -1 | awk '{print $2}')
fi

sign_binary() {
    local bin="$1"
    if [ -z "$SIGN_IDENTITY" ] || [ "$SIGN_IDENTITY" = "none" ]; then
        echo "Skipping code signing (no identity); privacy permissions will reset after each rebuild."
        return 0
    fi
    echo "Signing $bin with identity $SIGN_IDENTITY..."
    codesign --force --timestamp=none --identifier "$BUNDLE_ID" --sign "$SIGN_IDENTITY" "$bin"
}

build_arm64() {
    echo "Building for macOS (arm64)..."
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-arm64 ./cmd/app &&
        sign_binary $OUTPUT_DIR/$APP_NAME-darwin-arm64
}

build_amd64() {
    echo "Building for macOS (amd64)..."
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-amd64 ./cmd/app &&
        sign_binary $OUTPUT_DIR/$APP_NAME-darwin-amd64
}

if [ "$BUILD_ALL" = true ]; then
    echo "Building for both architectures..."
    build_arm64 || echo "Failed to build for arm64"
    build_amd64 || echo "Failed to build for amd64 (this is expected on Apple Silicon without x86_64 libs)"
else
    ARCH=$(uname -m)
    echo "Detected architecture: $ARCH"
    if [ "$ARCH" = "arm64" ]; then
        build_arm64
    elif [ "$ARCH" = "x86_64" ] || [ "$ARCH" = "amd64" ]; then
        build_amd64
    else
        echo "Unknown architecture: $ARCH. Attempting to build both..."
        build_arm64 || echo "Failed to build for arm64"
        build_amd64 || echo "Failed to build for amd64"
    fi
fi

echo "Done. Binaries are in $OUTPUT_DIR/"
