#!/bin/bash
# ==============================================================================
# SV241 Alpaca Proxy - One-Line Installer
# ==============================================================================
# Usage:
#   curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo bash
# OR:
#   wget -qO- https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo bash
#
# To uninstall:
#   curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo SV241_UNINSTALL=1 bash
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

# --- Uninstall ---
# SV241_UNINSTALL=1 removes everything this script itself installs, then exits - same env-var
# override pattern as SV241_RELEASE_TAG above, for consistency (one invocation style to remember,
# not a second one like `bash -s -- --uninstall`):
#   curl -sSL .../install_linux.sh | sudo SV241_UNINSTALL=1 bash
# Doesn't need architecture detection or a download - skip straight to it before any of that.
if [ -n "${SV241_UNINSTALL:-}" ]; then
    echo "=== SV241 Alpaca Proxy - Linux Uninstaller ==="
    echo ""

    echo "[1/3] Stopping and disabling service..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "$SERVICE_DIR/$SERVICE_NAME.service"
    systemctl daemon-reload

    echo "[2/3] Removing udev rule..."
    rm -f /etc/udev/rules.d/99-sv241-usb.rules
    udevadm control --reload-rules
    udevadm trigger --subsystem-match=usb
    udevadm trigger --subsystem-match=tty

    echo "[3/3] Removing binary..."
    rm -f "$INSTALL_DIR/$BINARY_NAME"

    echo ""
    echo "=== Uninstall Complete ==="
    echo ""
    echo "Left untouched on purpose (remove these yourself if you want a completely clean slate):"
    echo "  - dialout group membership - other serial devices on this system may rely on it"
    echo "  - the libusb-1.0-0 package - other software may depend on it"
    echo "  - saved config/logs at ~/.config/SV241AlpacaProxy/ - kept in case you reinstall later"
    exit 0
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
# SV241_RELEASE_TAG overrides which release to install from - for pointing a specific tester at a
# beta/pre-release build (e.g. `sudo SV241_RELEASE_TAG=v0.9.21-beta.1 bash`). GitHub itself never
# treats a pre-release as "latest", so without this override there's no way to reach one at all.
# Unset (the normal case) behaves exactly as before: resolve whatever's currently "latest".
if [ -n "${SV241_RELEASE_TAG:-}" ]; then
    LATEST_TAG="$SV241_RELEASE_TAG"
    echo "Using explicitly requested release: $LATEST_TAG"
else
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
fi

DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/${BINARY_NAME}-linux-${ARCH_TAG}"
echo "Download URL: $DOWNLOAD_URL"

# --- Download Binary ---
echo ""
echo "[1/7] Downloading binary ($ARCH_TAG)..."
TMP_BINARY=$(mktemp)
curl -sSfL "$DOWNLOAD_URL" -o "$TMP_BINARY"
chmod 755 "$TMP_BINARY"

# ------------------------------------------------------------------------------
# Everything below this point through step [4/7] sets up prerequisites the proxy
# needs to actually reach the SV241 over USB - deliberately done *before* the
# systemd service is installed/started (steps [5/7]-[7/7]). Doing it in the other
# order (as an earlier version of this script did) meant the service's very first
# connection attempt could race a udev rule or group membership that had not been
# applied yet, failing with "libusb: bad access" until the proxy's own built-in
# reconnect logic retried a few seconds later. Not harmful (it always recovered on
# its own), but not clean either - verified against real hardware that ordering
# prerequisites first makes even a from-scratch install connect successfully on the
# very first attempt, no retry needed.
# ------------------------------------------------------------------------------

# --- Runtime Dependency: libusb-1.0 ---
# The proxy talks to the SV241's CH340 chip directly over libusb instead of going through the
# kernel's ch341 tty driver (see internal/serial/ch340_linux.go and the udev rule step below for
# why) - that needs the real libusb-1.0.so.0 shared library present on the system at startup, or
# the service will crash-loop immediately. Many systems already have it as some other package's
# dependency, but it's not guaranteed on a minimal/headless image (e.g. Raspberry Pi OS Lite).
echo "[2/7] Ensuring libusb-1.0 runtime library is installed..."
if ldconfig -p 2>/dev/null | grep -q 'libusb-1\.0\.so\.0'; then
    echo "  -> libusb-1.0 already present. OK."
elif command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq libusb-1.0-0
    echo "  -> Installed libusb-1.0-0 via apt."
else
    echo "  -> WARNING: could not find libusb-1.0.so.0 and no 'apt-get' available to install it."
    echo "     Install the 'libusb-1.0-0' (or equivalent) package for your distro manually, or"
    echo "     the service below will fail to start."
fi

# --- Serial Port Permissions ---
echo "[3/7] Ensuring serial port access for user '$ACTUAL_USER'..."
if ! groups "$ACTUAL_USER" | grep -q dialout; then
    usermod -aG dialout "$ACTUAL_USER"
    echo "  -> Added '$ACTUAL_USER' to 'dialout' group."
    echo "  -> NOTE: the service started below picks this up immediately (it's a fresh process)."
    echo "     Any *existing* login shell for '$ACTUAL_USER' still needs a fresh login to see it."
else
    echo "  -> User '$ACTUAL_USER' is already in the 'dialout' group. OK."
fi

# --- Raw USB Access (CH340 direct-libusb driver) ---
# On Linux, the proxy talks to the SV241's CH340 (1a86:7523) chip directly over libusb
# instead of going through the kernel's ch341 tty driver - this avoids resetting the ESP32
# on every connect, which the tty layer can't be told to skip (see internal/serial/ch340_linux.go).
# That means it needs read/write access to the raw /dev/bus/usb/BBB/DDD device node, which the
# 'dialout' group above does not cover on its own (that group is scoped to /dev/ttyUSB* nodes).
echo "[4/7] Installing udev rule for raw USB access to the SV241 (CH340 1a86:7523)..."
UDEV_RULE_FILE="/etc/udev/rules.d/99-sv241-usb.rules"
cat > "$UDEV_RULE_FILE" <<EOF
# SV241 Alpaca Proxy - raw USB access for the CH340 direct-libusb driver (Linux only).
# Installed by install_linux.sh. Safe to remove if the proxy is uninstalled.
SUBSYSTEM=="usb", ATTR{idVendor}=="1a86", ATTR{idProduct}=="7523", MODE="0664", GROUP="dialout"

# Every time the proxy releases the device for the web flasher (or on any reconnect), the CH340
# driver's SetAutoDetach(true) makes the kernel's ch341 driver detach and reattach - which, from
# udev's point of view, looks just like the device freshly appearing again. ModemManager (present
# on many Debian/Raspberry Pi OS images for USB-modem support) auto-probes every new tty device by
# briefly opening it, racing the proxy/browser's own attempt to open the exact same port right
# after a release - causing the web flasher's "Serial port is not ready"/"device has been lost"
# errors. ID_MM_DEVICE_IGNORE tells ModemManager to leave this specific device alone entirely -
# the standard fix for this well-known class of problem, also used by many other USB-serial dev
# boards (Arduino, etc.) for the same reason.
SUBSYSTEM=="tty", ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", ENV{ID_MM_DEVICE_IGNORE}="1"
EOF
udevadm control --reload-rules
udevadm trigger --subsystem-match=usb
udevadm trigger --subsystem-match=tty
echo "  -> Installed $UDEV_RULE_FILE and reloaded udev rules."
echo "  -> NOTE: if the SV241 is already plugged in, unplug and replug it once so the new rule applies."

# --- Stop existing service ---
echo "[5/7] Stopping existing service (if running)..."
systemctl stop "$SERVICE_NAME" 2>/dev/null || true

# --- Install Binary ---
echo "[6/7] Installing binary to $INSTALL_DIR/$BINARY_NAME..."
mv "$TMP_BINARY" "$INSTALL_DIR/$BINARY_NAME"
chmod 755 "$INSTALL_DIR/$BINARY_NAME"

# --- Install systemd Service (inline) ---
# All of the prerequisites above (libusb, dialout group, udev rule) are already in place by the
# time this starts the service, so even a from-scratch install's very first connection attempt
# should succeed immediately - no dependency on the proxy's own reconnect/retry logic to recover.
echo "[7/7] Installing systemd service..."
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
