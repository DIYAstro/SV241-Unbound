# Configuration Reference

[← Back to proxy overview](./ASCOM_PROXY.md)

The proxy creates its configuration files in the following directory on Windows:
*   **Windows:** `%APPDATA%\SV241AlpacaProxy\`

## Manual Configuration (`proxy_config.json`)

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
  "enableAlpacaDiscovery": true,
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
  },
  "deviceProfiles": {
    "AA:BB:CC:11:22:33": {
      "rigName": "Imaging Rig",
      "switchNames": {
        "dc1": "Camera",
        "dc2": "Mount"
      },
      "lensTempName": "Box Ambient Temp",
      "heaterAutoEnableLeader": { "pwm1": true, "pwm2": true },
      "weatherSourcePriority": { "temperature": "hybrid" }
    }
  },
  "alwaysShowLensTemp": true,
  "lensTempName": "Box Ambient Temp",
  "enableWeatherService": true,
  "weatherLatitude": 52.52,
  "weatherLongitude": 13.40,
  "weatherModel": "best_match",
  "weatherInterval": 5,
  "weatherSourcePriority": {
    "temperature": "hybrid",
    "cloudcover": "internet"
  }
}
```

**Parameter Explanation:**

*   `serialPortName` (string): The name of the serial port for the SV241 device (e.g., `"COM9"`). If this string is empty (`""`), the proxy will attempt to auto-detect the port on startup.
    > [!NOTE]
    > When `Auto-Detect Port` is enabled (or `serialPortName` is empty), the proxy probes all available USB serial ports to find the SV241. This "safe-but-aggressive" probing can potentially interfere with other sensitive devices (e.g., Mounts, Weather Stations). **Solution:** To prevent conflicts, connect the SV241 once to let it auto-detect, then **disable "Auto-Detect Port"** (or uncheck the box in the web UI) **and ensure a port name is configured**. The proxy will then strictly only open the configured port.

    > [!IMPORTANT]
    > If you disable `Auto-Detect Port` but leave `serialPortName` empty, the proxy will still fall back to auto-detection. Both settings must be configured together: disable auto-detect AND specify the port name.
*   `autoDetectPort` (boolean): When `true`, the proxy will attempt to find the SV241 automatically if the configured port fails. When `false` **and** a `serialPortName` is specified, the proxy will only try the configured port. Default is `true`.
*   `networkPort` (integer): The TCP port on which the Alpaca API server will listen for connections from client applications. The default is `32241`. A restart of the proxy is required for changes to this value to take effect.
*   `listenAddress` (string): The IP address to bind the server to. Use `"127.0.0.1"` for local-only access (recommended for security) or `"0.0.0.0"` to allow network access. Default is `"127.0.0.1"`.
*   `logLevel` (string): Controls the verbosity of the log file. Valid values are `"ERROR"`, `"WARN"`, `"INFO"`, and `"DEBUG"`. This setting is applied live when changed.
*   `historyRetentionNights` (integer): The number of days/nights of telemetry history to retain in the SQLite database (`alpaca_proxy.db`). Older rows are automatically pruned at startup. Default is `10`.
*   `telemetryInterval` (integer): The interval in seconds between telemetry log entries. Default is `10`.
*   `enableAlpacaVoltageControl` (boolean): When `true`, the adjustable voltage output can be controlled as a slider (0-15V) via ASCOM. When `false`, it behaves as a simple on/off switch. Default is `false`.
    > [!CAUTION]
    > If this setting is `false` (Switch Mode), ensure that the Adjustable Output has a pre-configured voltage > 0V (e.g., set via Web Interface or Startup Config). If the port is at 0V, switching it "ON" via ASCOM will technically succeed but remain at 0V, potentially causing ASCOM clients to time out or report failure because they don't see a voltage increase.
*   `enableAlpacaDiscovery` (boolean): When `true`, the proxy responds to Alpaca discovery packets on UDP port 32227. This allows astronomy software like NINA to find the device automatically. If you have other Alpaca servers on the same PC, you may need to disable this to avoid port conflicts. Default is `true`.
*   `enableMasterPower` (boolean): When `true`, a "Master Power" switch is exposed via ASCOM that controls all outputs simultaneously. Default is `false`.
*   `switchNames` (object): A map that allows you to assign custom, user-friendly names to the internal switch identifiers. The `key` is the internal name (e.g., `"dc1"`) and the `value` is the custom name you want to see in ASCOM clients and the web interface.
*   `heaterAutoEnableLeader` (object): Controls automatic leader activation for PID-Sync mode. When a follower heater (in mode 3) is enabled, the proxy can automatically enable its leader heater. Keys are `"pwm1"` and `"pwm2"`, values are `true`/`false`.
*   `deviceProfiles` (object): The actual per-box storage for `switchNames`, `lensTempName`, `heaterAutoEnableLeader`, and `weatherSourcePriority` - keyed by the MAC address of each SV241 box the proxy has ever seen, each also carrying its own `rigName` label. The top-level `switchNames`/`lensTempName`/`heaterAutoEnableLeader`/`weatherSourcePriority` fields above are kept as a live mirror of whichever box is *currently* connected - reading or writing them (via the API, the config file, or the web UI) always reflects/updates that active box's own entry here. The very first box a proxy install ever sees inherits whatever those top-level fields already held (the upgrade path for existing single-box setups); any box after that starts with clean default names rather than inheriting an unrelated box's. You normally don't need to hand-edit this - the web UI's Rig Name field (System Tab) and the Switches/Sensors tabs manage it for you.
*   `alwaysShowLensTemp` (boolean): When `true`, the "Lens Temperature" sensor switch is always exposed to ASCOM, even if the heater modes that require it (PID/MinTemp) are disabled. Handy for monitoring the sensor value (reading) in Manual Mode. Default is `false`.
*   `lensTempName` (string): Allows you to override the default name "Lens Temperature" with a custom name (e.g., "Ambient Box Temp"). If empty, the default name is used.


## Log Level Configuration

The proxy driver provides a configurable logging level to control the amount of detail written to the log file (`proxy.log` in the configuration directory). This is useful for both normal operation and detailed troubleshooting. The log level can be set in the **"Proxy Connection Settings"** section of the web setup page.

The available levels are:

*   **ERROR**: Logs only critical errors that prevent the proxy from working correctly (e.g., failure to open a serial port, server start failure).
*   **WARN**: Logs warnings about non-critical issues that the proxy can recover from (e.g., a temporary connection loss, auto-detection failures).
*   **INFO** (Default): Logs major events during normal operation, such as application start/stop, successful connections, and configuration changes. This level provides a good overview without being too verbose.
*   **DEBUG**: Logs highly detailed information, including every incoming HTTP request, every command sent to the device, and periodic status checks. This level is extremely useful for diagnosing communication problems with ASCOM client software but will create very large log files.

## Log Rotation

To prevent the log file from growing indefinitely, the proxy performs a simple rotation upon every start:
1.  The existing `proxy.log` is renamed to `proxy.log.old`, overwriting any previous `.old` file.
2.  A new, empty `proxy.log` is created for the current session.

This ensures that the logs from the current and the immediately preceding session are always available for troubleshooting.
