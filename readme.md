# SV241-Unbound Telescope Power Controller

This is a replacement firmware for the **Svbony SV241 Pro**.
**DISCLAIMER:** This firmware is provided "as-is" without any warranty. Use it at your own risk. The author is not responsible for any damage to your device.

---

<p align="center">
  <em>This is a hobby project. If you find it useful and would like to support a good cause, consider donating via <strong>betterplace.org</strong>—all donations go directly to <strong>Doctors Without Borders</strong>.</em><br><br>
  <a href="https://www.betterplace.org/de/fundraising-events/55631-diyastro-for-doctors-without-borders">
    <img src="https://img.shields.io/badge/Donate-Doctors%20Without%20Borders-red?style=for-the-badge&logo=heart" alt="Donate to Doctors Without Borders">
  </a>
</p>

---

## Table of Contents

*   [Documentation Map](#documentation-map)
*   [Project Overview](#project-overview)
*   [Quick Start Guide](#quick-start-guide)
    *   [1. Install the ASCOM Alpaca Proxy](#1-install-the-ascom-alpaca-proxy)
    *   [2. Flashing the Firmware](#2-flashing-the-firmware)
    *   [3. Connecting from Astronomy Software](#3-connecting-from-astronomy-software)
*   [The ASCOM Alpaca Proxy](#the-ascom-alpaca-proxy)

## Documentation Map

This readme covers the quick start. Everything else lives in [`docs/`](./docs/):

| Topic | Where |
|---|---|
| Quick start, features, project overview | This page |
| Firmware serial JSON protocol reference | [docs/SERIAL_API.md](./docs/SERIAL_API.md) |
| Hardware specs / GPIO pinout | [docs/HARDWARE.md](./docs/HARDWARE.md) |
| ASCOM Alpaca Proxy overview, security, setup access | [docs/ASCOM_PROXY.md](./docs/ASCOM_PROXY.md) |
| Proxy web interface walkthrough | [docs/WEB_INTERFACE.md](./docs/WEB_INTERFACE.md) |
| Proxy telemetry & Data Explorer | [docs/TELEMETRY.md](./docs/TELEMETRY.md) |
| Registering a classic ASCOM driver | [docs/DRIVER_INSTALLATION.md](./docs/DRIVER_INSTALLATION.md) |
| Proxy REST API & automation | [docs/REST_API.md](./docs/REST_API.md) |
| `proxy_config.json` reference | [docs/CONFIGURATION_REFERENCE.md](./docs/CONFIGURATION_REFERENCE.md) |
| Building the proxy from source | [AscomAlpacaProxy/build_scripts/readme.md](./AscomAlpacaProxy/build_scripts/readme.md) |
| Fixing problems | [docs/TROUBLESHOOTING.md](./docs/TROUBLESHOOTING.md) |
| Linux / Raspberry Pi install | [docs/PINS_INSTALL.md](./docs/PINS_INSTALL.md) |

## Project Overview

This project consists of two main components:
1.  **Custom Firmware:** A replacement firmware for the ESP32-based Svbony SV241 Pro controller. It unlocks advanced control over power outputs and dew heaters.
2.  **ASCOM Alpaca Proxy:** A standalone application that runs on your computer. It connects to the controller via USB and exposes its functions as standard ASCOM devices. It should work with any ASCOM Alpaca compatible astronomy software (tested with [NINA](https://nighttime-imaging.eu/), validated with [Conform Universal](https://github.com/ASCOMInitiative/ConformU)). For software without native Alpaca support, the installer includes a helper script to register a classic ASCOM driver (see [Driver Installation](./docs/DRIVER_INSTALLATION.md#driver-installation)).

### Firmware Features
*   Control for 5 DC outputs, 2 USB groups, and 1 adjustable voltage output (0-15V, powered by a [Southchip SC8903](https://datasheet.lcsc.com/lcsc/2107141624_Southchip-Semicon-SC8903QDHR_C252424.pdf) buck-boost converter).
*   Advanced dew heater control:
    *   **Manual Mode:** Variable 0-100% PWM control.
    *   **PID Mode:** Automatic temperature regulation using a lens temperature sensor and configurable target temperature above dew point.
    *   **Ambient Tracking:** Sensorless power adjustment based on ambient temperature and humidity.

#### PID Mode - How It Works

In **PID Mode**, the controller automatically adjusts heater power to maintain the lens temperature at a safe level above the dew point:

*   **Lens Temp:** The current temperature of the lens, measured by an external sensor.
*   **Target Temp / Minimum Temp:** The desired temperature offset above the ambient dew point. A typical value is 3-5°C.

The controller calculates the dew point from ambient temperature and humidity, adds the target offset, and adjusts heater power to maintain that temperature.

> **Important:** The lens temperature sensor (labeled **TEMP** on the SV241) must be positioned **under or adjacent to the dew heater strap**. This ensures the sensor measures the heated area and provides accurate feedback for the PID controller.
*   On-board sensor suite for monitoring power, ambient temperature/humidity, and lens temperature. The firmware is resilient to sensor failures.
*   Experimental automatic drying cycle for the SHT40 humidity sensor.
*   Configuration persistence across reboots.
*   A powerful JSON-based serial command interface for direct control and integration - see [docs/SERIAL_API.md](./docs/SERIAL_API.md).

## Quick Start Guide

Follow these steps to get up and running quickly.

### 1. Install the ASCOM Alpaca Proxy

1.  Download the latest installer (`SV241-AscomAlpacaProxy-Setup-x.x.exe`) from the [latest release page](https://github.com/DIYAstro/SV241-Unbound/releases/latest).
2.  Run the installer. It's recommended to allow the proxy to start automatically with Windows.
3.  Once running, an icon will appear in your system tray. Right-click it and select **"Open Setup Page"** to access the web interface.

### 2. Flashing the Firmware

> **Note:** The web flasher requires a modern browser with Web Serial API support (**Chrome** or **Edge**).

On first startup, the proxy will display a **First-Run Wizard** that guides you through the firmware installation:

1.  Connect the SV241 controller to your computer via USB.
2.  The wizard will automatically check for compatible firmware.
3.  If no firmware is detected, click **"Flash Firmware"** to open the integrated web flasher.
4.  Select the correct COM port and follow the on-screen instructions.

> **Warning:** Make sure you select the correct COM port! If you have other ESP32 devices connected, their firmware will be overwritten without further confirmation.

**Alternative:** Use the standalone **[SV241-Unbound Web Flasher](https://diyastro.github.io/SV241-Unbound/)** directly.

### 3. Connecting from Astronomy Software

1.  Open your ASCOM-compatible astronomy software (e.g., NINA).
2.  Go to the equipment or hardware section.
3.  When choosing a **Switch** or **ObservingConditions** device, open the ASCOM chooser.
4.  You should see "SV241 Power Switch" and "SV241 Environment" listed under the Alpaca section. Select them.

You can now control the power outputs and read sensor data directly from your main software!

---

## The ASCOM Alpaca Proxy

The proxy application is a crucial part of the system, translating the device's serial commands into standard ASCOM Alpaca APIs. It runs on your computer, connects to the controller via USB, and exposes its functions to astronomy software.


It includes features like auto-detection and custom ASCOM actions.

**Key Features:**
*   **Rich Web Interface:** A modern, responsive dark-themed dashboard for full control and configuration.
*   **Telemetry History:** Built-in interactive charts for analyzing power and environmental data over time.

**For detailed information on its features, configuration, and usage, please see the dedicated documentation:**

[**ASCOM Alpaca Proxy Documentation**](./docs/ASCOM_PROXY.md)
