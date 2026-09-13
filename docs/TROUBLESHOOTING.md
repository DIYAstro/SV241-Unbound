# Troubleshooting Guide

[← Back to main readme](../readme.md)

## Table of Contents

- [0. USB Drivers (Critical First Step)](#0-usb-drivers-critical-first-step)
- [Proxy Issues](#proxy-issues)
  - ["Serial port busy" Error](#serial-port-busy-error)
- [ASCOM Client Issues](#ascom-client-issues)
  - [Adjustable Voltage / Number Input Issues (Decimal Separators)](#adjustable-voltage--number-input-issues-decimal-separators)
  - [Adjustable Voltage Switch Timeout](#adjustable-voltage-switch-timeout)
  - [Master Power Switch Timeout](#master-power-switch-timeout)
- [Sensor Issues](#sensor-issues)
  - [Sensors showing 0 or "null" (No Values)](#sensors-showing-0-or-null-no-values)
- [Firmware Issues](#firmware-issues)
  - [Web Flasher not working](#web-flasher-not-working)
  - ["Pro Level" (Recommended): Flashing & Debugging with VS Code](#pro-level-recommended-flashing--debugging-with-vs-code)

## 0. USB Drivers (Critical First Step)

If you encounter connection issues or timeouts, the very first step should always be to ensure you have the latest official drivers installed. Windows sometimes uses generic drivers that can be unstable with high-speed serial communication.

**Solution:**
Download and install the official CH340 drivers from the manufacturer:
[WCH CH341SER (Official Driver)](https://www.wch-ic.com/downloads/ch341ser_zip.html)

> [!NOTE]
> Always restart your computer after installing the driver to ensure it is properly loaded by the system.

---

## Proxy Issues

### "Serial port busy" Error

**Symptoms:**
- Auto-detection fails with "Serial port busy"
- Log shows: `Could not open port COMX to probe: Serial port busy`

**Solutions:**

1. **Reboot your computer**  
   This ensures all port handles are properly released.

2. **Don't run as Admin**  
   The proxy doesn't require admin rights. Running as admin may cause permission issues with config files. If you did run as admin, try:
   - Delete the config folder at `%APPDATA%\SV241AlpacaProxy`
   - Start fresh as a normal user

3. **Check for multiple proxy instances**  
   Open Task Manager → search for "AscomAlpacaProxy" → end all instances → start fresh.

4. **Manually configure the port**  
   If auto-detect keeps failing:
   - Open Setup Page → **System** tab
   - Enter your port (e.g., `COM3` - check Device Manager for the correct one)
   - **Disable** "Auto-Detect Port"
   - Click **Save**

5. **Check for other software**  
   Make sure no other apps are using the port (serial monitors, other astronomy software, etc.)

6. **Edit config file directly**  
   If the Setup page is not accessible, you can [manually configure the port](./CONFIGURATION_REFERENCE.md#manual-configuration-proxy_configjson):
   1. Close the proxy completely
   2. Navigate to `%APPDATA%\SV241AlpacaProxy`
   3. Edit `proxy_config.json` and set:
      ```json
      "serialPortName": "COM3",
      "autoDetectPort": false
      ```
      (Replace `COM3` with your actual port from Device Manager)
   4. Save and restart the proxy

---

## ASCOM Client Issues

### Adjustable Voltage / Number Input Issues (Decimal Separators)
Some ASCOM clients (including test tools and custom scripts) may strip decimal separators or behave unexpectedly when sending floating-point values.

*   **Symptom:** You input `0,5` V or `12,8` V but the device sets `5` V or `128` V.
*   **Cause:** The client software filters out the decimal separator (comma or dot) before sending the command to the proxy.
*   **Verification:** Set the proxy Log Level to `DEBUG`. Check the log for a line like: `[DEBUG] SetSwitchValue (AdjConv) - Received: '5', Normalized: '5'`.
*   **Solution:** This is a client-side formatting issue. Check your client's region settings or input validation rules. The proxy natively supports both `.` (dot) and `,` (comma) separators, provided the client actually sends them.

### Adjustable Voltage Switch Timeout
When trying to switch ON the Adjustable Voltage port (e.g., from NINA), the operation times out or fails.

*   **Symptom:** You toggle the switch to ON, but it fails after a few seconds or reverts to OFF.
*   **Cause:** If `EnableAlpacaVoltageControl` is disabled (default), the proxy acts as a simple switch. If the port was previously set to **0V**, switching it "ON" keeps it at 0V. The ASCOM client expects to see the port turn "ON" (Voltage > 1V), but since it stays at 0V, the client reports a timeout.
*   **Solution:** Configure a startup voltage > 0V for the Adjustable Output (via the Web Interface), or enable `EnableAlpacaVoltageControl` to set the voltage directly via ASCOM.

### Master Power Switch Timeout
If using the "Master Power" switch to turn on all devices, NINA may report a timeout if a heater is configured to 0%.

*   **Symptom:** You turn on "Master Power", but it switches back off after a few seconds, or NINA shows an error.
*   **Cause:** The Master Switch relies on the "Manual Power" configuration of the heaters. If a heater is configured to **0%** (Off), the Master Switch will turn it "On" to 0% power. Since the power output remains at 0, NINA (expecting a value > 0 for "On") thinks the command failed.
*   **Solution:** Ensure that you have configured a valid power level (e.g., 10%) in the `Manual Power` settings *before* using the Master Switch. The Master Switch simply restores this configured value.

### Dew Heater Weaker Than Configured

*   **Symptom:** A heater's actual output (visible in Live Telemetry or via ASCOM) is lower than what you set, in any mode, and a small red dot appears next to the PWM value in the Live Telemetry panel.
*   **Cause:** The **Power Protection** current limit (Dew Heaters Tab) is enabled and the total measured input current is near the configured limit - the heater is being deliberately throttled to stay under it, not malfunctioning.
*   **Solution:** This is expected behavior, not a fault. If it happens more often than you'd like, either raise the current limit (if your power supply can actually handle it) or reduce the configured power/duty of one of the heaters. Check the Telemetry Explorer's "Current Limit Active" value to see how often and when this has been happening.

---

## Sensor Issues

### Sensors showing 0 or "null" (No Values)

**Symptoms:**
- Ambient Temperature, Humidity, Voltage, and Current all show `0.0` or are empty in the Web UI.
- **Wait!** The Lens Temperature (DS18B20) might still show a correct value.
- If you check the Debug Log, you see many `null` values: `{"v":null,"i":null,"p":null,"t_amb":null,...}`.

**Cause:**  
This is almost always an **I2C Bus Error**. Most internal sensors (SHT40 for ambient data, INA219 for power monitoring) share the same communication bus (I2C). If one sensor has a bad connection or is partially unplugged, it can "short" or block the entire bus, causing all other I2C sensors to fail as well.

**Solution:**
1.  The firmware detects repeated I2C read failures on its own and attempts an automatic bus recovery after a few seconds - most transient cases (a brief glitch, a momentarily loose plug) resolve by themselves without you having to do anything. Give it a few seconds before assuming it's stuck.
2.  If it's still showing `0`/`null` after that, check the **physical plug** of the external Temperature/Humidity sensor (SHT40).
3.  Ensure the plug is **fully clicked/seated** into the socket of the SV241 box. Even a half-millimeter gap can cause the I2C bus to hang.
4.  As a last resort, a power cycle (disconnect and reconnect power) always clears it.

> [!NOTE]
> The Lens Temperature sensor (DS18B20) uses a different protocol (1-Wire), which is why it often continues to work even if the I2C bus is blocked.

> [!WARNING]
> **Don't unplug/replug any sensor connector while the box is powered on**, regardless of which one. Depending on the connector, a live disconnect or reconnect can momentarily bridge power against a data or ground line as the pins mate/separate - at best this looks like the I2C error above, at worst it briefly browns out the whole board and resets the ESP32 (you'll see the device drop off and reconnect in the proxy's log). Neither is likely to cause lasting damage, but there's no good reason to risk it - power down first if you need to re-seat a sensor cable.

---

## Firmware Issues

### Web Flasher not working

The proxy's own built-in flasher (`http://<proxy-address>:32241/flasher/`) flashes natively from
the proxy itself - it no longer uses the browser's Web Serial API, so it works in any browser
(including Firefox and Safari) and from any device on your network, not just Chrome/Edge running
on the same machine the box is plugged into. If it fails:

1. **Check the port picker.** If more than one CH340-based USB device is connected, the flasher
   will ask you to pick which one to flash instead of guessing - make sure you picked the right
   one (see the warning box on that page).
2. Disconnect and reconnect the USB cable, then reload the page.
3. Try a different USB port (preferably directly on the computer, not a hub).
4. **Use a shorter USB cable:** flashing can fail with long or low-quality cables.
5. **Try the standalone online flasher instead:** [diyastro.github.io/SV241-Unbound](https://diyastro.github.io/SV241-Unbound/) - a separate, browser-based flasher for when the proxy itself won't start or isn't installed at all. This one *does* need Chrome or Edge (Web Serial API) and a browser running on the same machine the box is plugged into, since it has no proxy backend to talk to.

---

### "Pro Level" (Recommended): Flashing & Debugging with VS Code

If the web flasher fails or shows a "Connect/Disconnect" loop, we recommend using the professional developer method. While it requires a bit more setup, it is **far more robust** and allows you to see the "Internal Logs" of the device, which is essential for determining if a hardware sensor is failing.

#### 1. Setup the Environment
1.  **Download VS Code:** Visit [code.visualstudio.com](https://code.visualstudio.com/) and install Visual Studio Code.
2.  **Add PlatformIO Extension:**
    *   Open VS Code.
    *   Click on the **Extensions** icon on the left (it looks like 4 squares).
    *   Search for **"PlatformIO IDE"** and click **Install**. 
    *   Wait until a small **Ant-Head Icon** appears in your left sidebar.
3.  **Get the Source Code:**
    *   Go to the [GitHub Repository](https://github.com/DIYAstro/SV241-Unbound).
    *   Click the green **Code** button and select **Download ZIP**.
    *   Extract the ZIP file to a folder on your computer (e.g., your Desktop).

#### 2. Open the Project
1.  In VS Code, go to `File` -> `Open Folder...`.
2.  Select the folder you just extracted (ensure the file `platformio.ini` is visible inside that folder).
3.  Click **Select Folder**. PlatformIO will take a minute to download the required ESP32 tools in the background.

#### 3. Flash the Device
1.  **IMPORTANT:** Completely **Close the SV241 Alpaca Proxy** (Right-click the icon in your system tray -> Quit).
2.  Connect your SV241 to your PC via USB.
3.  Look at the **bottom blue status bar** in VS Code. You will see several icons:
    *   `✓` (Build): Compiles the code.
    *   `→` (Upload): Compiles and sends the firmware to the device. **Click this!**
4.  The terminal will open and show the progress. It should end with a green `[SUCCESS]` message.

#### 4. The "Magic" Discovery: Serial Monitor
If you want to know *why* something isn't working (e.g., a heater isn't turning on or a sensor isn't found):
1.  Click the **Plug Icon** (Serial Monitor) in the bottom blue status bar.
2.  A window will open showing the internal messages from the SV241.
3.  If you see things like `[ERROR] SHT40 not found`, you know it's a hardware issue.
4.  The correct baud rate (`115200`) is handled automatically by the project settings.

---

*For additional help, please [open an issue on GitHub](https://github.com/DIYAstro/SV241-Unbound/issues).*
