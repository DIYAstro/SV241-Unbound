#include "power_control.h"
#include "config_manager.h"
#include "hardware_pins.h"
#include "voltage_control.h"
#include "dew_control.h" // Include the new dew control header

// Array to hold the pin number for each power output
// Note: Pins managed by other modules (adj_conv, pwm1) use -1 as a placeholder.
const int power_output_pins[POWER_OUTPUT_COUNT] = {
  POWER_DC1_PIN,
  POWER_DC2_PIN,
  POWER_DC3_PIN,
  POWER_DC4_PIN,
  POWER_DC5_PIN,
  POWER_USBC12_PIN,
  POWER_USB345_PIN,
  -1, // Placeholder for POWER_ADJ_CONV
  -1,  // Placeholder for POWER_PWM1
  -1  // Placeholder for POWER_PWM2
};

// Array to hold the name for each power output
const char* const power_output_names[POWER_OUTPUT_COUNT] = {
  "d1",       // dc1
  "d2",       // dc2
  "d3",       // dc3
  "d4",       // dc4
  "d5",       // dc5
  "u12",      // usbc12
  "u34",      // usb345
  "adj",      // adj_conv
  "pwm1",     // pwm1 (bleibt gleich, da schon kurz)
  "pwm2"      // pwm2 (bleibt gleich, da schon kurz)
};

// Array to track the current state of each power output
static bool power_output_states[POWER_OUTPUT_COUNT];

void setup_power_outputs() {
  xSemaphoreTake(config_mutex, portMAX_DELAY);
  // Load startup states from the global config struct
  const bool startup_states[POWER_OUTPUT_COUNT] = {
    config.power_startup_states.dc1,
    config.power_startup_states.dc2,
    config.power_startup_states.dc3,
    config.power_startup_states.dc4,
    config.power_startup_states.dc5,
    config.power_startup_states.usbc12,
    config.power_startup_states.usb345,
    config.power_startup_states.adj_conv,
    config.dew_heaters[0].enabled_on_startup,
    config.dew_heaters[1].enabled_on_startup
  };
  xSemaphoreGive(config_mutex);

  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    // Outputs managed by other modules are skipped here
    if ((PowerOutput)i == POWER_ADJ_CONV || (PowerOutput)i == POWER_PWM1 || (PowerOutput)i == POWER_PWM2) {
        // Their state is set in their own setup function, but we need to track it here too.
        power_output_states[i] = startup_states[i];
        continue;
    }

    pinMode(power_output_pins[i], OUTPUT);
    set_power_output((PowerOutput)i, startup_states[i]);
  }
}

void set_power_output(PowerOutput output, bool on) {
  if (output < 0 || output >= POWER_OUTPUT_COUNT) return;

  // Special handling for outputs managed by other modules
  if (output == POWER_ADJ_CONV) {
    set_adjustable_converter_state(on);
  } else if (output == POWER_PWM1) {
    set_dew_heater_state(0, on); // 0 is the index for PWM1
  } else if (output == POWER_PWM2) {
    set_dew_heater_state(1, on); // 1 is the index for PWM2
  } else {
    digitalWrite(power_output_pins[output], on ? HIGH : LOW);
  }
  
  power_output_states[output] = on;
}

const char* get_power_output_name(PowerOutput output) {
  if (output >= 0 && output < POWER_OUTPUT_COUNT) {
    return power_output_names[output];
  }
  return "unknown";
}

void get_power_status_json(JsonDocument& doc) {
  JsonObject status = doc["status"].to<JsonObject>();
  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    const char* name = get_power_output_name((PowerOutput)i);
    if ((PowerOutput)i == POWER_ADJ_CONV) {
        // Special report for Adjustable Converter: Return target voltage if ON, else 0
        if (power_output_states[i]) {
             status[name] = get_adjustable_voltage_target();
        } else {
             status[name] = 0;
        }
    } else {
        // Standard On/Off
        status[name] = (int)power_output_states[i];
    }
  }
}

void handle_set_power_command(JsonVariant set_command) {
  if (set_command.is<JsonObject>()) {
    JsonObject set_obj = set_command.as<JsonObject>();

    // Check for the special "all" key first
    if (set_obj["all"].is<bool>() || set_obj["all"].is<int>()) {
        bool all_state = set_obj["all"].as<bool>();
        for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
            set_power_output((PowerOutput)i, all_state);
        }
        return; // Exit after handling the "all" command
    }

    // If "all" key is not present, proceed with individual keys
    for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
      const char* name = get_power_output_name((PowerOutput)i);
      
      // Check if the key exists in the object (using ArduinoJson v7 compatible check)
      if (set_obj[name].isNull()) continue;

      // Special handling for Adjustable Converter (0-15V RAM override)
      if ((PowerOutput)i == POWER_ADJ_CONV) {
         if (set_obj[name].is<bool>()) {
             bool state = set_obj[name].as<bool>();
             set_power_output((PowerOutput)i, state);
         } else if (set_obj[name].is<int>() || set_obj[name].is<float>()) {
             float v = set_obj[name].as<float>();
             set_adjustable_voltage_ram(v);
             set_power_output((PowerOutput)i, true);
         }
         continue;
      }

      // Special handling for PWM channels
      if ((PowerOutput)i == POWER_PWM1 || (PowerOutput)i == POWER_PWM2) {
          // ... (existing PWM logic, which was reverted by user but might exist in file if I messed up revert?)
          // Wait, user reverted PWM logic. I should just treat PWM as binary here unless I am restoring PWM too?
          // User said "Alpaca Override" for Voltage, he didn't say restore PWM.
          // BUT, I should check if my previous edits left power_control.cpp in a mixed state.
          // Re-reading power_control.cpp showed standard logic.
          // I will stick to standard binary for PWM as agreed.
      } 
      
      // Standard handling for all (including PWM if not special)
      if (set_obj[name].is<bool>() || set_obj[name].is<int>()) {
        bool state = set_obj[name].as<bool>();
        set_power_output((PowerOutput)i, state);
      }
    }
  }
}

bool get_power_output_state(PowerOutput output) {
  if (output >= 0 && output < POWER_OUTPUT_COUNT) {
    return power_output_states[output];
  }
  return false;
}