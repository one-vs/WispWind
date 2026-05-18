#!/bin/bash
# set -e removed to allow multi-arch builds to continue even if one fails due to missing local libs

APP_NAME="wispwind"
OUTPUT_DIR="dist"
BUILD_FLAGS="-ldflags=-s -w"

mkdir -p $OUTPUT_DIR

echo "Building for macOS (arm64)..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-arm64 ./cmd/app || echo "Failed to build for arm64"

echo "Building for macOS (amd64)..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build "$BUILD_FLAGS" -o $OUTPUT_DIR/$APP_NAME-darwin-amd64 ./cmd/app || echo "Failed to build for amd64 (this is expected on Apple Silicon without x86_64 libs)"

echo "Done. Binaries are in $OUTPUT_DIR/"
