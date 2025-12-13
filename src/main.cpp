#include <Arduino.h>
#include <esp_task_wdt.h>
#include "config_manager.h"
#include "sensors.h"
#include "power_control.h"
#include "voltage_control.h"
#include "dew_control.h"

#define WDT_TIMEOUT 90 // 90 seconds

// Global variable for the config mutex
SemaphoreHandle_t config_mutex;
// Global variable for the serial port mutex
SemaphoreHandle_t serial_mutex;


// Task function to handle sensor updates
void sensor_update_task(void *pvParameters) {
  esp_task_wdt_add(NULL); // Register this task with the watchdog
  xSemaphoreTake(serial_mutex, portMAX_DELAY);
  Serial.println("Sensor update task started.");
  xSemaphoreGive(serial_mutex);
  for (;;)
 {
    update_sensor_cache();
    // Yield to other tasks
    vTaskDelay(100 / portTICK_PERIOD_MS);
    esp_task_wdt_reset(); // Feed the watchdog
  }
}

// Task function to monitor memory usage
void memory_monitor_task(void *pvParameters) {
  esp_task_wdt_add(NULL); // Register this task with the watchdog
  xSemaphoreTake(serial_mutex, portMAX_DELAY);
  Serial.println("Memory monitor task started.");
  xSemaphoreGive(serial_mutex);
  for (;;) {
    if(xSemaphoreTake(sensor_cache_mutex, (TickType_t)10) == pdTRUE) {
      sensor_cache.heap_free = ESP.getFreeHeap();
      sensor_cache.heap_min_free = ESP.getMinFreeHeap();
      sensor_cache.heap_max_alloc = ESP.getMaxAllocHeap();
      sensor_cache.heap_size = ESP.getHeapSize();
      xSemaphoreGive(sensor_cache_mutex);
    }
    vTaskDelay(pdMS_TO_TICKS(60000)); // Run every 60 seconds
    esp_task_wdt_reset(); // Feed the watchdog
  }
}

// Task function to handle serial commands
void serial_command_task(void *pvParameters) {
  esp_task_wdt_add(NULL); // Register this task with the watchdog
  xSemaphoreTake(serial_mutex, portMAX_DELAY);
  Serial.println("Serial command task started.");
  xSemaphoreGive(serial_mutex);

  // Use static buffers to avoid heap fragmentation
  const size_t MAX_INPUT_SIZE = 8192;
  static char input_buffer[MAX_INPUT_SIZE];
  static size_t input_pos = 0;
  static JsonDocument doc;  // Static to reduce heap fragmentation

  for (;;) {
    while (Serial.available() > 0) {
      char incoming_char = Serial.read();

      if (incoming_char == '\n') {
        // Null-terminate the string
        input_buffer[input_pos] = '\0';

        // reset position for next command
        input_pos = 0;

        // Try to parse the input string as JSON
        doc.clear();  // Clear before each use
        DeserializationError error = deserializeJson(doc, input_buffer);

        if (!error) {
          if (doc["command"].is<const char*>() && strcmp(doc["command"], "reboot") == 0) {
            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println("{\"status\":\"rebooting\"}");
            xSemaphoreGive(serial_mutex);
            delay(100);
            ESP.restart();
          } else if (doc["command"].is<const char*>() && strcmp(doc["command"], "factory_reset") == 0) {
            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println("{\"status\":\"performing factory reset\"}");
            xSemaphoreGive(serial_mutex);
            xSemaphoreTake(config_mutex, portMAX_DELAY);
            createDefaultConfig(); // This function is in config_manager.cpp
            xSemaphoreGive(config_mutex);
            delay(100);
            ESP.restart();
          } else if (doc["command"].is<const char*>() && strcmp(doc["command"], "dry_sensor") == 0) {
            // This is a blocking call, so the serial task will be busy until it's done.
            // This is acceptable for a maintenance command.
            dry_sht40_sensor();
          } else if (doc["get"].is<const char*>() && strcmp(doc["get"], "status") == 0) {
            String output_buffer;
            JsonDocument status_doc;
            get_power_status_json(status_doc);
            
            // Piggyback Dew Heater Modes onto status for lightweight detection
            JsonArray dew_modes = status_doc["dm"].to<JsonArray>();
            for(int i=0; i<MAX_DEW_HEATERS; i++) {
                dew_modes.add(get_dew_heater_mode(i));
            }

            serializeJson(status_doc, output_buffer);
            
            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println(output_buffer);
            xSemaphoreGive(serial_mutex);

          } else if (doc["set"].is<JsonObject>()) {
            // Apply power settings
            handle_set_power_command(doc["set"]);
            // Respond with the updated power status directly to the serial port for performance.
            // This is safe because get_power_status_json() does not take other mutexes.
            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            JsonDocument status_doc;
            get_power_status_json(status_doc);
            serializeJson(status_doc, Serial);
            Serial.println();
            xSemaphoreGive(serial_mutex);

          } else if (doc["get"].is<const char*>() && strcmp(doc["get"], "config") == 0) {
            String output_buffer;
            JsonDocument config_doc;
            xSemaphoreTake(config_mutex, portMAX_DELAY);
            serializeConfig(config_doc);
            xSemaphoreGive(config_mutex);
            
            serializeJson(config_doc, output_buffer);

            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println(output_buffer);
            xSemaphoreGive(serial_mutex);

          } else if (doc["get"].is<const char*>() && strcmp(doc["get"], "sensors") == 0) {
            String output_buffer;
            JsonDocument sensors_doc;
            get_sensor_values_json(sensors_doc); // This function is now safe
            serializeJson(sensors_doc, output_buffer);

            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println(output_buffer);
            xSemaphoreGive(serial_mutex);

          } else if (doc["get"].is<const char*>() && strcmp(doc["get"], "version") == 0) {
            String output_buffer;
            JsonDocument version_doc;
            version_doc["version"] = FIRMWARE_VERSION;
            serializeJson(version_doc, output_buffer);

            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println(output_buffer);
            xSemaphoreGive(serial_mutex);

          } else if (doc["sc"].is<JsonObject>()) { // "sc" for "set_config"
            JsonObject set_obj = doc["sc"].as<JsonObject>();
            bool adj_voltage_changed = !set_obj["av"].isNull(); // "av" for "adj_conv_preset_v"
            String output_buffer;
            
            xSemaphoreTake(config_mutex, portMAX_DELAY);
            updateConfig(set_obj);
            saveConfig();
            JsonDocument config_doc;
            serializeConfig(config_doc);
            xSemaphoreGive(config_mutex);

            serializeJson(config_doc, output_buffer);

            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println(output_buffer);
            xSemaphoreGive(serial_mutex);

            // Now, outside the previous mutex lock, apply live settings that require it.
            if (adj_voltage_changed) {
              if (get_power_output_state(POWER_ADJ_CONV)) {
                set_adjustable_converter_state(true); 
              }
            }
          } else {
            // JSON is valid, but the command is not recognized
            xSemaphoreTake(serial_mutex, portMAX_DELAY);
            Serial.println("{\"error\":\"unknown command in valid JSON\"}");
            xSemaphoreGive(serial_mutex);
          }
        } else {
          // If not valid JSON, check for simple commands
          // Note: trim() is not available on char arrays easily, but since we just parsed JSON and failed,
          // and we don't have other simple text commands besides JSON ones in the original code (except implicit logic),
          // we just report error.
          // The orig logic called `input_string.trim()`. 
          // Since we are moving to pure JSON, we can likely skip the complex trimming or just ignore whitespace.
          
          xSemaphoreTake(serial_mutex, portMAX_DELAY);
          Serial.println("{\"error\":\"invalid command\"}");
          xSemaphoreGive(serial_mutex);
        }
      } else {
        if (input_pos < MAX_INPUT_SIZE - 1) {
          input_buffer[input_pos++] = incoming_char;
        } else {
          // Buffer overflow - discard characters until newline or just reset?
          // For safety, let's reset and ignore the rest of this line until newline.
          // But implementing "ignore until newline" is complex inside this simple loop without state.
          // Simplest approach: Just stop adding, effectively truncating.
          // When newline comes, it will try to parse a truncated string, likely fail JSON, and error out.
          // This is a safe failure mode.
        }
      }
    }
    // Yield to other tasks
    esp_task_wdt_reset(); // Feed the watchdog
    vTaskDelay(10 / portTICK_PERIOD_MS);
  }
}

void setup() {
  Serial.begin(115200);
  while (!Serial) {
    delay(10); // wait for serial port to connect.
  }

  // Initialize the Task Watchdog Timer
  esp_task_wdt_init(WDT_TIMEOUT, true); // true = panic and reboot on timeout
  esp_task_wdt_add(NULL); // Add the main loop task to the watchdog
  
  // Serial is now available, but we can't protect it until the mutex is created.
  Serial.println("\n--- SV241-Unbound ---");


  // Create the mutex for thread-safe cache access
  sensor_cache_mutex = xSemaphoreCreateMutex();
  if (sensor_cache_mutex == NULL) {
    Serial.println("Error: Could not create sensor cache mutex!");
    while(1); // Halt
  }

  // Create the mutex for thread-safe config access
  config_mutex = xSemaphoreCreateMutex();
  if (config_mutex == NULL) {
    Serial.println("Error: Could not create config mutex!");
    while(1); // Halt
  }

  // Create the mutex for thread-safe serial port access
  serial_mutex = xSemaphoreCreateMutex();
  if (serial_mutex == NULL) {
    Serial.println("Error: Could not create serial mutex!");
    while(1); // Halt
  }

  // Initialize configuration and log the outcome
  if (initConfig()) {
    xSemaphoreTake(serial_mutex, portMAX_DELAY);
    Serial.println("Default configuration created.");
    xSemaphoreGive(serial_mutex);
  } else {
    xSemaphoreTake(serial_mutex, portMAX_DELAY);
    Serial.println("Existing configuration loaded.");
    xSemaphoreGive(serial_mutex);
  }

  // Initialize sensors

  // Initialize sensors
  setup_sensors();

  // Initialize voltage-controlled outputs first
  setup_voltage_control();

  // Initialize power outputs
  setup_power_outputs();

  // Initialize dew heaters
  setup_dew_heaters();

  xSemaphoreTake(serial_mutex, portMAX_DELAY);
  Serial.println("Creating FreeRTOS tasks...");
  xSemaphoreGive(serial_mutex);

  // Create and pin tasks to specific cores
  xTaskCreatePinnedToCore(
      sensor_update_task,   // Task function
      "SensorUpdateTask",   // Name of the task
      4096,                 // Stack size of the task
      NULL,                 // Parameter of the task
      1,                    // Priority of the task
      NULL,                 // Task handle to keep track of the task
      1);                   // Pin to core 1

  xTaskCreatePinnedToCore(
      serial_command_task,  // Task function
      "SerialCommandTask",  // Name of the task
      4096,                 // Stack size of the task
      NULL,                 // Parameter of the task
      1,                    // Priority of the task
      NULL,                 // Task handle to keep track of the task
      0);                   // Pin to core 0

  xTaskCreatePinnedToCore(
      memory_monitor_task,  // Task function
      "MemoryMonitorTask",  // Name of the task
      2048,                 // Stack size of the task
      NULL,                 // Parameter of the task
      1,                    // Priority of the task
      NULL,                 // Task handle to keep track of the task
      1);                   // Pin to core 1

  xSemaphoreTake(serial_mutex, portMAX_DELAY);
  Serial.println("Setup complete. Ready for JSON commands.");
  xSemaphoreGive(serial_mutex);
}

void loop() {
  // The loop is used to feed the watchdog for the main task.
  esp_task_wdt_reset();
  vTaskDelay(pdMS_TO_TICKS(1000)); // Feed watchdog every second
}