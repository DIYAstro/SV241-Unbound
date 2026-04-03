#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROXY_ROOT="$PROJECT_ROOT/AscomAlpacaProxy"

echo "--- Building SV241 Ascom Alpaca Proxy (Linux) ---"

# 0. Cleanup previous build
if [ -f "$PROXY_ROOT/build/AscomAlpacaProxy" ]; then
    echo "[0/3] Cleaning previous build..."
    rm "$PROXY_ROOT/build/AscomAlpacaProxy"
fi

# 1. Build Frontend
echo "[1/3] Building Frontend..."
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

# 2. Get Product Version for Go Build
echo "[2/3] Reading ProductVersion from versioninfo.json..."
cd "$PROXY_ROOT"
APP_VERSION=$(grep -oP '"ProductVersion":\s*"\K[^"]+' versioninfo.json)
if [ -z "$APP_VERSION" ]; then
    echo "Error: Could not extract ProductVersion!"
    exit 1
fi
echo "App Version: $APP_VERSION"

# 3. Build Binary
echo "[3/3] Compiling Go executable..."
mkdir -p build
go build -ldflags="-X main.AppVersion=$APP_VERSION" -o build/AscomAlpacaProxy .

echo "--- Build Complete: build/AscomAlpacaProxy (v$APP_VERSION) ---"
