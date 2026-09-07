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
  float ds18b20_temperature;

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

// Triggers the SHT40 internal heater to dry the sensor.
void dry_sht40_sensor();

#endif // SENSORS_H
