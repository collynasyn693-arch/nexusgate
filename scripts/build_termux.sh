#!/bin/sh
set -e

# NexusGate Native Termux ARM64 Static Build Script
# Compiles a pure Go static ELF binary optimized for Android Termux userland.

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
OUTPUT_BINARY="${BIN_DIR}/nexusgate"

VERSION="${VERSION:-1.0.0}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
TARGET="termux/arm64"

echo "=========================================================="
echo " Building NexusGate v${VERSION} (${GIT_COMMIT})"
echo " Target Architecture: ${TARGET}"
echo " Timestamp:           ${BUILD_DATE}"
echo "=========================================================="

mkdir -p "${BIN_DIR}"

# Detect execution environment
if [ -n "${TERMUX_VERSION}" ] || [ -d "/data/data/com.termux/files" ]; then
    echo "[✓] Android Termux environment detected"
    BUILD_OS="android"
else
    echo "[!] Non-Termux host detected (cross-compiling for Android ARM64)"
    BUILD_OS="android"
fi

export CGO_ENABLED=0
export GOOS="${BUILD_OS}"
export GOARCH="arm64"

LDFLAGS="-s -w \
  -X nexusgate/pkg/cli.Version=${VERSION} \
  -X nexusgate/pkg/cli.GitCommit=${GIT_COMMIT} \
  -X nexusgate/pkg/cli.BuildDate=${BUILD_DATE} \
  -X nexusgate/pkg/cli.Target=${TARGET}"

cd "${ROOT_DIR}"
echo "[*] Compiling static arm64 ELF binary..."
go build -trimpath -ldflags="${LDFLAGS}" -o "${OUTPUT_BINARY}" ./cmd/nexusgate

if [ -f "${OUTPUT_BINARY}" ]; then
    chmod +x "${OUTPUT_BINARY}"
    SIZE=$(du -h "${OUTPUT_BINARY}" | cut -f1)
    echo "[✓] Build successful: ${OUTPUT_BINARY} (${SIZE})"
else
    echo "[✗] Build failed: binary not created" >&2
    exit 1
fi
