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

// --- Staggered "all" power-on (soft-start) ---
// Queue used by the live {"set":{"all":true}} command, serviced asynchronously by
// service_power_stagger_queue() so handle_set_power_command() never blocks (see
// power_control.cpp's handle_set_power_command() and main.cpp's power_stagger_task()).
// Access to these is synchronized via stagger_mutex, not `volatile` - a mutex already provides
// the necessary memory visibility/ordering guarantees between the task that queues (in
// handle_set_power_command()) and the task that drains the queue (service_power_stagger_queue()).
static PowerOutput stagger_queue[POWER_OUTPUT_COUNT];
static int stagger_queue_len = 0;
static int stagger_queue_pos = 0;
static unsigned long stagger_due_at_ms = 0;
static SemaphoreHandle_t stagger_mutex = NULL;

// Forward declarations - defined further down alongside the rest of the delayed-action queue's
// internals, but handle_set_power_command() (defined before that point in this file) needs to
// call them too. See each definition's doc comment for why.
static void clear_delayed_action_queue();
static void clear_delayed_action_for(PowerOutput output);

void setup_power_outputs() {
  if (stagger_mutex == NULL) {
    stagger_mutex = xSemaphoreCreateMutex();
  }

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
  unsigned long stagger_delay_ms = config.poweron_stagger_delay_ms;
  // Configured per-switch delay-on (see SwitchTimingConfig) - copied into a local array under
  // the same config_mutex block as startup_states[]/stagger_delay_ms above, for the same reason:
  // this loop runs unlocked below.
  unsigned long delay_on_s[POWER_OUTPUT_COUNT];
  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    delay_on_s[i] = config.switch_timing[i].delay_on_s;
  }
  xSemaphoreGive(config_mutex);

  // This runs in setup(), before any FreeRTOS tasks are created (setup_power_outputs() is
  // called ahead of every xTaskCreatePinnedToCore() in main.cpp), so a blocking vTaskDelay()
  // here cannot stall anything else - unlike the live "all" command below, which needs the
  // async queue instead. Delay before each enable except the first one, so multiple DC/USB
  // outputs configured to power on at boot don't all inrush-current-spike simultaneously.
  bool first_enable = true;
  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    // Outputs managed by other modules are skipped here
    if ((PowerOutput)i == POWER_ADJ_CONV || (PowerOutput)i == POWER_PWM1 || (PowerOutput)i == POWER_PWM2) {
        // Their physical state is applied in their own setup function (setup_voltage_control(),
        // dew heater setup), so this loop never calls set_power_output() for them - but we still
        // need to track the resulting state here for get_power_status_json(). For standard
        // switches, 2 means Disabled, which physically means Off. If that other setup function
        // deferred its own enable via schedule_delayed_action() (configured delay_on_s > 0), the
        // output isn't physically on yet either - stay false until the delayed action actually
        // fires (it calls set_power_output() itself, which updates this array then). Currently
        // only reachable for ADJ_CONV: PWM1/PWM2 never get a configured delay_on_s (SwitchConfig.
        // vue's dl only covers dc1-5/usbc12/usb345/adj_conv), so delay_on_s[i] is always 0 there.
        power_output_states[i] = (startup_states[i] == 1) && delay_on_s[i] == 0;
        continue;
    }

    pinMode(power_output_pins[i], OUTPUT);

    // Logic: 0 -> Off, 1 -> On, 2 -> Disabled (Off)
    bool physical_state = (startup_states[i] == 1);
    if (physical_state && stagger_delay_ms > 0) {
        if (!first_enable) {
            vTaskDelay(pdMS_TO_TICKS(stagger_delay_ms));
        }
        first_enable = false;
    }

    if (physical_state && delay_on_s[i] > 0) {
      // Stagger sequencing above already happened regardless - only this one output's actual
      // enable is pushed further out by its own configured delay, on top of its place in the
      // boot sequence. Mirrors service_power_stagger_queue()'s equivalent handling for Master
      // Power On.
      schedule_delayed_action((PowerOutput)i, true, delay_on_s[i] * 1000UL);
    } else {
      set_power_output((PowerOutput)i, physical_state);
    }
  }
}

bool set_power_output(PowerOutput output, bool on) {
  if (output < 0 || output >= POWER_OUTPUT_COUNT) return true;

  // If trying to turn ON, check if this output is disabled in config
  if (on) {
    bool is_disabled = false;
    xSemaphoreTake(config_mutex, portMAX_DELAY);
    switch (output) {
      case POWER_DC1:     is_disabled = (config.power_startup_states.dc1 == 2); break;
      case POWER_DC2:     is_disabled = (config.power_startup_states.dc2 == 2); break;
      case POWER_DC3:     is_disabled = (config.power_startup_states.dc3 == 2); break;
      case POWER_DC4:     is_disabled = (config.power_startup_states.dc4 == 2); break;
      case POWER_DC5:     is_disabled = (config.power_startup_states.dc5 == 2); break;
      case POWER_USBC12:  is_disabled = (config.power_startup_states.usbc12 == 2); break;
      case POWER_USB345:  is_disabled = (config.power_startup_states.usb345 == 2); break;
      case POWER_ADJ_CONV: is_disabled = (config.power_startup_states.adj_conv == 2); break;
      case POWER_PWM1:    is_disabled = (config.dew_heaters[0].mode == DEW_MODE_DISABLED); break;
      case POWER_PWM2:    is_disabled = (config.dew_heaters[1].mode == DEW_MODE_DISABLED); break;
      default: break;
    }
    xSemaphoreGive(config_mutex);

    if (is_disabled) {
      xSemaphoreTake(serial_mutex, portMAX_DELAY);
      Serial.printf("{\"error\":\"Cannot enable disabled output: %s\"}\n", get_power_output_name(output));
      xSemaphoreGive(serial_mutex);
      return false; // Block the command - caller must not send a second response line
    }
  }

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
  return true;
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

bool handle_set_power_command(JsonVariant set_command) {
  bool all_succeeded = true;
  if (set_command.is<JsonObject>()) {
    JsonObject set_obj = set_command.as<JsonObject>();

    // Check for the special "all" key first
    if (set_obj["all"].is<bool>() || set_obj["all"].is<int>()) {
        bool all_state = set_obj["all"].as<bool>();

        // Build array of disabled states from config (inside mutex)
        xSemaphoreTake(config_mutex, portMAX_DELAY);
        bool is_disabled[POWER_OUTPUT_COUNT] = {
            config.power_startup_states.dc1 == 2,
            config.power_startup_states.dc2 == 2,
            config.power_startup_states.dc3 == 2,
            config.power_startup_states.dc4 == 2,
            config.power_startup_states.dc5 == 2,
            config.power_startup_states.usbc12 == 2,
            config.power_startup_states.usb345 == 2,
            config.power_startup_states.adj_conv == 2,
            config.dew_heaters[0].mode == DEW_MODE_DISABLED,
            config.dew_heaters[1].mode == DEW_MODE_DISABLED
        };
        xSemaphoreGive(config_mutex);

        // Master Power is a decisive, complete action - any delayed action left over from an
        // earlier individual command (explicit "DelayedOn"/"DelayedOff", or a configured
        // per-switch delay) must not be allowed to override it later. See
        // clear_delayed_action_queue()'s doc comment - found via real hardware testing.
        clear_delayed_action_queue();

        if (all_state) {
            // Turning on: don't switch directly here (would block this task, and thus the
            // {"set":{"all":true}} response, for up to several seconds - see
            // service_power_stagger_queue()). Queue the non-disabled outputs instead; a
            // background task enables them one at a time with the configured delay between.
            xSemaphoreTake(stagger_mutex, portMAX_DELAY);
            stagger_queue_len = 0;
            stagger_queue_pos = 0;
            for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
                if (is_disabled[i]) continue;
                stagger_queue[stagger_queue_len++] = (PowerOutput)i;
            }
            stagger_due_at_ms = millis(); // first queued output is due immediately
            xSemaphoreGive(stagger_mutex);
        } else {
            // Turning off: no inrush-current concern, switch everything immediately as before.
            // Also clear any still-pending staggered "on" queue, so a follower output from an
            // earlier all:true doesn't switch back on seconds after the user just turned
            // everything off.
            xSemaphoreTake(stagger_mutex, portMAX_DELAY);
            stagger_queue_len = 0;
            stagger_queue_pos = 0;
            xSemaphoreGive(stagger_mutex);

            for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
                if (is_disabled[i]) continue;
                set_power_output((PowerOutput)i, false);
            }
        }
        // Neither branch above can reject: the staggered-on path already filters out disabled
        // outputs before queuing them, and turning off is never rejected - so no {"error":...}
        // line was ever sent for this command.
        return true; // Exit after handling the "all" command
    }

    // If "all" key is not present, proceed with individual keys
    for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
      const char* name = get_power_output_name((PowerOutput)i);

      // Check if the key exists in the object (using ArduinoJson v7 compatible check)
      if (set_obj[name].isNull()) continue;

      // Special handling for Adjustable Converter (0-15V RAM override)
      if ((PowerOutput)i == POWER_ADJ_CONV) {
         bool state = false;
         bool have_state = false;
         if (set_obj[name].is<bool>()) {
             state = set_obj[name].as<bool>();
             have_state = true;
         } else if (set_obj[name].is<int>() || set_obj[name].is<float>()) {
             float v = set_obj[name].as<float>();
             state = (v > 0.0f);
             have_state = true;
             // Applied immediately regardless of any configured delay below - this only writes
             // the RAM target voltage_control.cpp's set_adjustable_converter_state() reads once
             // the output actually gets enabled (right away, or later via the delayed path); it
             // does not itself energize anything.
             if (state) set_adjustable_voltage_ram(v);
         }

         if (have_state) {
             // Configured per-switch delay (see SwitchTimingConfig) - same mechanism as the
             // "standard handling" branch further below, just applied here too now (this branch
             // continue()s past that one, so it never runs it itself). Requested by the user
             // after noticing Adj. Port was the only switch that ignored its configured delay.
             unsigned long delay_s = 0;
             xSemaphoreTake(config_mutex, portMAX_DELAY);
             delay_s = state ? config.switch_timing[i].delay_on_s : config.switch_timing[i].delay_off_s;
             xSemaphoreGive(config_mutex);

             // See clear_delayed_action_for()'s doc comment: must run before the immediate branch
             // below, or a still-pending delayed action from an earlier command on this same
             // output could fire later and undo this one.
             clear_delayed_action_for((PowerOutput)i);
             if (delay_s > 0) {
                 schedule_delayed_action((PowerOutput)i, state, delay_s * 1000UL);
             } else {
                 all_succeeded &= set_power_output((PowerOutput)i, state);
             }
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
               all_succeeded &= set_power_output((PowerOutput)i, state);
          } else if (set_obj[name].is<int>() || set_obj[name].is<float>()) {
               int pwm = set_obj[name].as<int>();
               pwm = constrain(pwm, 0, 100);
               set_dew_heater_pwm_ram(heater_idx, pwm);
               all_succeeded &= set_power_output((PowerOutput)i, true);
          }
          continue;
      }

      // Standard handling for all (including PWM if not special)
      if (set_obj[name].is<bool>() || set_obj[name].is<int>()) {
        bool state = set_obj[name].as<bool>();

        // Configured per-switch delay (see SwitchTimingConfig) - only ever reached for the plain
        // DC/USB switches (ADJ_CONV/PWM1/PWM2 are already handled and `continue`d above), which
        // is also the only scope this feature was built for. Deliberately not applied here to
        // "all" (Master Power) - see handle_set_power_command's "all" branch above, which always
        // stays immediate on purpose.
        unsigned long delay_s = 0;
        xSemaphoreTake(config_mutex, portMAX_DELAY);
        delay_s = state ? config.switch_timing[i].delay_on_s : config.switch_timing[i].delay_off_s;
        xSemaphoreGive(config_mutex);

        // See clear_delayed_action_for()'s doc comment: must run before the immediate branch
        // below, or a still-pending delayed action from an earlier command on this same output
        // could fire later and undo this one.
        clear_delayed_action_for((PowerOutput)i);
        if (delay_s > 0) {
          schedule_delayed_action((PowerOutput)i, state, delay_s * 1000UL);
          // Request accepted, just deferred - not a failure.
        } else {
          all_succeeded &= set_power_output((PowerOutput)i, state);
        }
      }
    }
  }
  return all_succeeded;
}

// --- Delayed Action queue ---
// Unlike stagger_queue above (which drains sequentially, one entry at a time with a shared
// delay), each output here counts down independently to its own due time - multiple unrelated
// delayed actions (on unrelated outputs, or even opposite directions) can be pending at once.
// Deliberately NOT persisted to flash: a delayed action only makes sense in the context of the
// live session that scheduled it - if the ESP32 itself reboots, that context is gone anyway.
struct DelayedActionEntry {
  bool pending;
  bool turn_on;   // true = turn on when due, false = turn off
  unsigned long due_at_ms;
};
static DelayedActionEntry delayed_action_queue[POWER_OUTPUT_COUNT];
static SemaphoreHandle_t delayed_action_mutex = NULL;

void schedule_delayed_action(PowerOutput output, bool turn_on, unsigned long delay_ms) {
  if (output < 0 || output >= POWER_OUTPUT_COUNT) return;
  if (delayed_action_mutex == NULL) {
    delayed_action_mutex = xSemaphoreCreateMutex();
  }
  xSemaphoreTake(delayed_action_mutex, portMAX_DELAY);
  delayed_action_queue[output].pending = true;
  delayed_action_queue[output].turn_on = turn_on;
  delayed_action_queue[output].due_at_ms = millis() + delay_ms;
  xSemaphoreGive(delayed_action_mutex);
}

// Cancels any still-pending delayed action for a single output, regardless of direction. Call
// this before handling any live "set" command for that same output (whether the command itself
// ends up being immediate or newly delayed) - found via writing this feature's formal test suite:
// without this, an immediate command (delay_s == 0, e.g. the opposite direction from whichever
// one has a configured delay) would silently leave an earlier, now-contradicting delayed action
// for the same output still pending, which then fires later and undoes what the immediate command
// just did. schedule_delayed_action() already overwrites on its own for the "ends up delayed"
// case, so calling this unconditionally beforehand is a harmless no-op there - this only matters
// for the immediate path. Singular sibling of clear_delayed_action_queue() below (that one is for
// Master Power, which affects every output at once).
static void clear_delayed_action_for(PowerOutput output) {
  if (delayed_action_mutex == NULL || output < 0 || output >= POWER_OUTPUT_COUNT) return;
  xSemaphoreTake(delayed_action_mutex, portMAX_DELAY);
  delayed_action_queue[output].pending = false;
  xSemaphoreGive(delayed_action_mutex);
}

// Cancels every pending delayed action, regardless of output or direction. Call this whenever
// "all" is used (see handle_set_power_command below) - found via real hardware testing: without
// this, a delayed action scheduled shortly before a Master Power command would still fire later
// on its own, silently undoing what Master Power just did (e.g. Master Power Off, followed
// moments later by an unrelated output spontaneously turning back on once its stale delayed-on
// came due). Master Power is meant to be a decisive, complete action - nothing should be able to
// override it after the fact.
static void clear_delayed_action_queue() {
  if (delayed_action_mutex == NULL) return;
  xSemaphoreTake(delayed_action_mutex, portMAX_DELAY);
  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    delayed_action_queue[i].pending = false;
  }
  xSemaphoreGive(delayed_action_mutex);
}

void service_delayed_action_queue() {
  if (delayed_action_mutex == NULL) return; // nothing ever scheduled yet
  for (int i = 0; i < POWER_OUTPUT_COUNT; i++) {
    bool due = false, turn_on = false;
    xSemaphoreTake(delayed_action_mutex, portMAX_DELAY);
    if (delayed_action_queue[i].pending && millis() >= delayed_action_queue[i].due_at_ms) {
      delayed_action_queue[i].pending = false;
      due = true;
      turn_on = delayed_action_queue[i].turn_on;
    }
    xSemaphoreGive(delayed_action_mutex);
    // set_power_output() takes config_mutex/serial_mutex internally - call it outside
    // delayed_action_mutex's critical section, same reasoning as service_power_stagger_queue().
    if (due) {
      set_power_output((PowerOutput)i, turn_on);
    }
  }
}

bool get_power_output_state(PowerOutput output) {
  if (output >= 0 && output < POWER_OUTPUT_COUNT) {
    return power_output_states[output];
  }
  return false;
}

void service_power_stagger_queue() {
  PowerOutput output_to_enable = POWER_DC1; // placeholder, only used if should_enable is set true
  bool should_enable = false;

  xSemaphoreTake(stagger_mutex, portMAX_DELAY);
  if (stagger_queue_pos < stagger_queue_len && millis() >= stagger_due_at_ms) {
    output_to_enable = stagger_queue[stagger_queue_pos++];
    should_enable = true;

    xSemaphoreTake(config_mutex, portMAX_DELAY);
    unsigned long delay_ms = config.poweron_stagger_delay_ms;
    xSemaphoreGive(config_mutex);
    stagger_due_at_ms = millis() + delay_ms;
  }
  xSemaphoreGive(stagger_mutex);

  // Call set_power_output() outside the stagger_mutex critical section - it takes config_mutex
  // internally, and there's no need to hold stagger_mutex while that happens.
  if (should_enable) {
    unsigned long extra_delay_s = 0;
    xSemaphoreTake(config_mutex, portMAX_DELAY);
    extra_delay_s = config.switch_timing[output_to_enable].delay_on_s;
    xSemaphoreGive(config_mutex);

    if (extra_delay_s > 0) {
      // Stagger sequencing already advanced above regardless of this - only this one output's
      // actual enable is pushed further out by its own configured delay, on top of its place in
      // the stagger order.
      schedule_delayed_action(output_to_enable, true, extra_delay_s * 1000UL);
    } else {
      set_power_output(output_to_enable, true);
    }
  }
}