#ifndef DIAGNOSTICS_H
#define DIAGNOSTICS_H

#include <ArduinoJson.h>

// Starts a small background task that samples FreeRTOS run-time stats every 10 seconds. Call once
// from setup(), after the application tasks have been created.
void diagnostics_start();

// Adds the latest diagnostics to the sensors response (see get_sensor_values_json()):
//   "c0", "c1" - how busy core 0 / core 1 were over the last window, in 0.1 % units (core 1 idle
//                share is what the DS18B20 busy-wait used to eat - see sensors.cpp)
//   "sk"       - smallest stack headroom ever seen across the application tasks, in bytes
//   "up"       - uptime in seconds since the ESP32 booted (a drop means the controller restarted)
//   "rr"       - esp_reset_reason() of the current boot (1 power-on, 3 software, 4 panic,
//                5/6/7 watchdog, 9 brownout)
// "c0", "c1" and "sk" appear once the first window has been sampled. All values are plain
// integers - "up" in particular must never become a float (a float32 loses integer precision
// beyond 2^24 seconds, ~194 days).
void diagnostics_add_to_json(JsonDocument& doc);

#endif
