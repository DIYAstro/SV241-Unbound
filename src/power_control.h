#ifndef POWER_CONTROL_H
#define POWER_CONTROL_H

#include <Arduino.h>
#include <ArduinoJson.h>

// Enum to identify each power output
enum PowerOutput {
  POWER_DC1,
  POWER_DC2,
  POWER_DC3,
  POWER_DC4,
  POWER_DC5,
  POWER_USBC12,
  POWER_USB345,
  POWER_ADJ_CONV,
  POWER_PWM1,
  POWER_PWM2,
  POWER_OUTPUT_COUNT // Keep this last for array sizing
};

// Initialize GPIOs and set startup states
void setup_power_outputs();

// Set the state of a specific power output. Returns false (and sends its own {"error":...} line
// on Serial) if the output is disabled in config and `on` was true - the caller must not send a
// second response line of its own in that case, since the port only expects one response per
// command. Returns true otherwise (state applied, or a no-op "turn off" that's always allowed).
bool set_power_output(PowerOutput output, bool on);

// Get the name of a power output as a string
const char* get_power_output_name(PowerOutput output);

// Populate a JsonDocument with the current status of all power outputs
void get_power_status_json(JsonDocument& doc);

// Handle incoming JSON command for setting power outputs. Returns false if any individual output
// in the command was rejected (disabled in config) - its own {"error":...} line has already been
// sent on Serial in that case, and the caller must not follow up with a second response line.
bool handle_set_power_command(JsonVariant set_command);

// Get the current state of a specific power output
bool get_power_output_state(PowerOutput output);

// Processes one pending item (if due) from the staggered "all" master-power-on queue.
// Call periodically (e.g. every ~100ms) from a background task. Non-blocking.
void service_power_stagger_queue();

#endif // POWER_CONTROL_H
