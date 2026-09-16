# REST API & Automation

[← Back to proxy overview](./ASCOM_PROXY.md)

This guide covers advanced usage for power users who want to control the SV241 via command line, scripts, or custom integrations.

## Custom ASCOM Actions

To provide functionality beyond the standard ASCOM `Switch` specification, the driver implements several custom **Actions**. These can be triggered by ASCOM client software that supports them, or manually via API calls.

### Master Switch Actions (Switch Device)

These actions provide a "Master Switch" to control all power outputs simultaneously.

*   `MasterSwitchOn`: Turns all power outputs on.
*   `MasterSwitchOff`: Turns all power outputs off.

### Sensor Actions (ObservingConditions Device)

The `ObservingConditions` device provides an action to read the lens/objective temperature separately from the ambient temperature.

*   `getlenstemperature`: Returns the current lens/objective temperature from the DS18B20 sensor (in °C).


### Using Actions via API (e.g., with `curl`)

You can trigger these actions from the command line using a tool like `curl`. The endpoint for actions is `/api/v1/switch/0/action` and the method is `PUT`.

**Example: Turn all switches off**
```bash
curl -X PUT -d "Action=MasterSwitchOff" http://localhost:32241/api/v1/switch/0/action
```

**Example: Turn all switches on**
```bash
curl -X PUT -d "Action=MasterSwitchOn" http://localhost:32241/api/v1/switch/0/action
```

> [!NOTE]
> **For Windows PowerShell users:** The standard `curl` command in PowerShell is an alias for `Invoke-WebRequest`, which has a different syntax and requires the `Content-Type` to be set explicitly. Here are the correct PowerShell commands:
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

## Reading Sensor Values (Sensor Switches)

The power metrics and environmental sensors are exposed as read-only ASCOM Switch devices. These can be used to display values in NINA gauges or any ASCOM client that supports analog switch values.

**Input Voltage (ID 0), Total Current (ID 1), and Total Power (ID 2) are always at those
fixed IDs.** The rest of the sensor slots are conditional on your dew heater
configuration, so their IDs - and where the power switches start right after them - vary
by configuration:

| Slot (after ID 2) | Name | Unit | Present when... |
|----|------|------|-------------|
| next free ID | Lens Temperature | °C | Any heater is in PID or Minimum Temperature mode, or "Persistent Lens Temp" is enabled in the Proxy Tab |
| next free ID | Heater 1 Output | % | Heater 1's mode isn't Disabled |
| next free ID | Heater 2 Output | % | Heater 2's mode isn't Disabled |

So power switches (`dc1` onward) can start anywhere from **ID 3** (both heaters Disabled,
Lens Temp not forced on) to **ID 6** (Lens Temp shown and both heaters enabled). A fresh
install with both heaters left in the default Manual mode puts DC1 at **ID 5** (Lens Temp
hidden, both Heater Output sensors present).

> [!TIP]
> Don't hardcode a switch's ID in a script - it depends on your current configuration.
> Read `GET /api/v1/switch/0/maxswitch` for the current switch count, or
> `GET /api/v1/switch/0/getswitchname?Id=X` to look up what a given ID currently points to.

> [!NOTE]
> **PWM Dual-Exposure:** Dew heaters are exposed twice—once as a read-only sensor (showing
> current power %, at whichever ID applies per the table above) and once as a toggle at the
> end of the switch list (allowing manual override).

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

> [!TIP]
> **For localized Windows:** `Invoke-RestMethod` parses JSON and displays numbers using your locale (e.g., `12,8` in German). To get the raw JSON with standard decimal format, use `Invoke-WebRequest` and access the `.Content` property:
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

## Controlling Individual Switches via REST API

Beyond the custom actions, you can directly control individual switches using the standard ASCOM Alpaca `Switch` endpoints.

> [!IMPORTANT]
> **Switch ID Schema:** Voltage/Current/Power always occupy IDs 0-2. Lens Temperature and
> the two Heater Output sensors conditionally occupy the next 0-3 slots depending on your
> dew heater configuration (see [Reading Sensor
> Values](#reading-sensor-values-sensor-switches) above) - power switches start right after
> those. Disabling a power switch removes it from the ASCOM device list, causing subsequent
> power switch IDs to shift down; changing a heater's mode can also shift where power
> switches start, since it changes how many sensor slots exist.

**Endpoints:**
- `PUT /api/v1/switch/0/setswitch` – Set a switch on or off (parameters: `Id`, `State`)
- `PUT /api/v1/switch/0/setswitchvalue` – Set a switch value (parameters: `Id`, `Value`) – used for adjustable voltage (0-15V)
- `GET /api/v1/switch/0/getswitch?Id=X` – Get the current state of a switch (on/off)
- `GET /api/v1/switch/0/getswitchvalue?Id=X` – Get the current value of a switch (e.g., voltage for adj_conv)

**Examples using native `curl` (Linux/Mac/Git Bash):**

```bash
# Replace ID 3 with the actual ID of dc1 in your configuration (see Switch ID Schema note above)
# Turn switch dc1 ON
curl -X PUT -d "Id=3&State=true" http://localhost:32241/api/v1/switch/0/setswitch

# Turn switch dc1 OFF
curl -X PUT -d "Id=3&State=false" http://localhost:32241/api/v1/switch/0/setswitch

# Get the current state of switch dc1
curl "http://localhost:32241/api/v1/switch/0/getswitch?Id=3"

# Set adjustable converter to 9.5V (requires EnableAlpacaVoltageControl in proxy config)
# Replace ID 10 with the actual ID of adj_conv in your configuration
curl -X PUT -d "Id=10&Value=9.5" http://localhost:32241/api/v1/switch/0/setswitchvalue

# Get the current voltage of the adjustable converter
curl "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=10"
```

**Examples using Windows PowerShell:**

```powershell
# Replace ID 3 with the actual ID of dc1 in your configuration (see Switch ID Schema note above)
# Turn switch dc1 ON
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitch" -Method PUT -Body "Id=3&State=true" -ContentType "application/x-www-form-urlencoded"

# Turn switch dc1 OFF
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitch" -Method PUT -Body "Id=3&State=false" -ContentType "application/x-www-form-urlencoded"

# Get the current state of switch dc1
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitch?Id=3"

# Set adjustable converter to 9.5V (requires EnableAlpacaVoltageControl in proxy config)
# Replace ID 10 with the actual ID of adj_conv in your configuration
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/switch/0/setswitchvalue" -Method PUT -Body "Id=10&Value=9.5" -ContentType "application/x-www-form-urlencoded"

# Get the current voltage of the adjustable converter
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/switch/0/getswitchvalue?Id=10"
```

## Safety Monitor

When enabled (Configuration → Safety Monitor tab, "Expose ASCOM Alpaca SafetyMonitor device"),
the proxy exposes a standard ASCOM `SafetyMonitor` device that reports "unsafe" once any one of
your configured conditions is true - e.g. voltage at or below a threshold, current above a
threshold, ambient temperature or humidity out of range. Conditions are OR'd together: any single
one tripping is enough. Useful for a battery-powered rig in the field, so a sequencer (e.g.
N.I.N.A.) can abort a running sequence in an orderly way. If the feature is disabled, `IsSafe`
always returns `true` and the device doesn't appear in `management/v1/configureddevices` at all.

**Endpoint:** `GET /api/v1/safetymonitor/0/issafe`

```bash
curl "http://localhost:32241/api/v1/safetymonitor/0/issafe?ClientID=1&ClientTransactionID=1"
```

**Windows PowerShell:**
```powershell
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/safetymonitor/0/issafe?ClientID=1&ClientTransactionID=1"
```

**Example response:**
```json
{
  "Value": false,
  "ClientTransactionID": 1,
  "ServerTransactionID": 42,
  "ErrorNumber": 0,
  "ErrorMessage": ""
}
```

## Configuration Profiles

Named, manually-saved snapshots of one box's configuration (firmware config + that box's own
proxy-side settings - rig name, switch names, etc.), with no limit on how many you can keep.
See also the [Profiles Tab](./WEB_INTERFACE.md#profiles-tab) in the web UI.

**Endpoints:**
- `GET /api/v1/profiles/list` - List all saved profiles (metadata only - name, filename, saved
  timestamp, which device it was saved from, and whether that matches the currently connected
  device).
- `POST /api/v1/profiles/save` - Capture the current live configuration under a new profile.
  Body: `{"name": "My Profile"}`.
- `POST /api/v1/profiles/apply?file=<filename>` - Push a saved profile's settings live (a live
  config merge, same as any normal settings change - no reboot or reconnect). Add
  `&force=true` to apply a profile saved from a *different* box than the one currently
  connected (see below).
- `POST /api/v1/profiles/update?file=<filename>` - Overwrite a profile with the current live
  configuration, keeping its name and filename.
- `POST /api/v1/profiles/delete?file=<filename>` - Permanently delete a profile.

`filename` is the value returned by `list`/`save`/`update` (e.g.
`sv241_profile_20250115_143022.json`) - never construct it yourself.

> [!NOTE]
> **Device Mismatch Protection:** Like [Backup & Restore](./WEB_INTERFACE.md#system-tab),
> applying a profile saved from a box other than the one currently connected is blocked by
> default with an HTTP `409` response (`{"error":"device_mismatch","backupDeviceSerial":...,
> "backupRigName":...,"currentDeviceSerial":...,"currentRigName":...}`). Retry the same request
> with `&force=true` to apply it anyway (e.g. deliberately replacing one box with another).

**Examples using native `curl` (Linux/Mac/Git Bash):**

```bash
# List saved profiles
curl "http://localhost:32241/api/v1/profiles/list"

# Save the current configuration as a new profile
curl -X POST -H "Content-Type: application/json" -d '{"name":"Widefield Setup"}' \
  http://localhost:32241/api/v1/profiles/save

# Apply a saved profile (filename from the list/save response above)
curl -X POST "http://localhost:32241/api/v1/profiles/apply?file=sv241_profile_20250115_143022.json"

# Update a profile with the current configuration
curl -X POST "http://localhost:32241/api/v1/profiles/update?file=sv241_profile_20250115_143022.json"

# Delete a profile
curl -X POST "http://localhost:32241/api/v1/profiles/delete?file=sv241_profile_20250115_143022.json"
```

**Examples using Windows PowerShell:**

```powershell
# List saved profiles
Invoke-RestMethod -Uri "http://localhost:32241/api/v1/profiles/list"

# Save the current configuration as a new profile
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/profiles/save" -Method POST -Body '{"name":"Widefield Setup"}' -ContentType "application/json"

# Apply a saved profile (filename from the list/save response above)
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/profiles/apply?file=sv241_profile_20250115_143022.json" -Method POST

# Update a profile with the current configuration
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/profiles/update?file=sv241_profile_20250115_143022.json" -Method POST

# Delete a profile
Invoke-WebRequest -Uri "http://localhost:32241/api/v1/profiles/delete?file=sv241_profile_20250115_143022.json" -Method POST
```
