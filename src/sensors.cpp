#include <Arduino.h>
#include <Wire.h>
#include <Adafruit_INA219.h>
#include <Adafruit_SHT4x.h>
#include <OneWire.h>
#include <DallasTemperature.h>
#include <ArduinoJson.h>
#include <math.h>

#include "config_manager.h"
#include "hardware_pins.h"
#include "sensors.h"
#include "dew_control.h"

// --- INA219 Constants ---
const float SHUNT_RESISTANCE_OHMS = 0.005; // The value of the shunt resistor (R005)
const uint16_t INA219_CALIB_VALUE = 20480;  // Pre-calculated calibration value for 32V, 10A, 0.005 Ohm shunt

// --- Constants ---
// Define a maximum size for all averaging buffers. This must be larger than any value in config.averaging_counts.
const int MAX_SENSOR_AVG_COUNT = 20;

// --- The global sensor value cache ---
SensorValues sensor_cache;
// --- Mutex definition ---
SemaphoreHandle_t sensor_cache_mutex;

// --- Global sensor availability flags ---
bool is_ina219_available = false;
bool is_sht40_available = false;
bool is_ds18b20_available = false;

// --- Status flag for SHT40 drying process ---
static volatile bool is_sht40_drying = false;
static unsigned long sht40_drying_start_time = 0;
const unsigned long SHT40_DRYING_COOLDOWN_MS = 45000;

// Sensor Objects
Adafruit_INA219 ina219(INA219_ADDR);
Adafruit_SHT4x sht40 = Adafruit_SHT4x();
OneWire oneWire(ONE_WIRE_BUS);
DallasTemperature dallas_sensors(&oneWire);

// --- Last update timestamps ---
static unsigned long last_ina219_update = 0;
static unsigned long last_sht40_update = 0;
static unsigned long last_ds18b20_update = 0;

// --- Auto-dry state ---
static unsigned long high_humidity_start_time = 0; // 0 means timer is not running

// --- Filter buffers and indices ---
// Arrays are now sized to a fixed maximum. The actual count used is from the config.
static float ina219_voltage_readings[MAX_SENSOR_AVG_COUNT];
static int ina219_voltage_index = 0;
static int ina219_voltage_readings_count = 0;

static float ina219_current_readings[MAX_SENSOR_AVG_COUNT];
static int ina219_current_index = 0;
static int ina219_current_readings_count = 0;

static float sht40_temp_readings[MAX_SENSOR_AVG_COUNT];
static int sht40_temp_index = 0;
static int sht40_readings_count = 0;

static float sht40_humidity_readings[MAX_SENSOR_AVG_COUNT];
static int sht40_humidity_index = 0;
static int sht40_humidity_readings_count = 0;

static float ds18b20_temp_readings[MAX_SENSOR_AVG_COUNT];
static int ds18b20_temp_index = 0;
static int ds18b20_readings_count = 0;


// Helper function to calculate the median of an array
static float calculate_median(float arr[], int count) {
  if (count == 0) return 0;
  if (count > MAX_SENSOR_AVG_COUNT) count = MAX_SENSOR_AVG_COUNT; // Safety clamp
  
  // Use static array instead of VLA to avoid stack overflow risk
  static float sorted_arr[MAX_SENSOR_AVG_COUNT];
  memcpy(sorted_arr, arr, sizeof(float) * count);
  
  // This bubble sort is inefficient, but acceptable for small N (max 20).
  for (int i = 0; i < count - 1; i++) {
    for (int j = 0; j < count - i - 1; j++) {
      if (sorted_arr[j] > sorted_arr[j + 1]) {
        float temp = sorted_arr[j];
        sorted_arr[j] = sorted_arr[j + 1];
        sorted_arr[j + 1] = temp;
      }
    }
  }
  if (count % 2 == 0) {
    return (sorted_arr[count / 2 - 1] + sorted_arr[count / 2]) / 2.0;
  } else {
    return sorted_arr[count / 2];
  }
}

void setup_sensors() {
  // Initialize all sensor values to NAN to indicate they are not yet valid
  sensor_cache.ina_voltage = NAN;
  sensor_cache.ina_current = NAN;
  sensor_cache.ina_power = NAN;
  sensor_cache.sht_temperature = NAN;
  sensor_cache.sht_humidity = NAN;
  sensor_cache.sht_dew_point = NAN;
  sensor_cache.ds18b20_temperature = NAN;

  Wire.begin(I2C_SDA, I2C_SCL);

  is_ina219_available = ina219.begin();
  if (is_ina219_available) {
    // The Adafruit library's begin() function calls setCalibration_32V_2A(), which assumes a 0.1 Ohm shunt.
    // We must overwrite this with our custom calibration for the 0.005 Ohm shunt.
    uint16_t config_value = INA219_CONFIG_BVOLTAGERANGE_32V |
                      INA219_CONFIG_GAIN_8_320MV | INA219_CONFIG_BADCRES_12BIT |
                      INA219_CONFIG_SADCRES_12BIT_1S_532US |
                      INA219_CONFIG_MODE_SANDBVOLT_CONTINUOUS;

    // Write CONFIG register (separate I2C transaction per register)
    Wire.beginTransmission(INA219_ADDR);
    Wire.write(INA219_REG_CONFIG);
    Wire.write((config_value >> 8) & 0xFF);
    Wire.write(config_value & 0xFF);
    Wire.endTransmission();

    // Write CALIBRATION register (separate I2C transaction)
    Wire.beginTransmission(INA219_ADDR);
    Wire.write(INA219_REG_CALIBRATION);
    Wire.write((INA219_CALIB_VALUE >> 8) & 0xFF);
    Wire.write(INA219_CALIB_VALUE & 0xFF);
    Wire.endTransmission();
  } else {
    Serial.println("{\"error\":\"INA219 sensor not found\"}");
  }

  is_sht40_available = sht40.begin();
  if (is_sht40_available) {
    sht40.setPrecision(SHT4X_HIGH_PRECISION);
    sht40.setHeater(SHT4X_NO_HEATER);
  } else {
    Serial.println("{\"error\":\"SHT40 sensor not found\"}");
  }

  dallas_sensors.begin();
  is_ds18b20_available = (dallas_sensors.getDeviceCount() > 0);
  if (!is_ds18b20_available) {
    Serial.println("{\"error\":\"DS18B20 sensor not found\"}");
  }
}

void update_sensor_cache() {
  unsigned long current_millis = millis();

  // Create a thread-safe local copy of config values used in this function
  unsigned long ina219_interval, sht40_interval, ds18b20_interval;
  AveragingCounts avg_counts;
  SensorOffsets offsets;
  Sht40AutoDryConfig auto_dry_config;
  xSemaphoreTake(config_mutex, portMAX_DELAY);
  ina219_interval = config.update_intervals_ms.ina219;
  sht40_interval = config.update_intervals_ms.sht40;
  ds18b20_interval = config.update_intervals_ms.ds18b20;
  avg_counts = config.averaging_counts;
  offsets = config.sensor_offsets;
  auto_dry_config = config.sht40_auto_dry;
  xSemaphoreGive(config_mutex);

  // --- INA219 Update ---
  if (is_ina219_available && (current_millis - last_ina219_update >= ina219_interval)) {
    last_ina219_update = current_millis;
    float raw_bus_voltage = ina219.getBusVoltage_V();
    
    // Since we overwrote the calibration, ina219.getCurrent_mA() is incorrect.
    // We calculate the current manually using Ohm's law: I = V_shunt / R_shunt.
    float shunt_voltage_mV = ina219.getShuntVoltage_mV();
    float raw_current_mA = shunt_voltage_mV / SHUNT_RESISTANCE_OHMS;
    float final_bus_voltage = raw_bus_voltage;
    float final_current_mA = raw_current_mA;

    // Averaging for Voltage
    int avg_count_v = avg_counts.ina219_voltage;
    if (avg_count_v > 1 && avg_count_v <= MAX_SENSOR_AVG_COUNT) {
        ina219_voltage_readings[ina219_voltage_index] = raw_bus_voltage;
        ina219_voltage_index = (ina219_voltage_index + 1) % avg_count_v;
        if (ina219_voltage_readings_count < avg_count_v) { ina219_voltage_readings_count++; }
        final_bus_voltage = calculate_median(ina219_voltage_readings, ina219_voltage_readings_count);
    }

    // Averaging for Current
    int avg_count_c = avg_counts.ina219_current;
    if (avg_count_c > 1 && avg_count_c <= MAX_SENSOR_AVG_COUNT) {
        ina219_current_readings[ina219_current_index] = raw_current_mA;
        ina219_current_index = (ina219_current_index + 1) % avg_count_c;
        if (ina219_current_readings_count < avg_count_c) { ina219_current_readings_count++; }
        final_current_mA = calculate_median(ina219_current_readings, ina219_current_readings_count);
    }

    if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
      sensor_cache.ina_voltage = final_bus_voltage + offsets.ina219_voltage;
      sensor_cache.ina_current = final_current_mA + offsets.ina219_current;
      sensor_cache.ina_power = sensor_cache.ina_voltage * sensor_cache.ina_current / 1000.0;
      xSemaphoreGive(sensor_cache_mutex);
    }
  }

  // --- SHT40 Update ---
  if (is_sht40_drying) {
      if (millis() - sht40_drying_start_time >= SHT40_DRYING_COOLDOWN_MS) {
          is_sht40_drying = false;
           if(xSemaphoreTake(serial_mutex, (TickType_t)10) == pdTRUE) {
                Serial.println("{\"status\":\"SHT40 drying cycle complete\"}");
                xSemaphoreGive(serial_mutex);
            }
      }
  }

  if (is_sht40_available && !is_sht40_drying && (current_millis - last_sht40_update >= sht40_interval)) {
    last_sht40_update = current_millis;
    sensors_event_t humidity, temp;
    if (sht40.getEvent(&humidity, &temp)) {
      float final_sht40_temp = temp.temperature;
      float final_sht40_humidity = humidity.relative_humidity;

      // Averaging for Temperature
      int avg_count_t = avg_counts.sht40_temp;
      if (avg_count_t > 1 && avg_count_t <= MAX_SENSOR_AVG_COUNT) {
          sht40_temp_readings[sht40_temp_index] = temp.temperature;
          sht40_temp_index = (sht40_temp_index + 1) % avg_count_t;
          if (sht40_readings_count < avg_count_t) { sht40_readings_count++; }
          final_sht40_temp = calculate_median(sht40_temp_readings, sht40_readings_count);
      }

      // Averaging for Humidity
      int avg_count_h = avg_counts.sht40_humidity;
      if (avg_count_h > 1 && avg_count_h <= MAX_SENSOR_AVG_COUNT) {
          sht40_humidity_readings[sht40_humidity_index] = humidity.relative_humidity;
          sht40_humidity_index = (sht40_humidity_index + 1) % avg_count_h;
          if (sht40_humidity_readings_count < avg_count_h) { sht40_humidity_readings_count++; }
          final_sht40_humidity = calculate_median(sht40_humidity_readings, sht40_humidity_readings_count);
      }

      // --- Auto-Dry Logic ---
      if (auto_dry_config.enabled) {
          if (final_sht40_humidity >= auto_dry_config.humidity_threshold) {
              if (high_humidity_start_time == 0) {
                  high_humidity_start_time = current_millis;
              } else {
                  if (current_millis - high_humidity_start_time >= auto_dry_config.trigger_duration_ms) {
                      dry_sht40_sensor();
                      high_humidity_start_time = 0;
                  }
              }
          } else {
              high_humidity_start_time = 0;
          }
      }

      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.sht_temperature = final_sht40_temp + offsets.sht40_temp;
        sensor_cache.sht_humidity = final_sht40_humidity + offsets.sht40_humidity;

        // Magnus formula for dew point calculation
        float temp_calc = sensor_cache.sht_temperature;
        float hum_calc = sensor_cache.sht_humidity;
        if (hum_calc > 0) { // Avoid log(0) which is -inf
          float gamma = log(hum_calc / 100.0) + (17.62 * temp_calc) / (243.12 + temp_calc);
          sensor_cache.sht_dew_point = (243.12 * gamma) / (17.62 - gamma);
        } else {
          sensor_cache.sht_dew_point = NAN;
        }

        xSemaphoreGive(sensor_cache_mutex);
      }
    } else {
      // Sensor read failed, assume it's disconnected
      is_sht40_available = false;
      Serial.println("{\"error\":\"SHT40 sensor disconnected\"}");
      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.sht_temperature = NAN;
        sensor_cache.sht_humidity = NAN;
        sensor_cache.sht_dew_point = NAN;
        xSemaphoreGive(sensor_cache_mutex);
      }
    }
  }

  // --- DS18B20 Update ---
  if (is_ds18b20_available && (current_millis - last_ds18b20_update >= ds18b20_interval)) {
    last_ds18b20_update = current_millis;
    dallas_sensors.requestTemperatures(); 
    float tempC = dallas_sensors.getTempCByIndex(0);
    if(tempC != DEVICE_DISCONNECTED_C) {
      float final_ds18b20_temp = tempC;

      // Averaging for Temperature
      int avg_count_t = avg_counts.ds18b20_temp;
      if (avg_count_t > 1 && avg_count_t <= MAX_SENSOR_AVG_COUNT) {
          ds18b20_temp_readings[ds18b20_temp_index] = tempC;
          ds18b20_temp_index = (ds18b20_temp_index + 1) % avg_count_t;
          if (ds18b20_readings_count < avg_count_t) { ds18b20_readings_count++; }
          final_ds18b20_temp = calculate_median(ds18b20_temp_readings, ds18b20_readings_count);
      }

      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.ds18b20_temperature = final_ds18b20_temp + offsets.ds18b20_temp;
        xSemaphoreGive(sensor_cache_mutex);
      }
    } else {
      // Device disconnected
      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.ds18b20_temperature = NAN;
        xSemaphoreGive(sensor_cache_mutex);
      }
    }
  }
}



void dry_sht40_sensor() {
    // 1. Set flag to pause normal SHT40 updates
    is_sht40_drying = true;
    sht40_drying_start_time = millis();

    // Optional: Log to serial that the process has started.
    if(xSemaphoreTake(serial_mutex, (TickType_t)10) == pdTRUE) {
        Serial.println("{\"status\":\"starting SHT40 drying cycle\"}");
        xSemaphoreGive(serial_mutex);
    }

    // 2. Activate the heater for one cycle (Blocking ~1s)
    sht40.setHeater(SHT4X_HIGH_HEATER_1S);
    sensors_event_t humidity, temp;
    sht40.getEvent(&humidity, &temp); 
    sht40.setHeater(SHT4X_NO_HEATER);
    
    // Return immediately to allow main loop/serial processing to continue.
    // The 'is_sht40_drying' flag prevents 'update_sensor_cache' from reading the sensor
    // until the cooldown period is over.
}

void get_sensor_values(SensorValues& values_copy) {
  if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
    values_copy = sensor_cache;
    xSemaphoreGive(sensor_cache_mutex);
  }
}

void get_sensor_values_json(JsonDocument& doc) {
  SensorValues values;
  get_sensor_values(values);

  // The ArduinoJson library automatically serializes NAN float/double values to 'null' in the JSON output.
  // By removing the isnan() checks, we achieve the desired behavior of reporting disconnected sensors as null.
  doc["v"] = round(values.ina_voltage * 10) / 10.0;
  doc["i"] = round(values.ina_current * 10) / 10.0;
  doc["p"] = round(values.ina_power * 10) / 10.0;
  doc["t_amb"] = round(values.sht_temperature * 10) / 10.0;
  doc["h_amb"] = round(values.sht_humidity * 10) / 10.0;
  doc["d"] = round(values.sht_dew_point * 10) / 10.0;
  doc["t_lens"] = round(values.ds18b20_temperature * 10) / 10.0;
  
  doc["pwm1"] = get_heater_power(0);
  doc["pwm2"] = get_heater_power(1);

  // Add memory statistics to the JSON response
  doc["hf"] = values.heap_free;
  doc["hmf"] = values.heap_min_free;
  doc["hma"] = values.heap_max_alloc;
  doc["hs"] = values.heap_size;
}