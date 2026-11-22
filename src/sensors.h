#ifndef SENSORS_H
#define SENSORS_H

#include <freertos/FreeRTOS.h>
#include <freertos/semphr.h>

// A struct to hold the latest, processed sensor values
struct SensorValues {
  float ina_voltage;
  float ina_current;
  float ina_power;
  float sht_temperature;
  float sht_humidity;
  float sht_dew_point;
  float lens_temperature[2]; // per dew heater (MAX_DEW_HEATERS), with the probe offset applied

  // Memory statistics
  uint32_t heap_free;
  uint32_t heap_min_free;
  uint32_t heap_max_alloc;
  uint32_t heap_size;
};

// Global instance of the sensor data cache
extern SensorValues sensor_cache;

// Mutex to protect access to the sensor_cache
extern SemaphoreHandle_t sensor_cache_mutex;

// Global sensor availability flags
extern bool is_ina219_available;
extern bool is_sht40_available;
extern bool is_ds18b20_available;

void setup_sensors();

// This function should be called continuously in the main loop.
// It checks timers and updates the internal sensor value cache when needed.
void update_sensor_cache();

// Returns a thread-safe copy of the internal sensor value cache.
void get_sensor_values(SensorValues& values_copy);

// Prints all current values from the cache in JSON format.
void get_sensor_values_json(JsonDocument& doc);

// Maximum number of DS18B20 probes remembered from a bus scan.
#define MAX_FOUND_PROBES 4

// Re-enumerates the 1-Wire bus (also called at startup) and resets the lens filters.
void scan_lens_probes();
// scan_lens_probes() followed by one measurement cycle (used by the "rescan_probes" command).
void rescan_lens_probes();
// {"probes":[{"id":"28FF...","t":12.4,"h":0},...]} from the cached list, no bus access.
// "t" is the raw reading (null if unreadable), "h" the assigned heater or -1.
void get_lens_probes_json(JsonDocument& doc);

// Triggers the SHT40 internal heater to dry the sensor. `automatic` is true for the auto-dry
// feature and false for the dry_sensor command: the command's reply is the "starting" line this
// function prints, so the automatic cycle prints a different one (see the definition).
void dry_sht40_sensor(bool automatic = false);

#endif // SENSORS_H
