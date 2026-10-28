#ifndef VOLTAGE_CONTROL_H
#define VOLTAGE_CONTROL_H

#include <Arduino.h>

// Sets up the PWM channel and pin for the adjustable converter
void setup_voltage_control();

// Sets the adjustable converter's output state (ON to preset voltage, or OFF)
void set_adjustable_converter_state(bool on);

// Sets the adjustable voltage target in RAM (temporarily)
void set_adjustable_voltage_ram(float voltage);

// Gets the current target voltage (RAM override or config preset)
float get_adjustable_voltage_target();

// Advances an in-progress ADJ_CONV soft-start ramp (see SwitchTimingConfig's soft_start_ms in
// config_manager.h) by one PWM duty step, if one is active and due. Call frequently (every
// 1-2ms, for a reasonably smooth ramp) from a background task - same cadence as
// service_dc_soft_start_ramps() in power_control.h, for the same non-blocking reasoning.
void service_adj_soft_start_ramp();

#endif // VOLTAGE_CONTROL_H
