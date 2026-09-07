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
// Define a maximum size for all averaging buffers.
const int MAX_SENSOR_AVG_COUNT = 20;

// Sensor poll intervals and median-filter window, formerly user-configurable (the "ui"/"ac"
// blocks) - removed because there's no legitimate reason to run them off these defaults, and
// getting either wrong causes real harm: too short and the median filter fills with duplicate
// readings of the same not-yet-finished conversion instead of independent samples (INA219 needs
// ~136ms for a fresh bus+shunt pair after the averaging fix below, and DS18B20's own
// requestTemperatures() blocks up to 750ms - an interval below that would leave the sensor task
// essentially permanently blocked); too long (or too wide a median window) directly delays every
// consumer of these readings - most safety-relevant now that the current-limit heater throttling
// (dew_control.cpp) reads this same cache, but the dew heater PID loops (target = dew point +
// offset) are just as exposed to a sluggish sensor feed.
const unsigned long SHT40_INTERVAL_MS = 1000;
const unsigned long DS18B20_INTERVAL_MS = 1000;
const unsigned long INA219_INTERVAL_MS = 1000;
const int SENSOR_MEDIAN_WINDOW = 5;
static_assert(SENSOR_MEDIAN_WINDOW <= MAX_SENSOR_AVG_COUNT, "SENSOR_MEDIAN_WINDOW must fit MedianFilterBuf's fixed-size buffer");

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

// A fixed-capacity ring buffer of raw readings feeding calculate_median() above - encapsulates the
// "write reading, advance index, grow count up to the configured averaging window, shrink count
// back down if that window is lowered live" bookkeeping that used to be copy-pasted once per
// sensor channel (voltage, current, SHT40 temp/humidity, DS18B20 temp). Kept as a plain struct
// with a fixed-size array member, not a pointer/dynamic allocation, so each instance below is a
// `static` global exactly like the old per-channel arrays were - same persistent-across-calls,
// no-heap-allocation, no-VLA semantics (see the "Replace VLA with static array" note on
// calculate_median above, which this must not reintroduce a variant of).
struct MedianFilterBuf {
  float readings[MAX_SENSOR_AVG_COUNT];
  int index = 0;
  int count = 0;

  // Feeds one new raw reading in and returns the resulting median. avg_count is the (now fixed)
  // averaging window, SENSOR_MEDIAN_WINDOW - never changes live, so unlike before there's no
  // longer a need to handle it shrinking mid-flight.
  float add_reading(float value, int avg_count) {
    readings[index] = value;
    index = (index + 1) % avg_count;
    if (count < avg_count) count++;
    return calculate_median(readings, count);
  }
};

// --- Filter buffers ---
// One MedianFilterBuf per averaged sensor channel - see the struct's doc comment above.
static MedianFilterBuf ina219_voltage_filter;
static MedianFilterBuf ina219_current_filter;
static MedianFilterBuf sht40_temp_filter;
static MedianFilterBuf sht40_humidity_filter;
static MedianFilterBuf ds18b20_temp_filter;

// Initializes the INA219 (calibration for the 0.005 Ohm shunt + 128-sample averaging - see the
// detailed rationale below) and returns whether it responded. Extracted out of setup_sensors()
// so attempt_i2c_bus_recovery() can re-run the exact same sequence after a bus reset, without
// duplicating it.
static bool init_ina219() {
  bool ok = ina219.begin();
  if (ok) {
    // The Adafruit library's begin() function calls setCalibration_32V_2A(), which assumes a 0.1 Ohm shunt.
    // We must overwrite this with our custom calibration for the 0.005 Ohm shunt.
    // Explicit cast on the first operand: these constants come from separate unscoped enums
    // in Adafruit_INA219.h, and combining values from different enum types via `|` is
    // deprecated as of C++20. Casting here doesn't change the resulting bit pattern.
    // 128-sample hardware averaging on BOTH channels (68.1 ms per conversion), not the
    // single-shot 532 us the shunt channel used to run: the dew heaters are PWM'd at 100 Hz
    // (10 ms period, see dew_control.cpp's PWM_FREQUENCY), so a 532 us conversion samples ~5% of
    // one PWM cycle at an effectively random phase - it reads either the full "heater on" current
    // or just the baseline "heater off" current, never the actual average. Measured on real
    // hardware at a constant 60% duty: readings alternated between ~4.7 A and ~1.0 A clusters,
    // with the true average (~2.8 A) essentially never reported. The median filter layered on top
    // (calculate_median below) makes that worse rather than better - a median picks one of the two
    // modes instead of averaging them. 68.1 ms spans 6.8 PWM periods, so the chip returns a real
    // average; only the fractional period still contributes ripple (roughly +-6% of the swing).
    // The bus voltage aliases the same way (supply sags during each "on" phase), which is why it
    // gets the same treatment - the web UI's "average volts at this duty" hint for heater-band
    // voltage limits reads that value.
    //
    // Costs nothing here: the chip free-runs in continuous mode, so a read just returns the last
    // completed conversion, and this code only reads once per second (INA219_INTERVAL_MS below).
    uint16_t config_value = (uint16_t)INA219_CONFIG_BVOLTAGERANGE_32V |
                      INA219_CONFIG_GAIN_8_320MV | INA219_CONFIG_BADCRES_12BIT_128S_69MS |
                      INA219_CONFIG_SADCRES_12BIT_128S_69MS |
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
  return ok;
}

// Initializes the SHT40 and returns whether it responded. Extracted for the same reason as
// init_ina219() above.
static bool init_sht40() {
  bool ok = sht40.begin();
  if (ok) {
    sht40.setPrecision(SHT4X_HIGH_PRECISION);
    sht40.setHeater(SHT4X_NO_HEATER);
  } else {
    Serial.println("{\"error\":\"SHT40 sensor not found\"}");
  }
  return ok;
}

// --- I2C bus recovery (INA219 and SHT40 only - both share the same physical I2C bus; DS18B20 is
// a separate OneWire bus and is unaffected by any of this) ---
//
// A hung I2C bus is a known failure mode: a slave can be left holding SDA low mid-transaction
// (e.g. a brief brownout/reset exactly while a transaction was in flight), waiting for clock
// pulses a normal transaction will never send it again. i2c_consecutive_failures is shared
// between both I2C devices - a bus-level hang affects both at once, so a failure on either one
// counts toward the same threshold, and one recovery attempt re-initializes both.
static int i2c_consecutive_failures = 0;
const int I2C_FAILURE_THRESHOLD = 3;
const unsigned long I2C_RECOVERY_COOLDOWN_MS = 5000; // avoid hammering recovery if the bus (or a
                                                       // device) is genuinely, persistently dead
static unsigned long last_i2c_recovery_attempt_ms = 0;

// Cheap address-only I2C probe, separate from the Adafruit_INA219 library's own register reads
// (which don't expose success/failure to the caller at all). Wire.endTransmission() == 0 is a
// direct, unambiguous success signal - unlike guessing from a suspiciously-low voltage reading.
static bool probe_ina219() {
  Wire.beginTransmission(INA219_ADDR);
  return Wire.endTransmission() == 0;
}

// Standard I2C bus recovery sequence: manually clock SCL (up to 9 cycles - the worst case for a
// slave stuck mid-byte-plus-ack) until the slave releases SDA, generate a manual STOP, then
// reinitialize the bus and both I2C devices. Rate-limited by I2C_RECOVERY_COOLDOWN_MS so a truly
// dead device/bus doesn't cause this to run on every single failed read.
static void attempt_i2c_bus_recovery() {
  unsigned long now = millis();
  if (now - last_i2c_recovery_attempt_ms < I2C_RECOVERY_COOLDOWN_MS) return;
  last_i2c_recovery_attempt_ms = now;

  Serial.println("{\"warn\":\"Attempting I2C bus recovery after repeated sensor read failures\"}");

  Wire.end();
  pinMode(I2C_SDA, INPUT_PULLUP);
  pinMode(I2C_SCL, OUTPUT);
  for (int i = 0; i < 9; i++) {
    digitalWrite(I2C_SCL, LOW);
    delayMicroseconds(5);
    digitalWrite(I2C_SCL, HIGH);
    delayMicroseconds(5);
    if (digitalRead(I2C_SDA) == HIGH) break; // slave released the bus
  }
  pinMode(I2C_SDA, OUTPUT);
  digitalWrite(I2C_SDA, LOW);
  delayMicroseconds(5);
  digitalWrite(I2C_SDA, HIGH); // manual STOP: release SDA while SCL is high
  delay(10);

  Wire.begin(I2C_SDA, I2C_SCL);

  // Re-run the same init sequence setup_sensors() uses - a device that was merely desynced (not
  // physically disconnected) should re-attach cleanly here.
  is_ina219_available = init_ina219();
  is_sht40_available = init_sht40();

  i2c_consecutive_failures = 0;
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

  is_ina219_available = init_ina219();
  is_sht40_available = init_sht40();

  dallas_sensors.begin();
  is_ds18b20_available = (dallas_sensors.getDeviceCount() > 0);
  if (!is_ds18b20_available) {
    Serial.println("{\"error\":\"DS18B20 sensor not found\"}");
  }
}

void update_sensor_cache() {
  unsigned long current_millis = millis();

  // Create a thread-safe local copy of config values used in this function
  SensorOffsets offsets;
  Sht40AutoDryConfig auto_dry_config;
  xSemaphoreTake(config_mutex, portMAX_DELAY);
  offsets = config.sensor_offsets;
  auto_dry_config = config.sht40_auto_dry;
  xSemaphoreGive(config_mutex);

  // --- INA219 Update ---
  if (is_ina219_available && (current_millis - last_ina219_update >= INA219_INTERVAL_MS)) {
    last_ina219_update = current_millis;

    if (!probe_ina219()) {
      i2c_consecutive_failures++;
      Serial.println("{\"warn\":\"INA219 read failed\"}");
      if (i2c_consecutive_failures >= I2C_FAILURE_THRESHOLD) {
        attempt_i2c_bus_recovery();
      }
      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.ina_voltage = NAN;
        sensor_cache.ina_current = NAN;
        sensor_cache.ina_power = NAN;
        xSemaphoreGive(sensor_cache_mutex);
      }
    } else {
      i2c_consecutive_failures = 0; // any successful I2C read, on either device, resets this
      float raw_bus_voltage = ina219.getBusVoltage_V();

      // Since we overwrote the calibration, ina219.getCurrent_mA() is incorrect.
      // We calculate the current manually using Ohm's law: I = V_shunt / R_shunt.
      float shunt_voltage_mV = ina219.getShuntVoltage_mV();
      float raw_current_mA = shunt_voltage_mV / SHUNT_RESISTANCE_OHMS;

      float final_bus_voltage = ina219_voltage_filter.add_reading(raw_bus_voltage, SENSOR_MEDIAN_WINDOW);
      float final_current_mA = ina219_current_filter.add_reading(raw_current_mA, SENSOR_MEDIAN_WINDOW);

      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.ina_voltage = final_bus_voltage + offsets.ina219_voltage;
        sensor_cache.ina_current = final_current_mA + offsets.ina219_current;
        sensor_cache.ina_power = sensor_cache.ina_voltage * sensor_cache.ina_current / 1000.0;
        xSemaphoreGive(sensor_cache_mutex);
      }
    }
  }

  // --- SHT40 Update ---
  if (is_sht40_drying) {
      if (millis() - sht40_drying_start_time >= SHT40_DRYING_COOLDOWN_MS) {
          is_sht40_drying = false;
           if(xSemaphoreTake(serial_mutex, (TickType_t)10) == pdTRUE) {
                Serial.println("{\"info\":\"SHT40 drying cycle complete\"}");
                xSemaphoreGive(serial_mutex);
            }
      }
  }

  if (is_sht40_available && !is_sht40_drying && (current_millis - last_sht40_update >= SHT40_INTERVAL_MS)) {
    last_sht40_update = current_millis;
    sensors_event_t humidity, temp;
    if (sht40.getEvent(&humidity, &temp)) {
      i2c_consecutive_failures = 0; // any successful I2C read, on either device, resets this
      float final_sht40_temp = sht40_temp_filter.add_reading(temp.temperature, SENSOR_MEDIAN_WINDOW);
      float final_sht40_humidity = sht40_humidity_filter.add_reading(humidity.relative_humidity, SENSOR_MEDIAN_WINDOW);

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
      // Read failed - retry on the next interval instead of giving up forever (that used to
      // permanently latch is_sht40_available false after a single transient failure, with no way
      // to recover short of a reboot). Same shared failure counter/recovery as INA219 above,
      // since both are on the same physical I2C bus.
      i2c_consecutive_failures++;
      Serial.println("{\"warn\":\"SHT40 read failed\"}");
      if (i2c_consecutive_failures >= I2C_FAILURE_THRESHOLD) {
        attempt_i2c_bus_recovery();
      }
      if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
        sensor_cache.sht_temperature = NAN;
        sensor_cache.sht_humidity = NAN;
        sensor_cache.sht_dew_point = NAN;
        xSemaphoreGive(sensor_cache_mutex);
      }
    }
  }

  // --- DS18B20 Update ---
  if (is_ds18b20_available && (current_millis - last_ds18b20_update >= DS18B20_INTERVAL_MS)) {
    last_ds18b20_update = current_millis;
    dallas_sensors.requestTemperatures();
    float tempC = dallas_sensors.getTempCByIndex(0);
    if(tempC != DEVICE_DISCONNECTED_C) {
      float final_ds18b20_temp = ds18b20_temp_filter.add_reading(tempC, SENSOR_MEDIAN_WINDOW);

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
        Serial.println("{\"info\":\"starting SHT40 drying cycle\"}");
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
  // Box-wide (not per-heater) current-limit ramp status - see dew_control.h. Reported here,
  // right alongside pwm1/pwm2, so it lands in the same JSON that already feeds the live UI
  // and (via serial.Conditions.Data in the proxy) the "cl" key logTelemetry() picks up.
  doc["cl"] = is_current_limit_active();

  // Add memory statistics to the JSON response
  doc["hf"] = values.heap_free;
  doc["hmf"] = values.heap_min_free;
  doc["hma"] = values.heap_max_alloc;
  doc["hs"] = values.heap_size;
}