# ASCOM Alpaca Proxy Driver

The project also includes a standalone ASCOM Alpaca proxy driver written in Go. This application connects to the SV241 device via its serial port and exposes it to the ASCOM ecosystem as standard `Switch` and `ObservingConditions` devices.

> **Note:** While the proxy is written in Go and could theoretically run on other platforms, it is currently only tested and supported on **Windows**. The system tray integration and installer are Windows-specific.

## Table of Contents

- [Features](#features)
- [Important Security Notice](#important-security-notice)
  - [Manually Creating a Firewall Rule](#manually-creating-a-firewall-rule)
- [Accessing the Setup Page](#accessing-the-setup-page)
- [Web Interface Guide](#web-interface-guide)
  - [Live Telemetry Panel](#live-telemetry-panel)
  - [Power Control Panel](#power-control-panel)
  - [Configuration Tabs](#configuration-tabs)
  - [Live Log Panel](#live-log-panel)
- [Telemetry & Logging](#telemetry--logging)
- [Driver Installation](#driver-installation)
  - [Easy Driver Creation (Recommended)](#easy-driver-creation-recommended)
  - [Manual Driver Creation (Fallback)](#manual-driver-creation-fallback)
- [REST API & Automation](#rest-api--automation)
  - [Custom ASCOM Actions](#custom-ascom-actions)
  - [Controlling Individual Switches via REST API](#controlling-individual-switches-via-rest-api)
- [Configuration Reference](#configuration-reference)
  - [Manual Configuration (`proxy_config.json`)](#manual-configuration-proxy_configjson)
  - [Log Level Configuration](#log-level-configuration)
  - [Log Rotation](#log-rotation)
- [Troubleshooting](../TROUBLESHOOTING.md)
- [Advanced: Development & Building](#advanced-development--building)

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
netsh advfirewall firewall add rule name="SV241 Alpaca Proxy" dir=in action=allow program="%ProgramFiles(x86)%\SV241 Ascom Alpaca Proxy\AscomAlpacaProxy.exe" enable=yes
```

This command adds an inbound rule specifically for the `AscomAlpacaProxy.exe` application, allowing it to receive connections from other devices on your network.

> **Note:** The proxy does **not** require administrator privileges to run. Running it as admin may cause permission issues with configuration files. Always run the proxy as a normal user.


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


## Web Interface Guide

This section provides a walkthrough of the web interface, explaining each panel and configuration tab.

### Live Telemetry Panel

The top panel displays real-time sensor readings from the SV241 device:

| Sensor | Description |
|--------|-------------|
| **Voltage** | Input voltage (V) |
| **Current** | Total current draw (A) |
| **Power** | Total power consumption (W) |
| **Amb Temp** | Ambient temperature from SHT40 sensor (°C) |
| **Humidity** | Relative humidity (%) |
| **Dew Point** | Calculated dew point temperature (°C) |
| **Lens Temp** | Objective/lens temperature from DS18B20 sensor (°C) |
| **PWM 1/2** | Current dew heater power levels (%) |

> **Tip:** Click on any sensor value to open an interactive chart showing historical data.

### Power Control Panel

This panel provides quick access to power output control:

*   **Master Power Toggle:** Controls all power outputs simultaneously (if enabled in Proxy settings).
*   **Individual Switches:** Toggle each power output independently. Switches marked as "Disabled" in the firmware configuration are automatically hidden.

### Configuration Tabs

The collapsible "Configuration & Settings" section contains five tabs:

#### Switches Tab
Configure power switch behavior:
*   **State (Startup):** Set each switch to On, Off, or Disabled at device boot.
*   **Custom Name:** Assign user-friendly names that appear in ASCOM clients.
*   **Voltage:** Set the adjustable converter output voltage (5-15V).

> [!IMPORTANT]
> **ASCOM Client Reconnection Required:** When you enable or disable switches, the ASCOM switch IDs change dynamically. Your astronomy software (NINA, SGP, etc.) must **disconnect and reconnect** to the Switch device to see the updated switch list.

#### Dew Heaters Tab
Configure the two PWM dew heater outputs:
*   **Enable on Startup:** Automatically enable the heater when the device boots.
*   **Mode:** Select the control strategy:
    - *Manual:* Fixed power percentage.
    - *PID (Lens Sensor):* Automatic control using the DS18B20 lens temperature sensor.
    - *Ambient Tracking:* Power scales based on proximity to dew point.
    - *PID-Sync (Follower):* Follows another heater's output (useful for dual-heater setups).
    - *Minimum Temperature:* Maintains a minimum lens temperature.
    - *Disabled:* Heater is hidden from UI and ASCOM.

##### Simplified PID Tuning Guide
PID mode automatically regulates the heater to keep your optics dry. If you notice unstable temperatures, use this guide to tune the parameters:

*   **Target Offset:** Desired temperature difference above the Dew Point (Recommended: 2.0°C - 5.0°C).
*   **Kp (Aggressiveness):** Controls how hard the heater pushes.
    *   *Problem:* Temperature swings up and down (Oscillation) -> **Reduce Kp**.
    *   *Problem:* Heating is too slow -> **Increase Kp**.
*   **Ki (Correction):** Corrects small, constant errors.
    *   *Problem:* Temperature stabilizes *below* the target -> **Increase Ki**.
*   **Kd (Damping):** Prevents shooting past the target.
    *   *Problem:* Temperature spikes significantly above target on startup (Overshoot) -> **Increase Kd**.

#### Sensors Tab
Fine-tune sensor readings:
*   **Offsets:** Calibrate temperature, humidity, voltage, and current readings.
*   **Averaging:** Set the number of samples to average (reduces noise).
*   **Intervals:** Configure sensor polling frequency.
*   **SHT40 Auto-Drying:** Enable automatic sensor heater activation at high humidity levels.

#### System Tab
Maintenance and backup functions:
*   **Manual Actions:** Trigger a sensor drying cycle manually.
*   **Backup & Restore:** Export or import the complete configuration (both proxy and firmware settings).
*   **Danger Zone:** Contains critical device operations:
    *   **Update Firmware:** Opens the integrated web flasher to update the SV241 firmware directly from the browser using the Web Serial API—no additional tools required.
    *   **Reboot Device:** Performs a soft restart of the SV241 device.
    *   **Factory Reset:** Erases all saved settings on the device and restores factory defaults.

#### Proxy Tab
Configure the proxy application itself:
*   **Connection:** Serial port settings, auto-detection toggle.
*   **Network:** Listen address, port, and log level.
*   **ASCOM Features:** Enable/disable voltage slider control and the virtual Master Power switch.
*   **Telemetry:** Configure history retention period.

> [!IMPORTANT]
> **ASCOM Client Reconnection Required:** Changes to the ASCOM Features settings (Master Power switch, Voltage Slider mode) also require your astronomy software to **disconnect and reconnect** to see the updated switch configuration.

> **Note:** When disabling Auto-Detect Port, make sure to also specify a serial port name. See [Configuration Reference](#configuration-reference) for details.

### Live Log Panel

The collapsible log viewer shows real-time proxy activity:
*   Color-coded entries (errors in red, warnings in yellow).
*   Auto-scrolls to newest entries (pauses when you scroll up).
*   Download button to save the current log file.


## Telemetry & Logging

The proxy driver includes a robust telemetry system that logs sensor data to a local SQLite database and provides interactive visualization with CSV export.

### Automatic Database Logging
*   **Storage:** Telemetry data is stored in a local SQLite database (`telemetry.db`) in the configuration directory.
*   **Frequency:** Configurable logging interval from 1-10 seconds, or disabled entirely (0 seconds). Default is 10 seconds.
*   **Data Points:** Logs all sensor values including voltage, current, power, temperatures, humidity, dew point, switch states, and heater PWM levels.
*   **Rotation:** Uses a "Noon-to-Noon" rotation strategy. A single imaging night is contained in one session, even if it spans midnight.
*   **Retention:** Old data is automatically pruned based on the configured number of nights to retain (default: 10).

### Data Explorer
The web interface features a built-in **Data Explorer** for interactive telemetry visualization:

*   **Access:** Click the 📊 button in the Live Telemetry panel to open the Data Explorer. This button is only visible when telemetry logging is enabled.
*   **Time Range:** Choose from presets (1h, 12h, 24h, 7d) or select a custom date/time range.
*   **Multi-Sensor Charts:** Select multiple sensors to display on the same chart for comparison.
*   **Interactive Navigation:** Zoom and pan through the data using mouse wheel and drag.
*   **Reset View:** Click "🔄 Reset View" to return to the full time range after zooming.
*   **Custom Names:** Sensors display your custom switch names (e.g., "DC 1 (Telescope Mount)").
*   **Disabled Filtering:** Switches and heaters marked as "Disabled" are automatically hidden from the sensor list.

### CSV Export
Export telemetry data for external analysis:

*   **Download:** Click "Download Selection CSV" in the Data Explorer to export only the selected sensors.
*   **Headers:** CSV headers include custom names in the format `key (custom_name)` for easy identification.
*   **Time Format:** Timestamps are exported in ISO 8601 format (RFC3339).

### Configuration
Telemetry settings are available in the **Proxy Settings** tab under "Logging & Telemetry":

| Setting | Description |
|---------|-------------|
| **Telemetry Interval** | How often to log data (Disabled, 1-10 seconds) |
| **Min. Retention (Nights)** | Minimum number of recorded nights to keep before pruning |


## Driver Installation

To connect your astronomy software to this proxy, a driver must be registered within the ASCOM system. This step is necessary if your software cannot connect to the Alpaca device directly, which can happen for two main reasons:

1.  The software does not have a built-in Alpaca client and relies on the classic ASCOM Chooser.
2.  The software has an Alpaca client, but the automatic network discovery process is not working (e.g., due to firewall settings or network configuration).

Registering a driver solves this by creating a permanent entry in the ASCOM Chooser. This entry acts as a bridge, telling the system exactly how to find and communicate with the proxy.

We provide two methods for this registration: an easy, automated script and a manual method.

### Easy Driver Creation (Recommended)

The installer includes a helper script that automates the entire driver creation process. This is the recommended method as it is fast, easy, and avoids common configuration errors.

1.  **Open the Start Menu**
    *   Navigate to the program folder (usually named `SV241-Unbound`).

2.  **Run the Helper Script**
    *   Click on **"Create SV241 Ascom Driver"**.
    *   This will open a new window and launch an interactive script.

3.  **Follow the On-Screen Instructions**
    *   The script will ask you to select the driver type (`Switch` or `ObservingConditions`). The default is `Switch`.
    *   It will ask you to provide a name for the driver. A default name will be suggested.
    *   It will automatically detect the correct network port from your proxy configuration and suggest it as the default.
    *   Simply press **Enter** at each prompt to accept the defaults, which is sufficient in most cases.

4.  **Done**
    *   After a few moments, the script will confirm that the driver has been successfully created.

**Result:** The driver is now registered system-wide. You can now open your astronomy software and select the driver you just created (e.g., "SV241 Power Switch") directly from the device list.

> **Note:** You can run this script multiple times to create drivers with different names or to create both a `Switch` and an `ObservingConditions` driver.

---

### Manual Driver Creation (Fallback)

If you prefer to set up the driver manually, or if the helper script fails for any reason, you can use the "ASCOM Diagnostics" application that comes with the ASCOM Platform.

1.  **Start ASCOM Diagnostics**

    Open the "ASCOM Diagnostics" application.
    *   You can find it in the Windows Start Menu under the "ASCOM Platform" folder.

2.  **Open the "Switch Chooser"**

    *   In the main window of ASCOM Diagnostics, select the device type `Switch` from the "Select Device Type" dropdown list.
    *   Click the `Choose Device...` button next to it.

3.  **Create a New Alpaca Driver**

    The "ASCOM Switch Chooser" window will open.
    *   In the menu bar of this window, click on `Alpaca`.
    *   Select `Create Alpaca Driver (Admin)` from the dropdown menu.
    *   Windows (UAC) may ask for administrative rights. Please confirm this.

4.  **Name the Driver**

    A small dialog box will ask for a name.
    *   Enter a descriptive name, e.g., `My Manual SV241 Switch`
    *   Click `OK`.

5.  **Configure the Alpaca Connection (Most Important Step)**

    You are now back in the "ASCOM Switch Chooser". Your new driver (e.g., `Switch.My_Manual_SV241_Switch`) is now highlighted in the list.
    *   Click the `Properties...` button.
    *   A setup window will open. Enter the exact connection details here:
        *   **Remote Device Host Name or IP Address:** `localhost`
        *   **Alpaca Port:** `32241` (or the port your proxy is running on)
        *   **Remote Device Number:** `0` (default for the first device)
    *   Click `OK` in the setup window.

6.  **Finalize Selection**

    *   Click `OK` in the "ASCOM Switch Chooser" window as well.

7.  **Test (Optional, but Recommended)**

    *   You are now back in the main window of ASCOM Diagnostics. The name of your new driver is now in the text field.
    *   Click `Connect`.
    *   If everything is configured correctly, the connection should be established successfully (the fields in the "Capabilities" section will be populated).

**Result:** Your manually configured driver "My Manual SV241 Switch" is now permanently registered in the ASCOM system. When you now start NINA (or other software), you can select it directly from the device list without having to enter the IP address again.

> **Note:** Repeat this process for the `ObservingConditions` device to also add the environmental sensors manually.


## REST API & Automation

This section covers advanced usage for power users who want to control the SV241 via command line, scripts, or custom integrations.

### Custom ASCOM Actions

To provide functionality beyond the standard ASCOM `Switch` specification, the driver implements several custom **Actions**. These can be triggered by ASCOM client software that supports them, or manually via API calls.

#### Master Switch Actions (Switch Device)

These actions provide a "Master Switch" to control all power outputs simultaneously.

*   `MasterSwitchOn`: Turns all power outputs on.
*   `MasterSwitchOff`: Turns all power outputs off.

#### Sensor Actions (ObservingConditions Device)

The `ObservingConditions` device provides an action to read the lens/objective temperature separately from the ambient temperature.

*   `getlenstemperature`: Returns the current lens/objective temperature from the DS18B20 sensor (in °C).


#### Using Actions via API (e.g., with `curl`)

You can trigger these actions from the command line using a tool like `curl`. The endpoint for actions is `/api/v1/switch/0/action` and the method is `PUT`.

**Example: Turn all switches off**
```bash
curl -X PUT -d "Action=MasterSwitchOff" http://localhost:32241/api/v1/switch/0/action
```

**Example: Turn all switches on**
```bash
curl -X PUT -d "Action=MasterSwitchOn" http://localhost:32241/api/v1/switch/0/action
```

> **Note for Windows PowerShell users:** The standard `curl` command in PowerShell is an alias for `Invoke-WebRequest`, which has a different syntax and requires the `Content-Type` to be set explicitly. Here are the correct PowerShell commands:
> ```powershell
> # Turn all switches off
> Invoke-WebRequest -Uri http://localhost:32241/api/v1/switch/0/action -Method PUT -Body "Action=MasterSwitchOff" -ContentType "application/x-www-form-urlencoded"
>
> # Turn all switches on
> Invoke-WebRequest -Uri http://localhost:32241/api/v1/switch/0/action -Method PUT -Body "Action=MasterSwitchOn" -ContentType "application/x-www-form-urlencoded"
> ```

**Example: Get lens temperature (via ObservingConditions)**
```bash
curl -X PUT -d "Action=getlenstemperature" http://localhost:32241/api/v1/observingconditions/0/action
```

**Windows PowerShell:**
```powershell
Invoke-WebRequest -Uri http://localhost:32241/api/v1/observingconditions/0/action -Method PUT -Body "Action=getlenstemperature" -ContentType "application/x-www-form-urlencoded"
```

### Reading Sensor Values (Sensor Switches)

The power metrics (Voltage, Current, Power) are exposed as read-only ASCOM Switch devices at **fixed IDs 0, 1, and 2**. These can be used to display values in NINA gauges or any ASCOM client that supports analog switch values.

| ID | Name | Unit | Description |
|----|------|------|-------------|
| 0 | Input Voltage | V | Input voltage from power supply |
| 1 | Total Current | A | Total current draw of all outputs |
| 2 | Total Power | W | Total power consumption |

> [!NOTE]
> **Sensor switch IDs are always fixed (0, 1, 2).** Unlike power switches, sensor IDs do not shift when switches are disabled. Power switches start at ID 3.

**Reading Sensor Values via API:**

**Linux/Mac/Git Bash (native curl):**
```bash
# Read input voltage (ID 0)
curl "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=0"

# Read total current (ID 1)
curl "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=1"

# Read total power (ID 2)
curl "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=2"
```

**Windows PowerShell:**
```powershell
# Read input voltage (ID 0)
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=0"

# Read total current (ID 1)
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=1"

# Read total power (ID 2)
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=2"
```

> **Tip for localized Windows:** `Invoke-RestMethod` parses JSON and displays numbers using your locale (e.g., `12,8` in German). To get the raw JSON with standard decimal format, use `Invoke-WebRequest` and access the `.Content` property:
> ```powershell
> # Get raw JSON (always uses period as decimal separator)
> (Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=0").Content
>
> # Example output: {"ClientTransactionID":0,"ServerTransactionID":123,"ErrorNumber":0,"ErrorMessage":"","Value":12.87}
> ```

**Example Response:**
```json
{
  "ClientTransactionID": 0,
  "ServerTransactionID": 123,
  "ErrorNumber": 0,
  "ErrorMessage": "",
  "Value": 12.87
}
```

### Controlling Individual Switches via REST API

Beyond the custom actions, you can directly control individual switches using the standard ASCOM Alpaca `Switch` endpoints.

> [!IMPORTANT]
> **Switch ID Schema:** Sensor switches (Voltage, Current, Power) always occupy IDs 0, 1, 2. Power switches start at ID 3. When you disable a power switch in the configuration, it is removed from the ASCOM device list, causing subsequent power switch IDs to shift down. Sensor IDs remain fixed.

**Endpoints:**
- `PUT /api/v1/switch/0/setswitch` – Set a switch on or off (parameters: `Id`, `State`)
- `PUT /api/v1/switch/0/setswitchvalue` – Set a switch value (parameters: `Id`, `Value`) – used for adjustable voltage (0-15V)
- `GET /api/v1/switch/0/getswitch?Id=X` – Get the current state of a switch (on/off)
- `GET /api/v1/switch/0/getswitchvalue?Id=X` – Get the current value of a switch (e.g., voltage for adj_conv)

**Examples using native `curl` (Linux/Mac/Git Bash):**

```bash
# Turn switch ID 3 (typically DC1) ON
curl -X PUT -d "Id=3&State=true" http://localhost:32241/api/v1/switch/0/setswitch

# Turn switch ID 3 OFF
curl -X PUT -d "Id=3&State=false" http://localhost:32241/api/v1/switch/0/setswitch

# Get the current state of switch ID 3
curl "http://localhost:32241/api/v1/switch/0/getswitch?Id=3"

# Set adjustable converter to 9.5V (requires EnableAlpacaVoltageControl in proxy config)
# Replace ID 10 with the actual ID of adj_conv in your configuration
curl -X PUT -d "Id=10&Value=9.5" http://localhost:32241/api/v1/switch/0/setswitchvalue

# Get the current voltage of the adjustable converter
curl "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=10"
```

**Examples using Windows PowerShell:**

```powershell
# Turn switch ID 3 (typically DC1) ON
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitch" -Method PUT -Body "Id=3&State=true" -ContentType "application/x-www-form-urlencoded"

# Turn switch ID 3 OFF
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitch" -Method PUT -Body "Id=3&State=false" -ContentType "application/x-www-form-urlencoded"

# Get the current state of switch ID 3
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitch?Id=3"

# Set adjustable converter to 9.5V (requires EnableAlpacaVoltageControl in proxy config)
# Replace ID 10 with the actual ID of adj_conv in your configuration
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitchvalue" -Method PUT -Body "Id=10&Value=9.5" -ContentType "application/x-www-form-urlencoded"

# Get the current voltage of the adjustable converter
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=10"
```

## Configuration Reference

The proxy creates its configuration files in the following directory on Windows:
*   **Windows:** `%APPDATA%\SV241AlpacaProxy\`

### Manual Configuration (`proxy_config.json`)

While most settings can be configured via the web interface, it is also possible to edit the `proxy_config.json` file directly. This can be useful for troubleshooting or for setting up the proxy in a headless environment.

Here is an example of the `proxy_config.json` file structure:

```json
{
  "serialPortName": "COM9",
  "autoDetectPort": false,
  "networkPort": 32241,
  "listenAddress": "127.0.0.1",
  "logLevel": "INFO",
  "historyRetentionNights": 10,
  "telemetryInterval": 10,
  "enableAlpacaVoltageControl": false,
  "enableMasterPower": false,
  "switchNames": {
    "adj_conv": "Adjustable Voltage",
    "dc1": "Camera",
    "dc2": "Mount",
    "dc3": "Focuser",
    "dc4": "Filter Wheel",
    "dc5": "Unused",
    "pwm1": "Main Dew Heater",
    "pwm2": "Guide Scope Heater",
    "usb345": "USB Hub 1",
    "usbc12": "USB Hub 2",
    "master_power": "Master Power"
  },
  "heaterAutoEnableLeader": {
    "pwm1": true,
    "pwm2": true
  }
}
```

**Parameter Explanation:**

*   `serialPortName` (string): The name of the serial port for the SV241 device (e.g., `"COM9"`). If this string is empty (`""`), the proxy will attempt to auto-detect the port on startup.
    > **Note:** When `Auto-Detect Port` is enabled (or `serialPortName` is empty), the proxy probes all available USB serial ports to find the SV241. This "safe-but-aggressive" probing can potentially interfere with other sensitive devices (e.g., Mounts, Weather Stations). **Solution:** To prevent conflicts, connect the SV241 once to let it auto-detect, then **disable "Auto-Detect Port"** (or uncheck the box in the web UI) **and ensure a port name is configured**. The proxy will then strictly only open the configured port.
    >
    > **Important:** If you disable `Auto-Detect Port` but leave `serialPortName` empty, the proxy will still fall back to auto-detection. Both settings must be configured together: disable auto-detect AND specify the port name.
*   `autoDetectPort` (boolean): When `true`, the proxy will attempt to find the SV241 automatically if the configured port fails. When `false` **and** a `serialPortName` is specified, the proxy will only try the configured port. Default is `true`.
*   `networkPort` (integer): The TCP port on which the Alpaca API server will listen for connections from client applications. The default is `32241`. A restart of the proxy is required for changes to this value to take effect.
*   `listenAddress` (string): The IP address to bind the server to. Use `"127.0.0.1"` for local-only access (recommended for security) or `"0.0.0.0"` to allow network access. Default is `"127.0.0.1"`.
*   `logLevel` (string): Controls the verbosity of the log file. Valid values are `"ERROR"`, `"WARN"`, `"INFO"`, and `"DEBUG"`. This setting is applied live when changed.
*   `historyRetentionNights` (integer): The number of days/nights to retain CSV telemetry logs. Older files are automatically deleted at startup. Default is `10`.
*   `telemetryInterval` (integer): The interval in seconds between telemetry log entries. Default is `10`.
*   `enableAlpacaVoltageControl` (boolean): When `true`, the adjustable voltage output can be controlled as a slider (0-15V) via ASCOM. When `false`, it behaves as a simple on/off switch. Default is `false`.
*   `enableMasterPower` (boolean): When `true`, a "Master Power" switch is exposed via ASCOM that controls all outputs simultaneously. Default is `false`.
*   `switchNames` (object): A map that allows you to assign custom, user-friendly names to the internal switch identifiers. The `key` is the internal name (e.g., `"dc1"`) and the `value` is the custom name you want to see in ASCOM clients and the web interface.
*   `heaterAutoEnableLeader` (object): Controls automatic leader activation for PID-Sync mode. When a follower heater (in mode 3) is enabled, the proxy can automatically enable its leader heater. Keys are `"pwm1"` and `"pwm2"`, values are `true`/`false`.


### Log Level Configuration

The proxy driver provides a configurable logging level to control the amount of detail written to the log file (`proxy.log` in the configuration directory). This is useful for both normal operation and detailed troubleshooting. The log level can be set in the **"Proxy Connection Settings"** section of the web setup page.

The available levels are:

*   **ERROR**: Logs only critical errors that prevent the proxy from working correctly (e.g., failure to open a serial port, server start failure).
*   **WARN**: Logs warnings about non-critical issues that the proxy can recover from (e.g., a temporary connection loss, auto-detection failures).
*   **INFO** (Default): Logs major events during normal operation, such as application start/stop, successful connections, and configuration changes. This level provides a good overview without being too verbose.
*   **DEBUG**: Logs highly detailed information, including every incoming HTTP request, every command sent to the device, and periodic status checks. This level is extremely useful for diagnosing communication problems with ASCOM client software but will create very large log files.

### Log Rotation

To prevent the log file from growing indefinitely, the proxy performs a simple rotation upon every start:
1.  The existing `proxy.log` is renamed to `proxy.log.old`, overwriting any previous `.old` file.
2.  A new, empty `proxy.log` is created for the current session.

This ensures that the logs from the current and the immediately preceding session are always available for troubleshooting.

---

## Advanced: Development & Building

<details>
<summary><strong>Click to expand the Developer Guide</strong></summary>

### Project Structure
```
AscomAlpacaProxy/
├── frontend-vue/     # Vue 3 SPA (web interface)
│   ├── src/          # Vue components & stores
│   ├── public/       # Static assets (flasher, favicon)
│   └── dist/         # Build output (generated)
├── internal/         # Go backend modules
├── build/            # Compiled .exe (generated)
└── install/          # Installer output (generated)
```

### Prerequisites
- **Node.js 18+** (for frontend)
- **Go 1.21+** (for proxy backend)
- **PlatformIO** (for firmware & proxy builds)
- **[Inno Setup](https://jrsoftware.org/isinfo.php)** (for Windows installer)

### Building the Frontend
```bash
cd AscomAlpacaProxy/frontend-vue
npm install
npm run build   # Output: dist/
```

### Building the Go Proxy (.exe only)
```bash
pio run --target buildgoproxyexe --environment AscomAlpacaProxyWinExecutable
```

### Building the Windows Installer
```bash
pio run --target buildgoproxyinstaller --environment AscomAlpacaProxyWinExecutable
```
> **Requires:** Inno Setup installed and `ISCC.exe` in PATH

### Development Mode (Hot Reload)
```bash
cd AscomAlpacaProxy/frontend-vue
npm run dev     # Dev server: http://localhost:5173
```
> **Note:** The dev server proxies API requests to the running Go proxy on port 32241.

</details>
