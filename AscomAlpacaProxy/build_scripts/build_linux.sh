#!/bin/bash
set -e

# Prerequisites beyond Go/Node itself: internal/serial/ch340_linux.go uses cgo to talk to the
# SV241's CH340 chip directly over libusb (bypasses the kernel's ch341 tty driver, which
# otherwise resets the ESP32 on every connect - see that file for why). That needs a C compiler
# and libusb-1.0's headers, e.g. on Debian/Raspberry Pi OS:
#   sudo apt-get install -y gcc libusb-1.0-0-dev
# Go's cgo is enabled by default whenever a C compiler is available, so no extra env vars are
# needed here (unlike build_linux.ps1, which cross-compiles from Windows and has to set up its
# own toolchain for this - see ensure_linux_crosscompile_toolchain.ps1).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Two levels up: build_scripts/ -> AscomAlpacaProxy/ -> repo root.
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"
PROXY_ROOT="$PROJECT_ROOT/AscomAlpacaProxy"

# Detect the host's own architecture - same mapping install_linux.sh uses - so the output is
# named the same way build_linux.ps1's cross-compiled binaries are (AscomAlpacaProxy-linux-amd64/
# -arm64), not the old unqualified "AscomAlpacaProxy". Matters for anything that consumes this
# output directly by that name, e.g. the release-linux.yml GitHub Action.
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        ARCH_TAG="amd64"
        ;;
    aarch64)
        ARCH_TAG="arm64"
        ;;
    *)
        echo "Error: Unsupported architecture '$ARCH'."
        echo "Supported: x86_64 (amd64), aarch64 (arm64)"
        exit 1
        ;;
esac
OUTPUT_BINARY="AscomAlpacaProxy-linux-$ARCH_TAG"

echo "--- Building SV241 Ascom Alpaca Proxy (Linux, $ARCH_TAG) ---"

# 0. Cleanup previous build
if [ -f "$PROXY_ROOT/build/$OUTPUT_BINARY" ]; then
    echo "[0/6] Cleaning previous build..."
    rm "$PROXY_ROOT/build/$OUTPUT_BINARY"
fi

# 1. Sync versions from release_version.json into versioninfo.json and config_manager.h.
#    Single source of truth for both version numbers - edit release_version.json only, this
#    script keeps every other spot in sync automatically. PowerShell isn't guaranteed to be
#    available on a native Linux host, so this uses the bash/sed equivalent sync_versions.sh
#    rather than calling sync_versions.ps1.
#    Invoked as `bash sync_versions.sh` rather than `./sync_versions.sh` on purpose: git's
#    executable bit is easy to lose on a file that's ever edited from Windows (as this repo's
#    scripts have been), and unlike relying on that bit, this works regardless of it.
echo "[1/6] Syncing versions from release_version.json..."
bash "$SCRIPT_DIR/sync_versions.sh" "$PROJECT_ROOT" "$PROXY_ROOT"

# 2. Build Firmware (PlatformIO) and copy the flashable artifacts into the in-app flasher's asset
#    folder - mirrors build_exe.bat's step 2. Must happen after the version sync above (so the
#    compiled firmware embeds the just-synced FIRMWARE_VERSION) and before the version string
#    gets extracted into version.json below - otherwise that file would claim a firmware version
#    that doesn't match what's actually embedded in firmware.bin. (set -e above means a failed
#    `pio run` or missing source file on the `cp` below already aborts this script - no separate
#    error checks needed here, unlike the .bat/.ps1 equivalents.)
echo "[2/6] Building Firmware..."
if ! command -v pio >/dev/null 2>&1; then
    echo "PlatformIO CLI not found on PATH - installing via pip..."
    pip3 install --user --upgrade platformio
    export PATH="$HOME/.local/bin:$PATH"
fi
(cd "$PROJECT_ROOT" && pio run)

echo "Copying firmware artifacts to in-app flasher..."
FW_BUILD_DIR="$PROJECT_ROOT/.pio/build/Firmware_ESP32"
FLASHER_FW_DIR="$PROXY_ROOT/frontend-vue/public/flasher/firmware"
mkdir -p "$FLASHER_FW_DIR"
for f in bootloader.bin partitions.bin firmware.bin; do
    cp "$FW_BUILD_DIR/$f" "$FLASHER_FW_DIR/$f"
done
# Note: docs/firmware/ (the separate GitHub Pages flasher) is deliberately NOT touched here -
# publishing there is a distinct, manual release step (same as build_exe.bat).

# 3. Build Frontend
echo "[3/6] Building Frontend..."
cd "$PROXY_ROOT/frontend-vue"
npm install
npm run build

# Extract Firmware Version (optional, for flasher page)
CONFIG_H="$PROJECT_ROOT/src/config_manager.h"
VERSION_JSON_DIR="$PROXY_ROOT/frontend-vue/dist/flasher/firmware"
VERSION_JSON="$VERSION_JSON_DIR/version.json"

if [ -f "$CONFIG_H" ]; then
    mkdir -p "$VERSION_JSON_DIR"
    FW_VERSION=$(grep -oP 'FIRMWARE_VERSION\s+"[^"]*"\s*$' "$CONFIG_H" | grep -oP '"[^"]*"' | tr -d '"' || true)
    if [ -n "$FW_VERSION" ]; then
        echo "Found Firmware Version: $FW_VERSION"
        echo "{\"version\": \"$FW_VERSION\"}" > "$VERSION_JSON"
    else
        echo "Warning: Could not parse FIRMWARE_VERSION from config_manager.h"
    fi
else
    echo "Note: config_manager.h not found, skipping firmware version extraction."
fi

# 3. Get Product Version for Go Build
echo "[4/6] Reading ProductVersion from versioninfo.json..."
cd "$PROXY_ROOT"
APP_VERSION=$(grep -oP '"ProductVersion":\s*"\K[^"]+' versioninfo.json)
if [ -z "$APP_VERSION" ]; then
    echo "Error: Could not extract ProductVersion!"
    exit 1
fi
echo "App Version: $APP_VERSION"

# 4. Build Binary
echo "[5/6] Compiling Go executable..."
mkdir -p build
go build -ldflags="-X main.AppVersion=$APP_VERSION" -o "build/$OUTPUT_BINARY" .

echo "--- Build Complete: build/$OUTPUT_BINARY (v$APP_VERSION) ---"
