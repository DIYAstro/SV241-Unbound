# Hardware SV241 Pro

[← Back to main readme](../readme.md)

Reference for the Svbony SV241 Pro's microcontroller and pin mapping - useful if you're
digging into the firmware source or debugging at the hardware level.

## Microcontroller Specifications
*   **Model:** ESP32-PICO-D4 (Espressif Systems)
*   **Architecture:** Xtensa LX6 Dual-Core up to 240 MHz
*   **Flash Memory:** 4 MB Embedded
*   **Connectivity:** Wi-Fi / Bluetooth (Hardware capability only; software uses USB-only communication)

## GPIO Pinout Mapping
| GPIO | Function | Description |
|------|----------|--------------|
| 12 | DC2 | 12V Switched Output |
| 13 | DC1 | 12V Switched Output |
| 14 | DC3 | 12V Switched Output |
| 18 | USB 3/4/5 | USB-A Ports |
| 19 | USB-C 1/2 | USB-C Ports |
| 21 | I2C SDA | Data Line (Sensors) |
| 22 | I2C SCL | Clock Line (Sensors) |
| 23 | OneWire | DS18B20 Lens Sensor |
| 25 | Adj. Voltage | PWM for Southchip SC8903 (Adjustable Output) |
| 26 | DC5 | 12V Switched Output |
| 27 | DC4 | 12V Switched Output |
| 32 | PWM2 | Dew Heater 2 |
| 33 | PWM1 | Dew Heater 1 |
