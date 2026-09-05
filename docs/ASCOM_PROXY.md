# ASCOM Alpaca Proxy Driver

[← Back to main readme](../readme.md)

The project also includes a standalone ASCOM Alpaca proxy driver written in Go. This application connects to the SV241 device via its serial port and exposes it to the ASCOM ecosystem as standard `Switch` and `ObservingConditions` devices.

> **Note:** The proxy is written in Go with cross-platform support in mind, and includes build scripts and an installer for Linux. That said, the maintainer doesn't use Linux day-to-day, so it isn't actively tested there - it should work, but it hasn't seen the same real-world mileage as the Windows build. Pull requests improving Linux support are very welcome; just note that the maintainer won't be able to actively chase down Linux-specific issues, since testing them isn't realistically possible on this end.

## Table of Contents

- [Documentation Map](#documentation-map)
- [Features](#features)
- [Important Security Notice](#important-security-notice)
  - [Manually Creating a Firewall Rule](#manually-creating-a-firewall-rule)
- [Linux Installation](#linux-installation)
- [Accessing the Setup Page](#accessing-the-setup-page)
- [Development & Building](#development--building)

## Documentation Map

| Topic | Where |
|---|---|
| Features, security, setup access | This page |
| Web interface walkthrough (all tabs) | [WEB_INTERFACE.md](./WEB_INTERFACE.md) |
| Telemetry history, Data Explorer, CSV export | [TELEMETRY.md](./TELEMETRY.md) |
| Registering a classic ASCOM driver | [DRIVER_INSTALLATION.md](./DRIVER_INSTALLATION.md) |
| REST endpoints, curl/PowerShell examples | [REST_API.md](./REST_API.md) |
| `proxy_config.json` field reference | [CONFIGURATION_REFERENCE.md](./CONFIGURATION_REFERENCE.md) |
| Building from source, dev server | [AscomAlpacaProxy/build_scripts/readme.md](../AscomAlpacaProxy/build_scripts/readme.md) |
| Fixing problems | [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) |
| Linux / Raspberry Pi install | [PINS_INSTALL.md](./PINS_INSTALL.md) |

## Features

*   Auto-detection of the SV241 serial port.
*   Exposes all power outputs as a single ASCOM `Switch` device.
*   Exposes environmental sensors as an ASCOM `ObservingConditions` device.
*   **Modern Web Interface:** A responsive, dark-themed dashboard with glassmorphism effects.
*   **Telemetry History:** Automatic CSV logging of all sensor data with an interactive historical chart visualization.
*   **Hide Unused Outputs:** Individual power switches and dew heaters can be disabled in the firmware configuration. Disabled outputs are automatically hidden from both the Web UI and the ASCOM device list, keeping your interface clean.
*   Provides a web-based setup page for configuration, including network settings.
*   Manages the connection to the device automatically.
*   Desktop notifications for device connection and disconnection events.
*   **Hardware-Internet Hybrid Sourcing:** Integrated Open-Meteo weather service to supplement or fallback for environmental metrics (Wind, Clouds, etc.) when hardware sensors are missing or initializing.
*   **Multi-Box / Per-Rig Naming:** Switch names, sensor labels, and related preferences are remembered per physical SV241 box (identified by its factory serial), not just per installation - swap boxes between rigs or plug a different box into the same computer and the right names follow automatically.
*   Helper scripts for easy, automated ASCOM driver creation.

## Important Security Notice

> **Warning:** All traffic between the astronomy software (client) and this Alpaca proxy driver is transmitted **unencrypted** over the network (HTTP).
>
> *   This means that **anyone on the same network** can potentially access the driver and control your device.
> *   By default, the proxy now listens only on `127.0.0.1` (localhost) for enhanced security. If you configure it to be accessible over the network, it is strongly recommended to restrict access to the proxy port (default `32241`) using **firewall rules**.
> *   Do not use this driver on unsecured networks (e.g., public Wi-Fi).

### Manually Creating a Firewall Rule

> **Note:** This section only applies if you have configured the proxy to listen on a network address (e.g., `0.0.0.0`). If you are using the default `127.0.0.1` (localhost), no firewall rule is needed.

If you have configured network access and accidentally clicked "Cancel" or denied access when the Windows Defender Firewall prompt appeared, you will need to create a rule manually.

1.  Open **Command Prompt** or **PowerShell** as an **Administrator**.
2.  Copy and paste the following command, then press Enter:

```bash
netsh advfirewall firewall add rule name="SV241 Alpaca Proxy" dir=in action=allow program="%ProgramFiles%\SV241 Ascom Alpaca Proxy\AscomAlpacaProxy.exe" enable=yes
```

This command adds an inbound rule specifically for the `AscomAlpacaProxy.exe` application, allowing it to receive connections from other devices on your network.

> **Note:** The proxy does **not** require administrator privileges to run. Running it as admin may cause permission issues with configuration files. Always run the proxy as a normal user.


## Linux Installation

> [!CAUTION]
> **Experimental Support:** Linux support (amd64 and arm64/Raspberry Pi) works, but isn't tested
> as thoroughly or continuously as the Windows build - see
> [PINS_INSTALL.md](./PINS_INSTALL.md) for the current testing status and details.

The SV241 Alpaca Proxy can be installed on most Linux distributions (Ubuntu, Debian, Raspberry Pi
OS, etc.) using a one-line command:

```bash
curl -sSL https://github.com/DIYAstro/SV241-Unbound/releases/latest/download/install_linux.sh | sudo bash
```

For the full step-by-step walkthrough - what the installer actually does, service management
commands, updating, uninstalling, installing a beta build, and how to update the firmware on a
headless/remote system - see **[PINS_INSTALL.md](./PINS_INSTALL.md)**. It's written with a
Raspberry Pi running PINS in mind, but nothing about the installer itself is PINS-specific; it
applies to any systemd-based Linux distribution.


## Accessing the Setup Page

The primary way to configure the SV241 Alpaca Proxy is through its built-in web interface. There are several ways to access it:

1.  **System Tray Icon (Recommended)**
    *   When the proxy is running, a new icon will appear in your system tray (usually in the bottom-right corner of the screen on Windows).
    *   **Right-click** the icon and select **"Open Setup Page"** from the menu. This will open the correct page in your default web browser.

2.  **Direct Browser URL**
    *   You can also access the page by manually entering the URL into your web browser. By default, the address is:
    *   `http://localhost:32241/setup`

3.  **If the Default Port is Busy**
    *   The proxy is configured to start on port `32241` by default. If another application is already using this port, the proxy will automatically search for the next available port (e.g., `32242`, `32243`, etc.).
    *   If you cannot connect using the default URL, check the `proxy.log` file located in the configuration directory. The log file will contain a line indicating which port the server started on, for example:
        ```
        [INFO] Starting Alpaca API server on port 32242...
        ```
    *   You would then use that port in the URL: `http://localhost:32242/setup`

## Development & Building

Building the proxy from source (frontend, Go backend, Windows installer), the project's
folder layout, and the frontend hot-reload dev server are documented in
[`AscomAlpacaProxy/build_scripts/readme.md`](../AscomAlpacaProxy/build_scripts/readme.md).
