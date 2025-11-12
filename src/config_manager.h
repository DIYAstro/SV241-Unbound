#ifndef CONFIG_MANAGER_H
#define CONFIG_MANAGER_H

#include <Arduino.h>
#include "ArduinoJson.h"

#define FIRMWARE_VERSION "0.9.3"

// Maximum number of supported dew heaters
#define MAX_DEW_HEATERS 2

// Structs for organizing configuration settings

struct SensorOffsets {
    float sht40_temp;
    float sht40_humidity;
    float ds18b20_temp;
    float ina219_voltage;
    float ina219_current;
};

struct UpdateIntervals {
    unsigned long ina219;
    unsigned long sht40;
    unsigned long ds18b20;
};

struct PowerStartupStates {
    bool dc1;
    bool dc2;
    bool dc3;
    bool dc4;
    bool dc5;
    bool usbc12;
    bool usb345;
    bool adj_conv;
    // Note: Dew heater startup state is now in DewHeaterConfig
};

struct AveragingCounts {
    int sht40_temp;
    int sht40_humidity;
    int ds18b20_temp;
    int ina219_voltage;
    int ina219_current;
};

struct DewHeaterConfig {
    char name[32];
    bool enabled_on_startup;
    int mode;              // 0: Manual, 1: PID, 2: Ambient Tracking
    int manual_power;      // Manual power in % (if mode is 0)
    
    // PID settings (for mode 1)
    float target_offset;   // Target temp = Dew Point + target_offset
    double pid_kp;
    double pid_ki;
    double pid_kd;

    // Ambient Tracking settings (for mode 2)
    float start_delta;     // Heating starts when T_ambient - T_dewpoint < start_delta
    float end_delta;       // Full power when T_ambient - T_dewpoint <= end_delta
    int max_power;         // Max power in % for this mode

    // PID Sync settings (for mode 3)
    float pid_sync_factor;
};


// Main configuration struct
struct Config {
    SensorOffsets sensor_offsets;
    UpdateIntervals update_intervals_ms;
    PowerStartupStates power_startup_states;
    AveragingCounts averaging_counts;
    float adj_conv_preset_v;
    DewHeaterConfig dew_heaters[MAX_DEW_HEATERS];
};

// Global configuration instance
extern Config config;

// Mutex to protect access to the config struct
extern SemaphoreHandle_t config_mutex;

// Mutex to protect access to the Serial port
extern SemaphoreHandle_t serial_mutex;

// Function declarations
bool initConfig();
bool loadConfig();
bool saveConfig();
void createDefaultConfig();
void serializeConfig(JsonDocument& doc);
void updateConfig(const JsonObject& doc);


#endif // CONFIG_MANAGER_H
