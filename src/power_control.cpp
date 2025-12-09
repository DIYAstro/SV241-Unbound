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
  // Using uint8_t to support 0=Off, 1=On, 2=Disabled
  uint8_t startup_states[POWER_OUTPUT_COUNT] = {
    config.power_startup_states.dc1,
    config.power_startup_states.dc2,
    config.power_startup_states.dc3,
    config.power_startup_states.dc4,
    config.power_startup_states.dc5,
    config.power_startup_states.usbc12,
    config.power_startup_states.usb345,
    config.power_startup_states.adj_conv,
    (uint8_t)(config.dew_heaters[0].enabled_on_startup ? 1 : 0),
    (uint8_t)(config.dew_heaters[1].enabled_on_startup ? 1 : 0)
  };
  xSemaphoreGive(config_mutex);

  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    // Outputs managed by other modules are skipped here
    if ((PowerOutput)i == POWER_ADJ_CONV || (PowerOutput)i == POWER_PWM1 || (PowerOutput)i == POWER_PWM2) {
        // Their state is set in their own setup function, but we need to track it here too.
        // For standard switches, 2 means Disabled, which physically means Off.
        power_output_states[i] = (startup_states[i] == 1);
        continue;
    }

    pinMode(power_output_pins[i], OUTPUT);
    
    // Logic: 0 -> Off, 1 -> On, 2 -> Disabled (Off)
    bool physical_state = (startup_states[i] == 1);
    set_power_output((PowerOutput)i, physical_state);
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
        // Special report for Adjustable Converter: Return target voltage if ON, else false (OFF)
        if (power_output_states[i]) {
             status[name] = get_adjustable_voltage_target();
        } else {
             status[name] = false;
        }
    } else if ((PowerOutput)i == POWER_PWM1) {
        // Correct logic for Auto Mode:
        // If Enabled AND in Auto Mode, report 'true` (Active) even if current power is 0.
        // This ensures the Switch UI stays ON.
        // If in Manual Mode, report the actual power level (0-100).
        bool enabled = get_dew_heater_state(0);
        int mode = get_dew_heater_mode(0); // 0=Manual, 1=Auto

        // If Enabled AND NOT in Manual Mode (0), report 'true' (Active) 
        // even if current power is 0. This covers Auto(1), Ambient(2), and Follower(3).
        if (enabled && mode != 0) {
             status[name] = true;
        } else if (enabled && mode == 0) {
             status[name] = get_heater_power(0); 
        } else {
             status[name] = false;
        }
    } else if ((PowerOutput)i == POWER_PWM2) {
        bool enabled = get_dew_heater_state(1);
        int mode = get_dew_heater_mode(1);

        // If Enabled AND NOT in Manual Mode (0), report 'true' (Active)
        // even if current power is 0. This covers Auto(1), Ambient(2), and Follower(3).
        if (enabled && mode != 0) {
             status[name] = true;
        } else if (enabled && mode == 0) {
             status[name] = get_heater_power(1);
        } else {
             status[name] = false;
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
          int heater_idx = ((PowerOutput)i == POWER_PWM1) ? 0 : 1;
          
          if (set_obj[name].is<bool>()) {
               bool state = set_obj[name].as<bool>();
               // If turning ON via boolean, reset RAM override to -1 (use config default)
               if (state) set_dew_heater_pwm_ram(heater_idx, -1);
               // Wait, don't reset to -1 on true, or we lose custom setting if user just toggles switch?
               // Actually for Alpaca, "True" usually means "On at default". 
               // But if we want persistence of session, maybe don't reset?
               // Let's stick to: Boolean TRUE = Reset to Config (Safe Default). Integer = Override.
               set_power_output((PowerOutput)i, state);
          } else if (set_obj[name].is<int>() || set_obj[name].is<float>()) {
               int pwm = set_obj[name].as<int>();
               pwm = constrain(pwm, 0, 100);
               set_dew_heater_pwm_ram(heater_idx, pwm);
               set_power_output((PowerOutput)i, true);
          }
          continue;
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