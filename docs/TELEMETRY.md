# Telemetry & Logging

[← Back to proxy overview](./ASCOM_PROXY.md)

The proxy driver includes a robust telemetry system that logs sensor data to a local SQLite database and provides interactive visualization with CSV export.

## Automatic Database Logging
*   **Storage:** Telemetry data is stored in a local SQLite database (`alpaca_proxy.db`) in the configuration directory. Each recorded point is tagged with the physical box that logged it, so history stays correctly attributed even if you swap boxes on the same computer.
*   **Frequency:** Configurable logging interval from 1-10 seconds, or disabled entirely (0 seconds). Default is 10 seconds.
*   **Data Points:** Logs all sensor values including voltage, current, power, temperatures, humidity, dew point, switch states, and heater PWM levels.
*   **Rotation:** Uses a "Noon-to-Noon" rotation strategy. A single imaging night is contained in one session, even if it spans midnight.
*   **Retention:** Old data is automatically pruned based on the configured number of nights to retain (default: 10).

## Data Explorer
The web interface features a built-in **Data Explorer** for interactive telemetry visualization:

*   **Access:** Click the 📊 button in the [Live Telemetry panel](./WEB_INTERFACE.md#live-telemetry-panel) to open the Data Explorer. This button is only visible when telemetry logging is enabled.
*   **Time Range:** Choose from presets (1h, 12h, 24h, 7d) or select a custom date/time range.
*   **Multi-Sensor Charts:** Select multiple sensors to display on the same chart for comparison.
*   **Interactive Navigation:** Zoom and pan through the data using mouse wheel and drag.
*   **Reset View:** Click "🔄 Reset View" to return to the full time range after zooming.
*   **Custom Names:** Sensors display your custom switch names (e.g., "DC 1 (Telescope Mount)").
*   **Disabled Filtering:** Switches and heaters marked as "Disabled" are automatically hidden from the sensor list.
*   **Rig Filter:** If the proxy has ever seen more than one box, a "Rig" dropdown lets you show just one box's history at a time (labeled by Rig Name where set); switch names in the chart legend then resolve from that box's own profile rather than whichever box is currently connected.

## CSV Export
Export telemetry data for external analysis:

*   **Download:** Click "Download Selection CSV" in the Data Explorer to export only the selected sensors.
*   **Headers:** CSV headers include custom names in the format `key (custom_name)` for easy identification. Each row also includes a **Device** column identifying which box recorded it (by Rig Name, falling back to the raw serial, or "Unknown" for rows logged before this tracking existed).
*   **Time Format:** Timestamps are exported in ISO 8601 format (RFC3339).

## External API Access
The telemetry system exposes a REST API that allows you to fetch historical data from any device in your network.

**Endpoint:** `GET /api/v1/telemetry/history?start={timestamp}&end={timestamp}`

**Features:**
*   **Universal Access:** Fetch data from Excel, PowerBI, Python scripts, Home Assistant, or Grafana.
*   **Network Configuration:** By default, the proxy listens on `127.0.0.1` (localhost). To access the API from other devices (e.g., a phone or laptop), you must change the `ListenAddress` in the proxy settings to `0.0.0.0` (see [Important Security Notice](./ASCOM_PROXY.md#important-security-notice) in the proxy overview).

**Example Scenarios:**
*   **PowerBI / Excel:** Import live data using PowerQuery: `http://192.168.1.100:32241/api/v1/telemetry/download?date=2023-10-27`
*   **Home Assistant:** Create REST sensors to poll the JSON history for custom dashboards.
*   **Python:** Automate data analysis with simple HTTP requests.

## Configuration
Telemetry settings are available in the **Proxy Settings** tab under "Logging & Telemetry":

| Setting | Description |
|---------|-------------|
| **Telemetry Interval** | How often to log data (Disabled, 1-10 seconds) |
| **Min. Retention (Nights)** | Minimum number of recorded nights to keep before pruning |
