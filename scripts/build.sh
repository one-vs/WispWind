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

build_arm64() {
    echo "Building for macOS (arm64)..."
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-arm64 ./cmd/app
}

build_amd64() {
    echo "Building for macOS (amd64)..."
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-amd64 ./cmd/app
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
