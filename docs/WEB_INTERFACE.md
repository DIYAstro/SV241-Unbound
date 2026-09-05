# Web Interface Guide

[← Back to proxy overview](./ASCOM_PROXY.md)

This guide provides a walkthrough of the web interface, explaining each panel and configuration tab.

## Table of Contents

- [Live Telemetry Panel](#live-telemetry-panel)
- [Power Control Panel](#power-control-panel)
- [Configuration Tabs](#configuration-tabs)
  - [Switches Tab](#switches-tab)
  - [Dew Heaters Tab](#dew-heaters-tab)
    - [How PID Mode Works](#how-pid-mode-works)
    - [Simplified PID Tuning Guide](#simplified-pid-tuning-guide)
  - [Sensors Tab](#sensors-tab)
  - [Weather Service Tab](#weather-service-tab)
  - [Proxy Tab](#proxy-tab)
  - [System Tab](#system-tab)
- [Live Log Panel](#live-log-panel)

## Live Telemetry Panel

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

> [!TIP]
> Use the chart button in the telemetry panel header to open the **Telemetry Explorer** for interactive historical charts and data export.

## Power Control Panel

This panel provides quick access to power output control:

*   **Master Power Toggle:** Controls all power outputs simultaneously (if enabled in Proxy settings).
*   **Individual Switches:** Toggle each power output independently. Switches marked as "Disabled" in the firmware configuration are automatically hidden.

## Configuration Tabs

The collapsible "Configuration & Settings" section contains six tabs:

### Switches Tab
Configure power switch behavior:
*   **State (Startup):** Set each switch to On, Off, or Disabled at device boot.
*   **Custom Name:** Assign user-friendly names that appear in ASCOM clients.
*   **Voltage:** Set the adjustable converter output voltage (0-15V).

> [!NOTE]
> **Names Follow the Box, Not the Computer:** Custom switch names (along with the Lens Temp label, dew heater auto-enable-leader preference, and weather source priorities) are stored per physical SV241 box, identified by its factory-set serial number - not per proxy installation. Plug a different box into the same computer and it gets its own clean set of names instead of inheriting the previous box's; plug a box you've configured before back in (even on a different computer, via a restored backup) and its names come right back. Give a box a friendly label under **System Tab > Connected Box** ("Rig Name") - it then appears in the header badge and lets you tell boxes apart at a glance instead of by raw serial number.

### Dew Heaters Tab
Configure the two PWM dew heater outputs:
*   **Enable on Startup:** Automatically enable the heater when the device boots.
*   **Mode:** Select the control strategy:
    - *Manual:* Fixed power percentage.
    - *PID (Lens Sensor):* Automatic control using the DS18B20 lens temperature sensor.
    - *Ambient Tracking:* Power scales based on proximity to dew point.
    - *PID-Sync (Follower):* Follows another heater's output (useful for dual-heater setups).
    - *Minimum Temperature:* Maintains a minimum lens temperature.
    - *Disabled:* Heater is hidden from UI and ASCOM.

#### How PID Mode Works

In **PID Mode**, the controller automatically adjusts heater power to maintain the lens temperature at a safe level above the dew point:

*   **Lens Temp:** The current temperature of the lens, measured by an external sensor.
*   **Target Temp / Minimum Temp:** The desired temperature offset above the ambient dew point (Recommended: 2.0°C - 5.0°C).

The controller calculates the dew point from ambient temperature and humidity, adds the target offset, and adjusts heater power to maintain that temperature.

> [!IMPORTANT]
> The lens temperature sensor (labeled **TEMP** on the SV241) must be positioned **under or adjacent to the dew heater strap**. This ensures the sensor measures the heated area and provides accurate feedback for the PID controller.

#### Simplified PID Tuning Guide
PID mode automatically regulates the heater to keep your optics dry. If you notice unstable temperatures, use this guide to tune the parameters:

*   **Target Offset:** Desired temperature difference above the Dew Point (Recommended: 2.0°C - 5.0°C).
*   **Kp (Aggressiveness):** Controls how hard the heater pushes.
    *   *Problem:* Temperature swings up and down (Oscillation) -> **Reduce Kp**.
    *   *Problem:* Heating is too slow -> **Increase Kp**.
*   **Ki (Correction):** Corrects small, constant errors.
    *   *Problem:* Temperature stabilizes *below* the target -> **Increase Ki**.
*   **Kd (Damping):** Prevents shooting past the target.
    *   *Problem:* Temperature spikes significantly above target on startup (Overshoot) -> **Increase Kd**.

### Sensors Tab
Fine-tune sensor readings:
*   **Offsets:** Calibrate temperature, humidity, voltage, and current readings.
*   **Averaging:** Set the number of samples to average (reduces noise).
*   **Intervals:** Configure sensor polling frequency.
*   **SHT40 Auto-Drying:** Enable automatic sensor heater activation at high humidity levels.

### Weather Service Tab
Configure supplemental environmental data:
*   **Enable Weather Service:** Fetch professional meteorological data from Open-Meteo.
*   **Location Detection:** Automatically detect coordinates (Latitude/Longitude) via the browser's Geolocation API.
*   **Sourcing Priority:** For metrics supported by SV241 hardware (Temperature, Humidity, Dew Point), choose the sourcing strategy:
    - *Hardware:* Exclusively use SV241 internal sensors.
    - *Internet:* Exclusively use Open-Meteo data.
    - *Hybrid:* Use hardware if available, fallback to Open-Meteo if hardware is initializing or missing.
    - > **Note:** Metrics without hardware counterparts (Wind, Pressure, Clouds, Rain) are automatically sourced from Open-Meteo.
*   **Prediction Models:** Select from global (ECMWF, GFS) or regional (ICON) weather models.

> [!NOTE]
> **Data Exposure:** Data from the Weather Service is exclusively exposed through the ASCOM Alpaca `ObservingConditions` interface and is not displayed as primary telemetry in the Web UI.

### Proxy Tab
Configure the proxy application itself:
*   **Connection:** Serial port settings, auto-detection toggle.
*   **Network:** Listen address, port, and log level.
*   **Discovery Service:** Enable or disable the Alpaca discovery responder (UDP port 32227). Recommended to keep enabled unless multiple Alpaca servers are running on the same machine and causing port conflicts.
*   **ASCOM Features:**
    - *Voltage Slider:* Enable/disable analog control for the adjustable output (0-15V).
    - *Master Power:* Expose a virtual switch to control all outputs at once.
    - *Persistent Lens Temp:* Keep the Lens Temperature sensor exposed to ASCOM even in manual mode (handy for monitoring without PID).
*   **Telemetry:** Configure history retention period.

### System Tab
Maintenance and backup functions:
*   **Manual Actions:** Trigger a sensor drying cycle manually.
*   **Backup & Restore:** Export or import the complete configuration (both proxy and firmware settings). A backup remembers which physical box its firmware settings (calibration offsets, heater configuration, etc.) came from; restoring it while a *different* box is connected is blocked with a warning by default, since applying one box's on-device settings to another is rarely what you want - confirm explicitly if that's intentional (e.g. replacing one box with another).
*   **Danger Zone:** Contains critical device operations:
    *   **Update Firmware:** Opens the integrated web flasher to update the SV241 firmware directly from the browser using the Web Serial API—no additional tools required.
        > [!NOTE]
        > Flashing requires the browser (Chrome/Edge) to run on the **same machine** where the SV241 device is connected via USB. Alternatively, use the [standalone Web Flasher](https://diyastro.github.io/SV241-Unbound/).
    *   **Reboot Device:** Performs a soft restart of the SV241 device.
    *   **Factory Reset:** Erases all saved settings on the device and restores factory defaults.

> [!IMPORTANT]
> **ASCOM Client Reconnection Required:** Changes to the ASCOM Features settings (Master Power switch, Voltage Slider mode) also require your astronomy software to **disconnect and reconnect** to see the updated switch configuration.

> [!NOTE]
> When disabling Auto-Detect Port, make sure to also specify a serial port name. See [Configuration Reference](./CONFIGURATION_REFERENCE.md#manual-configuration-proxy_configjson) for details.

## Live Log Panel

The collapsible log viewer shows real-time proxy activity:
*   Color-coded entries (errors in red, warnings in yellow).
*   Auto-scrolls to newest entries (pauses when you scroll up).
*   Download button to save the current log file.
