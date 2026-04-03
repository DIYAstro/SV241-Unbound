#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$SCRIPT_DIR/../build/AscomAlpacaProxy"
SERVICE_FILE="$SCRIPT_DIR/sv241-alpaca-proxy.service"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
SERVICE_NAME="sv241-alpaca-proxy"

echo "=== SV241 Alpaca Proxy - Linux Installer ==="
echo ""

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: Binary not found at '$BINARY'."
    echo "Please run build_linux.sh first."
    exit 1
fi

# Check for root privileges
if [ "$EUID" -ne 0 ]; then
    echo "This script requires root privileges. Please run with sudo."
    exit 1
fi

# Get the actual user (not root when running with sudo)
ACTUAL_USER="${SUDO_USER:-$USER}"

echo "[1/5] Stopping existing service (if running)..."
systemctl stop "$SERVICE_NAME" 2>/dev/null || true

echo "[2/5] Installing binary to $INSTALL_DIR..."
cp "$BINARY" "$INSTALL_DIR/AscomAlpacaProxy"
chmod 755 "$INSTALL_DIR/AscomAlpacaProxy"

echo "[3/5] Installing systemd service..."
# Update the User= line in the service file with the actual user
sed "s/User=astro/User=$ACTUAL_USER/" "$SERVICE_FILE" > "$SERVICE_DIR/$SERVICE_NAME.service"
systemctl daemon-reload

echo "[4/5] Enabling and starting service..."
systemctl enable "$SERVICE_NAME"
systemctl start "$SERVICE_NAME"

echo "[5/5] Ensuring serial port access for user '$ACTUAL_USER'..."
if ! groups "$ACTUAL_USER" | grep -q dialout; then
    usermod -aG dialout "$ACTUAL_USER"
    echo "  -> Added '$ACTUAL_USER' to 'dialout' group."
    echo "  -> NOTE: You must log out and back in for this to take effect!"
else
    echo "  -> User '$ACTUAL_USER' is already in the 'dialout' group."
fi

echo ""
echo "=== Installation Complete ==="
echo ""
echo "Service status:  sudo systemctl status $SERVICE_NAME"
echo "View logs:       sudo journalctl -u $SERVICE_NAME -f"
echo "Stop service:    sudo systemctl stop $SERVICE_NAME"
echo "Restart service: sudo systemctl restart $SERVICE_NAME"
echo ""
echo "The web interface is available at: http://0.0.0.0:32241/setup"
echo "(Replace 0.0.0.0 with your device's IP address)"
