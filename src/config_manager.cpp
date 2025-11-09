#include "config_manager.h"
#include <LittleFS.h>

// Forward declarations
void populateDefaultConfig();
void createDefaultDewHeaterConfig(int index);

// Global configuration instance
Config config;

const char* configFile = "/config.bin"; // Using a binary file now

// Populates the config struct with default values, but does not save.
void populateDefaultConfig() {
    config.sensor_offsets = {0.0f, -10.0f, 0.0f, 0.0f, 0.0f};
    config.update_intervals_ms = {1000, 1000, 1000};
    config.power_startup_states = {false, false, false, false, false, false, false, false};
    config.averaging_counts = {5, 5, 5, 5, 5};
    config.adj_conv_preset_v = 0.0f;

    for (int i = 0; i < MAX_DEW_HEATERS; i++) {
        createDefaultDewHeaterConfig(i);
    }
}

// Initializes the configuration from the filesystem.
bool initConfig() {
    // The `true` parameter formats the file system if it can't be mounted.
    // This is crucial for recovery from a corrupt file system.
    if (!LittleFS.begin(true)) {
        populateDefaultConfig();
        return true; // Indicate that defaults were populated for this session
    }

    if (!loadConfig()) {
        createDefaultConfig(); // Create and save a new default config
        return true; // Indicate a new config was created
    }
    return false; // Indicate existing config was loaded
}

// Loads the configuration from the binary file directly into the config struct.
bool loadConfig() {
    // The `true` parameter formats the file system if it can't be mounted.
    // This ensures that even if the filesystem gets corrupted, we can recover.
    if (!LittleFS.begin(true)) {
        return false;
    }

    File file = LittleFS.open(configFile, "r");
    if (!file) {
        return false;
    }

    // Sanity check: if the file size doesn't match the struct size, it's an old or corrupt file.
    if (file.size() != sizeof(Config)) {
        file.close();
        return false;
    }

    size_t bytes_read = file.read((uint8_t*)&config, sizeof(Config));
    file.close();

    if (bytes_read != sizeof(Config)) {
        return false;
    }

    return true;
}

// Saves the current in-memory config to the binary file.
bool saveConfig() {
    // The `true` parameter formats the file system if it can't be mounted.
    // This is the most robust way to ensure we can always write.
    if (!LittleFS.begin(true)) {
        return false;
    }

    File file = LittleFS.open(configFile, "w");
    if (!file) {
        return false;
    }

    size_t bytes_written = file.write((uint8_t*)&config, sizeof(Config));
    file.close();

    if (bytes_written != sizeof(Config)) {
        return false;
    }
    return true;
}

// Creates default values for a single dew heater config.
void createDefaultDewHeaterConfig(int index) {
    if (index < 0 || index >= MAX_DEW_HEATERS) return;

    snprintf(config.dew_heaters[index].name, sizeof(config.dew_heaters[index].name), "PWM%d", index + 1);
    config.dew_heaters[index].enabled_on_startup = false;
    config.dew_heaters[index].manual_power = 0;

    if (index == 0) { // PWM1 defaults to PID
        config.dew_heaters[index].mode = 1;
        config.dew_heaters[index].target_offset = 3.0f;
        config.dew_heaters[index].pid_kp = 20.0;
        config.dew_heaters[index].pid_ki = 1.0;
        config.dew_heaters[index].pid_kd = 15.0;
        config.dew_heaters[index].start_delta = 5.0f;
        config.dew_heaters[index].end_delta = 1.0f;
        config.dew_heaters[index].max_power = 80;
        config.dew_heaters[index].pid_sync_factor = 1.0f;
    } else { // PWM2 defaults to Ambient Tracking
        config.dew_heaters[index].mode = 2;
        config.dew_heaters[index].target_offset = 3.0f;
        config.dew_heaters[index].pid_kp = 20.0;
        config.dew_heaters[index].pid_ki = 1.0;
        config.dew_heaters[index].pid_kd = 15.0;
        config.dew_heaters[index].start_delta = 5.0f;
        config.dew_heaters[index].end_delta = 1.0f;
        config.dew_heaters[index].max_power = 80;
        config.dew_heaters[index].pid_sync_factor = 1.0f;
    }
}

// Creates and saves a default configuration file.
void createDefaultConfig() {
    populateDefaultConfig();
    saveConfig();
}


// --- JSON Communication Functions (Largely Unchanged) ---
// These functions are for the external API and still use JSON.

void serializeConfig(JsonDocument& doc) {
    JsonObject sensor_offsets = doc["so"].to<JsonObject>();
    sensor_offsets["st"] = config.sensor_offsets.sht40_temp;
    sensor_offsets["sh"] = config.sensor_offsets.sht40_humidity;
    sensor_offsets["dt"] = config.sensor_offsets.ds18b20_temp;
    sensor_offsets["iv"] = config.sensor_offsets.ina219_voltage;
    sensor_offsets["ic"] = config.sensor_offsets.ina219_current;

    JsonObject update_intervals_ms = doc["ui"].to<JsonObject>();
    update_intervals_ms["i"] = config.update_intervals_ms.ina219;
    update_intervals_ms["s"] = config.update_intervals_ms.sht40;
    update_intervals_ms["d"] = config.update_intervals_ms.ds18b20;

    JsonObject power_startup_states = doc["ps"].to<JsonObject>();
    power_startup_states["d1"] = (int)config.power_startup_states.dc1;
    power_startup_states["d2"] = (int)config.power_startup_states.dc2;
    power_startup_states["d3"] = (int)config.power_startup_states.dc3;
    power_startup_states["d4"] = (int)config.power_startup_states.dc4;
    power_startup_states["d5"] = (int)config.power_startup_states.dc5;
    power_startup_states["u12"] = (int)config.power_startup_states.usbc12;
    power_startup_states["u34"] = (int)config.power_startup_states.usb345;
    power_startup_states["adj"] = (int)config.power_startup_states.adj_conv;

    JsonObject averaging_counts = doc["ac"].to<JsonObject>();
    averaging_counts["st"] = config.averaging_counts.sht40_temp;
    averaging_counts["sh"] = config.averaging_counts.sht40_humidity;
    averaging_counts["dt"] = config.averaging_counts.ds18b20_temp;
    averaging_counts["iv"] = config.averaging_counts.ina219_voltage;
    averaging_counts["ic"] = config.averaging_counts.ina219_current;

    doc["av"] = config.adj_conv_preset_v;

    JsonArray dew_heaters_arr = doc["dh"].to<JsonArray>();
    for (int i = 0; i < MAX_DEW_HEATERS; i++) {
        JsonObject heater_obj = dew_heaters_arr.add<JsonObject>();
        heater_obj["n"] = config.dew_heaters[i].name;
        heater_obj["en"] = (int)config.dew_heaters[i].enabled_on_startup;
        heater_obj["m"] = config.dew_heaters[i].mode;
        heater_obj["mp"] = config.dew_heaters[i].manual_power;
        heater_obj["to"] = config.dew_heaters[i].target_offset;
        heater_obj["kp"] = config.dew_heaters[i].pid_kp;
        heater_obj["ki"] = config.dew_heaters[i].pid_ki;
        heater_obj["kd"] = config.dew_heaters[i].pid_kd;
        heater_obj["sd"] = config.dew_heaters[i].start_delta;
        heater_obj["ed"] = config.dew_heaters[i].end_delta;
        heater_obj["xp"] = config.dew_heaters[i].max_power;
        heater_obj["psf"] = config.dew_heaters[i].pid_sync_factor;
    }
}

void updateConfig(const JsonObject& doc) {
    if (!doc["so"].isNull()) {
        JsonObjectConst sensor_offsets = doc["so"];
        config.sensor_offsets.sht40_temp = sensor_offsets["st"] | config.sensor_offsets.sht40_temp;
        config.sensor_offsets.sht40_humidity = sensor_offsets["sh"] | config.sensor_offsets.sht40_humidity;
        config.sensor_offsets.ds18b20_temp = sensor_offsets["dt"] | config.sensor_offsets.ds18b20_temp;
        config.sensor_offsets.ina219_voltage = sensor_offsets["iv"] | config.sensor_offsets.ina219_voltage;
        config.sensor_offsets.ina219_current = sensor_offsets["ic"] | config.sensor_offsets.ina219_current;
    }

    if (!doc["ui"].isNull()) {
        JsonObjectConst update_intervals_ms = doc["ui"];
        config.update_intervals_ms.ina219 = update_intervals_ms["i"] | config.update_intervals_ms.ina219;
        config.update_intervals_ms.sht40 = update_intervals_ms["s"] | config.update_intervals_ms.sht40;
        config.update_intervals_ms.ds18b20 = update_intervals_ms["d"] | config.update_intervals_ms.ds18b20;
    }

    if (!doc["ps"].isNull()) {
        JsonObjectConst power_startup_states = doc["ps"];
        config.power_startup_states.dc1 = power_startup_states["d1"] | config.power_startup_states.dc1;
        config.power_startup_states.dc2 = power_startup_states["d2"] | config.power_startup_states.dc2;
        config.power_startup_states.dc3 = power_startup_states["d3"] | config.power_startup_states.dc3;
        config.power_startup_states.dc4 = power_startup_states["d4"] | config.power_startup_states.dc4;
        config.power_startup_states.dc5 = power_startup_states["d5"] | config.power_startup_states.dc5;
        config.power_startup_states.usbc12 = power_startup_states["u12"] | config.power_startup_states.usbc12;
        config.power_startup_states.usb345 = power_startup_states["u34"] | config.power_startup_states.usb345;
        config.power_startup_states.adj_conv = power_startup_states["adj"] | config.power_startup_states.adj_conv;
    }

    if (!doc["ac"].isNull()) {
        JsonObjectConst averaging_counts = doc["ac"];
        config.averaging_counts.sht40_temp = averaging_counts["st"] | config.averaging_counts.sht40_temp;
        config.averaging_counts.sht40_humidity = averaging_counts["sh"] | config.averaging_counts.sht40_humidity;
        config.averaging_counts.ds18b20_temp = averaging_counts["dt"] | config.averaging_counts.ds18b20_temp;
        config.averaging_counts.ina219_voltage = averaging_counts["iv"] | config.averaging_counts.ina219_voltage;
        config.averaging_counts.ina219_current = averaging_counts["ic"] | config.averaging_counts.ina219_current;
    }

    if (!doc["av"].isNull()) {
        config.adj_conv_preset_v = doc["av"] | config.adj_conv_preset_v;
    }

    JsonArrayConst dew_heaters_arr = doc["dh"];
    if (!dew_heaters_arr.isNull()) {
        for (int i = 0; i < MAX_DEW_HEATERS; i++) {
            if (i < dew_heaters_arr.size() && !dew_heaters_arr[i].isNull()) {
                JsonObjectConst heater_obj = dew_heaters_arr[i];
                if (!heater_obj["n"].isNull()) {
                    const char* new_name = heater_obj["n"];
                    strncpy(config.dew_heaters[i].name, new_name, sizeof(config.dew_heaters[i].name));
                    config.dew_heaters[i].name[sizeof(config.dew_heaters[i].name) - 1] = '\0'; // Ensure null termination
                }
                if (!heater_obj["en"].isNull()) config.dew_heaters[i].enabled_on_startup = heater_obj["en"];
                
                if (!heater_obj["auto_mode"].isNull()) {
                    config.dew_heaters[i].mode = (heater_obj["auto_mode"].as<bool>()) ? 1 : 0;
                }
                if (!heater_obj["m"].isNull()) config.dew_heaters[i].mode = heater_obj["m"];

                if (!heater_obj["mp"].isNull()) config.dew_heaters[i].manual_power = heater_obj["mp"];
                
                if (!heater_obj["to"].isNull()) {
                    float offset = heater_obj["to"];
                    if (offset > 0) {
                        config.dew_heaters[i].target_offset = offset;
                    }
                }

                if (!heater_obj["kp"].isNull()) config.dew_heaters[i].pid_kp = heater_obj["kp"];
                if (!heater_obj["ki"].isNull()) config.dew_heaters[i].pid_ki = heater_obj["ki"];
                if (!heater_obj["kd"].isNull()) config.dew_heaters[i].pid_kd = heater_obj["kd"];

                if (!heater_obj["sd"].isNull()) config.dew_heaters[i].start_delta = heater_obj["sd"];
                if (!heater_obj["ed"].isNull()) config.dew_heaters[i].end_delta = heater_obj["ed"];
                if (!heater_obj["xp"].isNull()) config.dew_heaters[i].max_power = heater_obj["xp"];
                if (!heater_obj["psf"].isNull()) config.dew_heaters[i].pid_sync_factor = heater_obj["psf"];
            }
        }
    }
}