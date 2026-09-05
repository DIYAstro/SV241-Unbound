# Serial Command Interface

[← Back to main readme](../readme.md)

Low-level reference for the JSON-over-serial protocol the SV241 firmware speaks directly -
useful for scripting, debugging, or talking to the device without the SV241 Alpaca Proxy at
all. Most users won't need this; the [SV241 Alpaca Proxy](./ASCOM_PROXY.md) already exposes
this functionality over friendlier interfaces (web UI, REST API).

## Table of Contents

- [Get Sensor Data](#get-sensor-data)
- [Get Power Status](#get-power-status)
- [Set Power State](#set-power-state)
- [System Commands](#system-commands)
- [Get/Set Full Configuration](#getset-full-configuration)
  - [Configuration Object Structure](#configuration-object-structure)
  - [`so` (Sensor Offsets)](#so-sensor-offsets)
  - [`ui` (Update Intervals)](#ui-update-intervals)
  - [`ps` (Power Startup States)](#ps-power-startup-states)
  - [`ac` (Averaging Counts)](#ac-averaging-counts)
  - [`ad` (Auto Dry)](#ad-auto-dry)
  - [`dh` (Dew Heaters)](#dh-dew-heaters)
- [Using PowerShell for Direct Serial Communication](#using-powershell-for-direct-serial-communication)

The controller communicates over serial at **115200 baud**. Commands are sent as JSON strings, terminated by a newline character (`\n`).

> [!IMPORTANT]
> All JSON commands must be sent as a single, continuous line of text without any line breaks, followed by a single newline character (`\n`) to execute the command.

## Get Sensor Data

*   **Request:** `{"get": "sensors"}`
*   **Response:** A JSON object with the latest sensor readings.
    *   `v`: Input Voltage (V), `i`: Input Current (mA), `p`: Input Power (W)
    *   `t_amb`: Ambient Temp (°C), `h_amb`: Ambient Humidity (%), `d`: Dew Point (°C)
    *   `t_lens`: Lens Temp (°C), `pwm1`/`pwm2`: Heater Power (%)
    *   `hf`, `hmf`, `hma`, `hs`: Heap memory statistics (Bytes)

## Get Power Status

*   **Request:** `{"get": "status"}`
*   **Response:** A JSON object with the on/off state (`1`/`0`) of all outputs.
    *   Example: `{"status":{"d1":1,"d2":1,"d3":0,...},"dm":[0,1]}`
    *   `dm`: Dew heater modes array (0: Manual, 1: PID, 2: Ambient Tracking, 3: PID-Sync, 4: Min Temp, 5: Disabled)

## Set Power State

*   **Request:** `{"set": {"<output_name>": <state>, ...}}`
*   **`<output_name>`:** `d1`-`d5`, `u12`, `u34`, `adj`, `pwm1`, `pwm2`, or `all`.
*   **`<state>`:** `1` or `true` for ON, `0` or `false` for OFF.
*   **Response:** The new power status JSON, reflecting the state after the change has been applied.

## System Commands

*   **Reboot:** `{"command": "reboot"}`
*   **Factory Reset:** `{"command": "factory_reset"}`
*   **Manual Sensor Drying:** `{"command": "dry_sensor"}`
    *   Triggers the SHT40 internal heater to remove condensation. This is a blocking operation.
*   **Get Firmware Version:** `{"get": "version"}`
    *   **Response:** A JSON object containing the firmware version and the device's factory MAC address, used by the Proxy as a per-box serial number to keep switch names and related settings tied to this specific physical box (e.g., `{"version": "1.0.0", "mac": "AA:BB:CC:11:22:33"}`).

## Get/Set Full Configuration

*   **Get Config Request:** `{"get": "config"}`
*   **Set Config Request:** `{"sc": { ... }}`
*   **Response (for both):** The complete configuration JSON, reflecting the state after the change has been applied.

### Configuration Object Structure
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

### `so` (Sensor Offsets)
| Sub-Key | Description | Value Type |
|:---|:--------------------------------|:-----------|
| `st` | SHT40 Temperature offset (°C) | `float` |
| `sh` | SHT40 Humidity offset (%) | `float` |
| `dt` | DS18B20 Temperature offset (°C) | `float` |
| `iv` | INA219 Voltage offset (V) | `float` |
| `ic` | INA219 Current offset (mA) | `float` |

### `ui` (Update Intervals)
| Sub-Key | Description | Value Type |
|:---|:--------------------------------|:---------------|
| `i` | INA219 (Power) interval (ms) | `unsigned long`|
| `s` | SHT40 (Ambient) interval (ms) | `unsigned long`|
| `d` | DS18B20 (Lens) interval (ms) | `unsigned long`|

### `ps` (Power Startup States)
| Sub-Key | Description | Value Type |
|:---|:------------------------------------------------|:---------|
| `d1`-`d5` | Startup state for DC Outputs 1-5 | `boolean`|
| `u12` | Startup state for USB Group 1/2 | `boolean`|
| `u34` | Startup state for USB Group 3/4/5 | `boolean`|
| `adj` | Startup state for the Adjustable Voltage Converter | `boolean`|

### `ac` (Averaging Counts)
| Sub-Key | Description | Value Type |
|:---|:-----------------------------------|:---------|
| `st` | Sample count for SHT40 temperature | `int` |
| `sh` | Sample count for SHT40 humidity | `int` |
| `dt` | Sample count for DS18B20 temperature | `int` |
| `iv` | Sample count for INA219 voltage | `int` |
| `ic` | Sample count for INA219 current | `int` |

### `ad` (Auto Dry)
| Sub-Key | Description | Value Type |
|:---|:------------------------------------------------------------------------------------------------|:---------------|
| `en` | **En**able the auto-dry feature (`true`/`false`). | `boolean` |
| `ht` | **H**umidity **T**hreshold: The humidity (%) above which the trigger timer starts (e.g., `99.0`). | `float` |
| `td` | **T**rigger **D**uration: The time in **seconds** the humidity must stay above the threshold to trigger the heater (e.g., `300` for 5 minutes). | `unsigned long`|


---

### `dh` (Dew Heaters)
This is an array that can contain up to two heater configuration objects. To update a specific heater, you place its configuration object at the corresponding index (0 for PWM1, 1 for PWM2).

**Common Heater Properties:**
| Key | Description | Value Type |
|:----|:------------------------------------------------------------------------------------------------|:---------|
| `n` | Name of the heater (e.g., "PWM1"). This is read-only. | `string` |
| `en` | **En**abled on startup: `true` to enable the heater on boot. | `boolean` |
| `m` | **M**ode: Sets the control mode for the heater (0: Manual, 1: PID, 2: Ambient Tracking, 3: PID-Sync, 4: Minimum Temperature, 5: Disabled). | `int` |
| `xd` | **M**ax **D**uty: Hard safety limit (0-100%) on the raw PWM duty cycle, enforced in *every* mode. Default `100` (no limit). Useful for heater bands rated below the 12V supply voltage. | `int` (0-100) |

> **Note on `xd`:** This limit acts on the raw electrical duty cycle, not on the "power %" values (`mp`, `xp`, PID output) used elsewhere in this API. Those values pass through a non-linear gamma curve before becoming a duty cycle, so a limit expressed in "power %" would not reliably cap the real voltage/power delivered to the heater. `xd` bypasses that curve and caps the hardware output directly. There is no fixed formula to translate a target voltage (e.g. "never exceed 5V on a 12V rail") into an exact `xd` percentage, since the real relationship depends on your heater's electrical characteristics — start conservatively low and verify with a multimeter or by monitoring temperature before relying on it unattended. In PID-Sync (Mode 3), the follower's `xd` is independent of the leader's `xd`. At very low `xd` values (roughly below 15%), the reported power percentage (`pwm1`/`pwm2` in `{"get":"sensors"}`, the web UI) may show `0%` even though the heater is still outputting a small, real, nonzero duty cycle up to the configured limit — this is a display rounding artifact only; the actual hardware output always tracks `xd` as closely as possible without ever exceeding it.

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
    | `psf` | **P**ID **S**ync **F**actor: A multiplier for the leader's power (e.g., `0.8` means the follower runs at 80% of the leader's power). | `float` |

*   **Mode 4: Minimum Temperature**
    This mode works like PID mode, but ensures the lens temperature never drops below a configured minimum, regardless of the dew point.
    | Key | Description | Value Type |
    |:----|:------------------------------------------------------------------------------------------------|:-----------|
    | `to` | **T**arget **O**ffset: Desired temperature difference above the dew point (same as PID mode). | `float` |
    | `mt` | **M**inimum **T**emperature: The absolute minimum lens temperature to maintain (e.g., `5.0` for 5°C). | `float` |
    | `kp`, `ki`, `kd` | PID tuning parameters (same as Mode 1). | `double` |

*   **Mode 5: Disabled**
    This mode completely disables the heater output and hides it from the ASCOM interface. Useful if you don't use one of the heater channels.


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

---

## Using PowerShell for Direct Serial Communication

You can send commands directly to the controller via PowerShell without the proxy. Replace `COM9` with your actual COM port.

**Generic Template:**
```powershell
$port = New-Object System.IO.Ports.SerialPort "COM9", 115200
$port.Open()
$port.WriteLine('{"get": "sensors"}')  # Your command here
Start-Sleep -Milliseconds 200
$port.ReadExisting()
$port.Close()
```

**Example: Read Sensor Data**
```powershell
$port.WriteLine('{"get": "sensors"}')
# Response: {"v":12.8,"i":802,"p":10.3,"t_amb":18.5,"h_amb":65,...}
```

**Example: Turn DC1 On**
```powershell
$port.WriteLine('{"set": {"d1": true}}')
# Response: {"status":{"d1":1,"d2":0,...}}
```

**Example: Set Heater 1 to Manual 50%**
```powershell
$port.WriteLine('{"sc": {"dh": [{"m": 0, "mp": 50}]}}')
# Response: Full config JSON
```

> [!TIP]
> For interactive testing, use a serial terminal like **PuTTY** (115200 baud) or the **Arduino IDE Serial Monitor**.
