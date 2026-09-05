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

## 📚 Documentation

**Getting Started**
- 🚀 [Quick Start Guide (Windows)](./docs/QUICK_START.md)
- 🐧 [Linux / Raspberry Pi Install](./docs/PINS_INSTALL.md)

**Using the Proxy**
- 🔌 [SV241 Alpaca Proxy Overview](./docs/ASCOM_PROXY.md)
- 🖥️ [Web Interface Guide](./docs/WEB_INTERFACE.md)
- 📊 [Telemetry & Data Explorer](./docs/TELEMETRY.md)
- 🛰️ [Driver Installation](./docs/DRIVER_INSTALLATION.md)

**When Something Goes Wrong**
- 🛠️ [Troubleshooting](./docs/TROUBLESHOOTING.md)

**Reference**
- ⚙️ [REST API & Automation](./docs/REST_API.md)
- 📄 [proxy_config.json Reference](./docs/CONFIGURATION_REFERENCE.md)
- 📡 [Serial API Reference](./docs/SERIAL_API.md)
- 🔧 [Hardware Specs](./docs/HARDWARE.md)

**Contributing**
- 🏗️ [Build From Source](./AscomAlpacaProxy/build_scripts/readme.md)

## Project Overview

This project consists of two main components:
1.  **Custom Firmware:** A replacement firmware for the ESP32-based Svbony SV241 Pro controller. It unlocks advanced control over power outputs and dew heaters.
2.  **SV241 Alpaca Proxy:** A standalone application that runs on your computer. It connects to the controller via USB and exposes its functions as standard ASCOM devices. It should work with any ASCOM Alpaca compatible astronomy software (tested with [NINA](https://nighttime-imaging.eu/), validated with [Conform Universal](https://github.com/ASCOMInitiative/ConformU)). For software without native Alpaca support, the installer includes a helper script to register a classic ASCOM driver (see [Driver Installation](./docs/DRIVER_INSTALLATION.md)). See [docs/ASCOM_PROXY.md](./docs/ASCOM_PROXY.md) for the full proxy documentation.

### Firmware Features
*   Control for 5 DC outputs, 2 USB groups, and 1 adjustable voltage output (0-15V, powered by a [Southchip SC8903](https://datasheet.lcsc.com/lcsc/2107141624_Southchip-Semicon-SC8903QDHR_C252424.pdf) buck-boost converter).
*   Advanced dew heater control:
    *   **Manual Mode:** Variable 0-100% PWM control.
    *   **PID Mode:** Automatic temperature regulation using a lens temperature sensor and configurable target temperature above dew point (see [How PID Mode Works](./docs/WEB_INTERFACE.md#how-pid-mode-works) for how it works and how to tune it).
    *   **Ambient Tracking:** Sensorless power adjustment based on ambient temperature and humidity.
*   On-board sensor suite for monitoring power, ambient temperature/humidity, and lens temperature. The firmware is resilient to sensor failures.
*   Experimental automatic drying cycle for the SHT40 humidity sensor.
*   Configuration persistence across reboots.
*   A powerful JSON-based serial command interface for direct control and integration - see [docs/SERIAL_API.md](./docs/SERIAL_API.md).
