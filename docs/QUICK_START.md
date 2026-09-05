# Quick Start Guide (Windows)

[← Back to main readme](../readme.md)

Follow these steps to get up and running quickly. Running on Linux or a Raspberry Pi
instead? See [PINS_INSTALL.md](./PINS_INSTALL.md).

## 1. Install the ASCOM Alpaca Proxy

1.  Download the latest installer (`SV241-AscomAlpacaProxy-Setup-x.x.exe`) from the [latest release page](https://github.com/DIYAstro/SV241-Unbound/releases/latest).
2.  Run the installer. It's recommended to allow the proxy to start automatically with Windows.
3.  Once running, an icon will appear in your system tray. Right-click it and select **"Open Setup Page"** to access the web interface.

## 2. Flashing the Firmware

> **Note:** The web flasher requires a modern browser with Web Serial API support (**Chrome** or **Edge**).

On first startup, the proxy will display a **First-Run Wizard** that guides you through the firmware installation:

1.  Connect the SV241 controller to your computer via USB.
2.  The wizard will automatically check for compatible firmware.
3.  If no firmware is detected, click **"Flash Firmware"** to open the integrated web flasher.
4.  Select the correct COM port and follow the on-screen instructions.

> **Warning:** Make sure you select the correct COM port! If you have other ESP32 devices connected, their firmware will be overwritten without further confirmation.

**Alternative:** Use the standalone **[SV241-Unbound Web Flasher](https://diyastro.github.io/SV241-Unbound/)** directly.

## 3. Connecting from Astronomy Software

1.  Open your ASCOM-compatible astronomy software (e.g., NINA).
2.  Go to the equipment or hardware section.
3.  When choosing a **Switch** or **ObservingConditions** device, open the ASCOM chooser.
4.  You should see "SV241 Power Switch" and "SV241 Environment" listed under the Alpaca section. Select them.

You can now control the power outputs and read sensor data directly from your main software!
