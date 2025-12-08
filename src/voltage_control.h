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

#endif // VOLTAGE_CONTROL_H
