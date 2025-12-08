# SV241-Unbound Telescope Power Controller

This is a replacement firmware for the **Svbony SV241 Pro**.
**DISCLAIMER:** This firmware is provided "as-is" without any warranty. Use it at your own risk. The author is not responsible for any damage to your device.

## Table of Contents

*   [Project Overview](#project-overview)
*   [Quick Start Guide](#quick-start-guide)
    *   [1. Flashing the Firmware](#1-flashing-the-firmware)
    *   [2. Running the ASCOM Alpaca Proxy](#2-running-the-ascom-alpaca-proxy)
    *   [3. Connecting from Astronomy Software](#3-connecting-from-astronomy-software)
*   [The ASCOM Alpaca Proxy](#the-ascom-alpaca-proxy)
*   [Advanced: Serial Command Interface](#advanced-serial-command-interface)

## Project Overview

This project consists of two main components:
1.  **Custom Firmware:** A replacement firmware for the ESP32-based Svbony SV241 Pro controller. It unlocks advanced control over power outputs and dew heaters.
2.  **ASCOM Alpaca Proxy:** A standalone application that runs on your computer. It connects to the controller via USB and exposes its functions as standard ASCOM devices, making it compatible with popular astronomy software like NINA, SGP, etc.

### Firmware Features
*   Control for 5 DC outputs, 2 USB groups, and 1 adjustable voltage output (with 0-15V slider control).
*   Advanced dew heater control:
    *   **Manual Mode:** Variable 0-100% PWM control.
    *   **PID Mode:** Automatic temperature regulation.
    *   **Ambient Tracking:** Sensorless power adjustment.
*   On-board sensor suite for monitoring power, ambient temperature/humidity, and lens temperature. The firmware is resilient to sensor failures.
*   Experimental automatic drying cycle for the SHT40 humidity sensor.
*   Configuration persistence across reboots.
*   A powerful JSON-based serial command interface for direct control and integration.

## Quick Start Guide

Follow these steps to get up and running quickly.

### 1. Flashing the Firmware

The easiest way to install the firmware is using the web flasher.

1.  Connect the SV241 controller to your computer via USB.
2.  Open the **[SV241-Unbound Web Flasher](https://diyastro.github.io/SV241-Unbound/)** in a modern web browser (Chrome, Edge).
3.  Click "Connect", select the correct COM port for your device, and follow the on-screen instructions.

### 2. Running the ASCOM Alpaca Proxy

The proxy is the bridge between the hardware and your astronomy software.

1.  Download the latest installer (`SV241-AscomAlpacaProxy-Setup-x.x.exe`) from the project's releases page.
2.  Run the installer. It's recommended to allow the proxy to start automatically with Windows.
3.  Once running, an icon will appear in your system tray. The proxy will automatically detect the controller's COM port and connect to it.

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

[**ASCOM Alpaca Proxy Documentation](./AscomAlpacaProxy/)

---

## Advanced: Serial Command Interface

<details>
<summary><strong>Click to expand the detailed Serial Command Reference</strong></summary>

The controller communicates over serial at **115200 baud**. Commands are sent as JSON strings, terminated by a newline character (`\n`).

> **Important:** All JSON commands must be sent as a single, continuous line of text without any line breaks, followed by a single newline character (`\n`) to execute the command.

### Get Sensor Data

*   **Request:** `{"get": "sensors"}`
*   **Response:** A JSON object with the latest sensor readings.
    *   `v`: Input Voltage (V), `i`: Input Current (mA), `p`: Input Power (W)
    *   `t_amb`: Ambient Temp (°C), `h_amb`: Ambient Humidity (%), `d`: Dew Point (°C)
    *   `t_lens`: Lens Temp (°C), `pwm1`/`pwm2`: Heater Power (%)
    *   `hf`, `hmf`, `hma`, `hs`: Heap memory statistics (Bytes)

### Get Power Status

*   **Request:** `{"get": "status"}`
*   **Response:** A JSON object with the on/off state (`1`/`0`) of all outputs.
    *   Example: `{"status":{"d1":1,"d2":1,"d3":0,...}}`

### Set Power State

*   **Request:** `{"set": {"<output_name>": <state>, ...}}`
*   **`<output_name>`:** `d1`-`d5`, `u12`, `u34`, `adj`, `pwm1`, `pwm2`, or `all`.
*   **`<state>`:** `1` or `true` for ON, `0` or `false` for OFF.
*   **Response:** The new power status JSON, reflecting the state after the change has been applied.

### System Commands

*   **Reboot:** `{"command": "reboot"}`
*   **Factory Reset:** `{"command": "factory_reset"}`
*   **Get Firmware Version:** `{"get": "version"}`
    *   **Response:** A JSON object containing the firmware version (e.g., `{"version": "1.0.0"}`). This value is defined in the firmware source code.

### Get/Set Full Configuration

*   **Get Config Request:** `{"get": "config"}`
*   **Set Config Request:** `{"sc": { ... }}`
*   **Response (for both):** The complete configuration JSON, reflecting the state after the change has been applied.

#### Configuration Object Structure
*   **Parameter Breakdown:**
    The body of the request is a JSON object containing one or more of the following top-level keys. You only need to send the keys for the settings you wish to change.

For numerical parameters without explicit ranges, typical values are expected. Refer to the firmware's source code for precise limits if needed.

| Key | Description | Value Type |
|:----|:------------------------------------------------------------------|:-----------|
| `so` | **S**ensor **O**ffsets: Sets calibration offsets for sensor readings. | `object` |
| `ui` | **U**pdate **I**ntervals: Sets the update frequency for sensors. | `object` |
| `ps` | **P**ower **S**tartup: Defines the on/off state of outputs at boot. | `object` |
| `ac` | **A**veraging **C**ounts: Controls the samples for the median filter. | `object` |
| `av` | **A**djustable **V**oltage: Sets the preset voltage for the converter. | `float` |
| `ad` | **A**uto **D**ry: Configures the automatic sensor drying feature. | `object` |
| `dh` | **D**ew **H**eaters: Configures the two dew heaters. | `array` |

---

#### `so` (Sensor Offsets)
| Sub-Key | Description | Value Type |
|:---|:--------------------------------|:-----------|
| `st` | SHT40 Temperature offset (°C) | `float` |
| `sh` | SHT40 Humidity offset (%) | `float` |
| `dt` | DS18B20 Temperature offset (°C) | `float` |
| `iv` | INA219 Voltage offset (V) | `float` |
| `ic` | INA219 Current offset (mA) | `float` |

#### `ui` (Update Intervals)
| Sub-Key | Description | Value Type |
|:---|:--------------------------------|:---------------|
| `i` | INA219 (Power) interval (ms) | `unsigned long`|
| `s` | SHT40 (Ambient) interval (ms) | `unsigned long`|
| `d` | DS18B20 (Lens) interval (ms) | `unsigned long`|

#### `ps` (Power Startup States)
| Sub-Key | Description | Value Type |
|:---|:------------------------------------------------|:---------|
| `d1`-`d5` | Startup state for DC Outputs 1-5 | `boolean`|
| `u12` | Startup state for USB Group 1/2 | `boolean`|
| `u34` | Startup state for USB Group 3/4/5 | `boolean`|
| `adj` | Startup state for the Adjustable Voltage Converter | `boolean`|

#### `ac` (Averaging Counts)
| Sub-Key | Description | Value Type |
|:---|:-----------------------------------|:---------|
| `st` | Sample count for SHT40 temperature | `int` |
| `sh` | Sample count for SHT40 humidity | `int` |
| `dt` | Sample count for DS18B20 temperature | `int` |
| `iv` | Sample count for INA219 voltage | `int` |
| `ic` | Sample count for INA219 current | `int` |

#### `ad` (Auto Dry)
| Sub-Key | Description | Value Type |
|:---|:------------------------------------------------------------------------------------------------|:---------------|
| `en` | **En**able the auto-dry feature (`true`/`false`). | `boolean` |
| `ht` | **H**umidity **T**hreshold: The humidity (%) above which the trigger timer starts (e.g., `99.0`). | `float` |
| `td` | **T**rigger **D**uration: The time in **seconds** the humidity must stay above the threshold to trigger the heater (e.g., `300` for 5 minutes). | `unsigned long`|


---

#### `dh` (Dew Heaters)
This is an array that can contain up to two heater configuration objects. To update a specific heater, you place its configuration object at the corresponding index (0 for PWM1, 1 for PWM2).

**Common Heater Properties:**
| Key | Description | Value Type |
|:----|:------------------------------------------------------------------------------------------------|:---------|
| `n` | Name of the heater (e.g., "PWM1"). This is read-only. | `string` |
| `en` | **En**abled on startup: `true` to enable the heater on boot. | `boolean` |
| `m` | **M**ode: Sets the control mode for the heater (0: Manual, 1: PID, 2: Ambient Tracking). | `int` |

**Mode-Specific Properties:**

*   **Mode 0: Manual**
    | Key | Description | Value Type |
    |:----|:-------------------------------------------|:-----------|
    | `mp` | **M**anual **P**ower (0-100%). | `int` (0-100) |

*   **Mode 1: PID (Lens Sensor)**
    | Key | Description | Value Type |
    |:----|:------------------------------------------------------------------------------------------------|:-----------|
    | `to` | **T**arget **O**ffset: Desired temperature difference above the dew point (e.g., `3.0` for 3°C warmer). | `float` |
    | `kp` | **P**roportional gain: Reacts proportionally to the current temperature error. Higher values lead to a stronger, faster reaction. | `double` |
    | `ki` | **I**ntegral gain: Accumulates past errors to correct small, constant offsets over time. Helps eliminate steady-state errors. | `double` |
    | `kd` | **D**erivative gain: Reacts to the rate of temperature change. Helps to dampen overshoot and oscillations. | `double` |

*   **Mode 2: Ambient Tracking (Sensorless)**
    | Key | Description | Value Type |
    |:----|:------------------------------------------------------------------------------------------------|:-----------|
    | `sd` | **S**tart **D**elta: Temp difference (Ambient - Dew Point) at which heating begins. | `float` |
    | `ed` | **E**nd **D**elta: Temp difference at which the heater reaches its maximum configured power. | `float` |
    | `xp` | Ma**x** **P**ower (0-100%): The maximum power the heater is allowed to use in this mode. | `int` (0-100) |

*   **Mode 3: PID-Sync (Follower)**
    This mode allows a heater (the "follower") to mirror the power output of another heater running in PID mode (the "leader"). It is ideal for a guidescope heater that should follow the main scope's heater without needing its own sensor.
    | Key | Description | Value Type |
    |:----|:------------------------------------------------------------------------------------------------|:-----------|
    | `sf` | **S**ync **F**actor: A multiplier for the leader's power (e.g., `0.8` means the follower runs at 80% of the leader's power). | `float` |


*   **Examples:**

    *   **Change Adjustable Converter Voltage:**
        ```json
        {"sc": {"av": 9.5}}
        ```

    *   **Set Heater 1 (PWM1) to Manual 50% power:**
        ```json
        {"sc": {"dh": [{"m": 0, "mp": 50}]}}
        ```
        *(Note: `[{"m":...}]` targets the first heater. The array index matters.)*

    *   **Configure Heater 2 (PWM2) for Ambient Tracking:**
        ```json
        {"sc": {"dh": [null, {"m": 2, "sd": 6.0, "ed": 1.5, "xp": 75}]}}
        ```
        *(Note: `null` is used as a placeholder to indicate that Heater 1's configuration should not be changed.)*

    *   **Comprehensive Example: Change multiple settings at once:**
        This example sets the startup state for DC1 to ON, changes the voltage preset, and configures Heater 1 for PID control.
        ```json
        {"sc":{"ps":{"d1":true},"av":8.5,"dh":[{"m":1,"to":2.5,"kp":150}]}}
        ```

</details>


</details>
