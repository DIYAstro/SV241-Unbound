#!/bin/bash
# ==============================================================================
# SV241 Alpaca Proxy - One-Line Installer
# ==============================================================================
# Usage:
#   curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install.sh | sudo bash
# OR:
#   wget -qO- https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install.sh | sudo bash
# ==============================================================================
set -e

REPO="DIYAstro/SV241-Unbound"
BINARY_NAME="AscomAlpacaProxy"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
SERVICE_NAME="sv241-alpaca-proxy"

echo "=== SV241 Alpaca Proxy - Linux Installer ==="
echo ""

# --- Check for root privileges ---
if [ "$EUID" -ne 0 ]; then
    echo "Error: This script requires root privileges. Please run with sudo."
    exit 1
fi

# Get the actual user (not root when running with sudo)
ACTUAL_USER="${SUDO_USER:-$USER}"

# --- Detect Architecture ---
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
        echo "Supported: x86_64 (amd64), aarch64 (Raspberry Pi arm64)"
        exit 1
        ;;
esac
echo "Detected architecture: $ARCH ($ARCH_TAG)"

# --- Determine Download URL ---
# Fetch the tag of the latest release from GitHub API
echo "Fetching latest release version from GitHub..."
LATEST_TAG=$(curl -sSf "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    echo "Error: Could not determine the latest release version."
    echo "Please check your internet connection and try again."
    exit 1
fi
echo "Latest release: $LATEST_TAG"

DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/${BINARY_NAME}-linux-${ARCH_TAG}"
echo "Download URL: $DOWNLOAD_URL"

# --- Download Binary ---
echo ""
echo "[1/5] Downloading binary ($ARCH_TAG)..."
TMP_BINARY=$(mktemp)
curl -sSfL "$DOWNLOAD_URL" -o "$TMP_BINARY"
chmod 755 "$TMP_BINARY"

# --- Stop existing service ---
echo "[2/5] Stopping existing service (if running)..."
systemctl stop "$SERVICE_NAME" 2>/dev/null || true

# --- Install Binary ---
echo "[3/5] Installing binary to $INSTALL_DIR/$BINARY_NAME..."
mv "$TMP_BINARY" "$INSTALL_DIR/$BINARY_NAME"
chmod 755 "$INSTALL_DIR/$BINARY_NAME"

# --- Install systemd Service (inline) ---
echo "[4/5] Installing systemd service..."
cat > "$SERVICE_DIR/$SERVICE_NAME.service" <<EOF
[Unit]
Description=SV241 ASCOM Alpaca Proxy
Documentation=https://github.com/$REPO
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$BINARY_NAME
Restart=on-failure
RestartSec=5
User=$ACTUAL_USER

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl start "$SERVICE_NAME"

# --- Serial Port Permissions ---
echo "[5/5] Ensuring serial port access for user '$ACTUAL_USER'..."
if ! groups "$ACTUAL_USER" | grep -q dialout; then
    usermod -aG dialout "$ACTUAL_USER"
    echo "  -> Added '$ACTUAL_USER' to 'dialout' group."
    echo "  -> NOTE: Please log out and back in for this to take effect!"
else
    echo "  -> User '$ACTUAL_USER' is already in the 'dialout' group. OK."
fi

# --- Done ---
echo ""
echo "=== Installation Complete (v$LATEST_TAG, $ARCH_TAG) ==="
echo ""
echo "Useful commands:"
echo "  Status:  sudo systemctl status $SERVICE_NAME"
echo "  Logs:    sudo journalctl -u $SERVICE_NAME -f"
echo "  Stop:    sudo systemctl stop $SERVICE_NAME"
echo "  Restart: sudo systemctl restart $SERVICE_NAME"
echo ""
# Show the machine's primary IP address for convenience
IP=$(hostname -I | awk '{print $1}')
echo "Web interface: http://${IP:-<your-ip>}:32241"
