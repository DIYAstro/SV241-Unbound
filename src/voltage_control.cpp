#include "voltage_control.h"
#include "config_manager.h"
#include "hardware_pins.h"
#include "power_control.h" // POWER_ADJ_CONV

// LEDC (PWM) settings
// Note: Arduino-ESP32 core 3.x manages LEDC channel assignment internally per pin
// (ledcAttach/ledcWrite address the pin directly), so no explicit channel number is needed here.
#define LEDC_FREQUENCY  50000 // 50 kHz - best balance within SC8903 VPWM range (20-100 kHz)
#define LEDC_RESOLUTION 8      // 8-bit resolution (0-255) = ~59mV steps, sufficient for voltage control

// SC8903 Calibration:
// The SC8903 exhibits a nonlinear offset error that varies with voltage.
// Measured offsets show ~0.5V error in the mid range, ~0.4V at 1V, and ~0.15V at 15V.
// This lookup table provides correction values based on empirical measurements.
struct CalibrationPoint {
  float desired_voltage;  // What we want to output
  float correction;       // How much to subtract from desired to get actual output
};

// Calibration data points (fine-tuned for Nominal+0.1V target)
// Target: Each voltage should output at Nominal+0.1V (e.g., 5V → 5.1V)
static const CalibrationPoint calibration_table[] = {
  {1.0f,  0.306f},  // 1V → target 1.1V (was 1.34V, corrected)
  {2.0f,  0.390f},  // 2V → target 2.1V (was 1.95V, corrected)
  {3.0f,  0.461f},  // 3V → target 3.1V (was 3.52V, corrected)
  {4.0f,  0.406f},  // 4V → target 4.1V (was 4.43V, corrected)
  {5.0f,  0.406f},  // 5V → target 5.1V (was 5.43V, corrected)
  {6.0f,  0.397f},  // 6V → target 6.1V (was 6.42V, corrected)
  {7.0f,  0.404f},  // 7V → target 7.1V (was 7.43V, corrected)
  {8.0f,  0.400f},  // 8V → target 8.1V (was 8.43V, corrected)
  {9.0f,  0.388f},  // 9V → target 9.1V (was 9.42V, corrected)
  {10.0f, 0.370f},  // 10V → target 10.1V (was 10.38V, corrected)
  {11.0f, 0.330f},  // 11V → target 11.1V (was 10.89V, corrected)
  {12.0f, 0.440f},  // 12V → target 12.1V (was 12.49V, corrected)
  {13.0f, 0.427f},  // 13V → target 13.1V (was 12.97V, corrected)
  {14.0f, 0.464f},  // 14V → target 14.1V (was 13.95V, corrected)
  {15.0f, 0.150f}   // 15V → target 15.1V (was 15.11V, perfect!)
};
static const int calibration_points = sizeof(calibration_table) / sizeof(CalibrationPoint);

// Linear interpolation to find correction value for any voltage
float get_voltage_correction(float desired_voltage) {
  // Clamp to table range
  if (desired_voltage <= calibration_table[0].desired_voltage) {
    return calibration_table[0].correction;
  }
  if (desired_voltage >= calibration_table[calibration_points - 1].desired_voltage) {
    return calibration_table[calibration_points - 1].correction;
  }
  
  // Find the two points to interpolate between
  for (int i = 0; i < calibration_points - 1; i++) {
    if (desired_voltage >= calibration_table[i].desired_voltage && 
        desired_voltage <= calibration_table[i + 1].desired_voltage) {
      
      float v1 = calibration_table[i].desired_voltage;
      float v2 = calibration_table[i + 1].desired_voltage;
      float c1 = calibration_table[i].correction;
      float c2 = calibration_table[i + 1].correction;
      
      // Linear interpolation: correction = c1 + (c2 - c1) * (v - v1) / (v2 - v1)
      float ratio = (desired_voltage - v1) / (v2 - v1);
      return c1 + (c2 - c1) * ratio;
    }
  }
  
  // Fallback (should never reach here due to clamping)
  return 0.5f;
}

// RAM-only target voltage override. -1.0 means "use config".
static float ram_voltage_target = -1.0f;

// --- ADJ_CONV soft-start ramp state (async, ticked via service_adj_soft_start_ramp()) ---
// Same non-blocking reasoning as DcSoftStartRamp in power_control.cpp - a blocking delay() loop
// here would stall the serial command task for the ramp's full duration, which can exceed the Go
// proxy's ~3s default command timeout and cause a duplicate ramp via its retry logic. See that
// struct's doc comment for the full story (found via real hardware testing). Duty is likewise
// computed from elapsed time (start_ms/duration_ms), not a fixed per-tick increment - see
// DcSoftStartRamp's doc comment for why a per-tick counter can't actually honor a short configured
// duration once the tick interval (power_stagger_task, 20ms back then, now 1ms) exceeds the per-step delay it
// implies (also found via real hardware testing - every ramp took ~5.1s regardless of
// soft_start_ms).
struct AdjSoftStartRamp {
  bool active;
  uint32_t target_duty;
  unsigned long start_ms;
  unsigned long duration_ms;
};
static AdjSoftStartRamp adj_ramp = {false, 0, 0, 0};
static SemaphoreHandle_t adj_ramp_mutex = NULL;

void setup_voltage_control() {
  // Configure the LEDC peripheral and attach it to the GPIO pin in one call (core 3.x API).
  // Applying ADJ_CONV's actual startup state (on/off, delay_on_s, soft-start ramp) happens later,
  // uniformly with every other output, from power_control.cpp's setup_power_outputs() - it used to
  // happen right here instead, which meant ADJ_CONV always powered up first, ahead of (and
  // potentially overlapping with) DC1-5's own staggered boot sequence, with nothing coordinating
  // the two. Folding it into the same staggered loop fixes that - see that function's doc comments.
  ledcAttach(ADJUSTABLE_CONVERTER_PIN, LEDC_FREQUENCY, LEDC_RESOLUTION);

  // On startup, we always respect the config preset, so ensure any stale RAM override is cleared.
  ram_voltage_target = -1.0f;
}

void set_adjustable_converter_state(bool on) {
  if (on) {
    float target_v = 0.0f;
    
    // Check for RAM override first
    if (ram_voltage_target >= 0.0f) {
        target_v = ram_voltage_target;
    } else {
        xSemaphoreTake(config_mutex, portMAX_DELAY);
        target_v = config.adj_conv_preset_v;
        xSemaphoreGive(config_mutex);
    }
    
    // Clamp to max voltage safety limit
    float desired_voltage = min(target_v, (float)ADJUSTABLE_CONVERTER_MAX_VOLTAGE);
    if (desired_voltage < 0.0f) desired_voltage = 0.0f;

    // Apply calibration correction to compensate for SC8903 output error
    float correction = get_voltage_correction(desired_voltage);
    float corrected_voltage = desired_voltage - correction;
    
    // Ensure corrected voltage doesn't go negative
    if (corrected_voltage < 0.0f) corrected_voltage = 0.0f;

    // SC8903 linear voltage control: VOUT = VOUT_SET × Duty Cycle
    // Therefore: Duty Cycle = VOUT / VOUT_SET
    // We use the corrected voltage to get the actual desired output
    uint32_t max_duty = (1 << LEDC_RESOLUTION) - 1; // 255 for 8-bit
    uint32_t duty_cycle = (corrected_voltage / ADJUSTABLE_CONVERTER_MAX_VOLTAGE) * max_duty;

    // Soft-start (see SwitchTimingConfig's doc comment): ramp 0 up to the computed target duty
    // over soft_start_ms, instead of jumping straight there. 0 = instant (the default, unchanged
    // behavior). Deliberately ramps to duty_cycle, not always to max_duty - a low preset voltage
    // (e.g. 5V) should ramp to its own, correspondingly lower target duty. Non-blocking - see
    // AdjSoftStartRamp's doc comment for why (same reasoning as DcSoftStartRamp).
    unsigned long soft_start_ms;
    xSemaphoreTake(config_mutex, portMAX_DELAY);
    soft_start_ms = config.switch_timing[POWER_ADJ_CONV].soft_start_ms;
    xSemaphoreGive(config_mutex);

    if (adj_ramp_mutex == NULL) {
      adj_ramp_mutex = xSemaphoreCreateMutex();
    }

    // This runs for EVERY "on" request, not just a fresh power-up. Already on (set_power_output() only
    // updates its state after this function returns, so this is still the previous one - false at boot
    // and after every Off) means a live voltage change ({"set":{"adj":<V>}}, or any config save from
    // the Web UI, which always carries "av" - see adj_voltage_changed in main.cpp) or a repeated On.
    // Restarting the ramp from duty 0 there would drop the output toward 0 V and ramp it back up, so
    // instead move a still-running ramp's target, or step straight to the new duty once it is done.
    // The ledcWrite() stays inside adj_ramp_mutex so the ramp tick can't overwrite it with a stale
    // duty (the 50 kHz PWM makes it block for microseconds only).
    const bool already_on = get_power_output_state(POWER_ADJ_CONV);

    if (soft_start_ms > 0 && duty_cycle > 0 && already_on) {
      xSemaphoreTake(adj_ramp_mutex, portMAX_DELAY);
      if (adj_ramp.active) {
        adj_ramp.target_duty = duty_cycle;
      } else {
        ledcWrite(ADJUSTABLE_CONVERTER_PIN, duty_cycle);
      }
      xSemaphoreGive(adj_ramp_mutex);
    } else if (soft_start_ms > 0 && duty_cycle > 0) {
      xSemaphoreTake(adj_ramp_mutex, portMAX_DELAY);
      adj_ramp = {true, duty_cycle, millis(), soft_start_ms};
      xSemaphoreGive(adj_ramp_mutex);
      ledcWrite(ADJUSTABLE_CONVERTER_PIN, 0); // service_adj_soft_start_ramp() takes it from here
    } else {
      if (adj_ramp_mutex != NULL) {
        xSemaphoreTake(adj_ramp_mutex, portMAX_DELAY);
        adj_ramp.active = false;
        xSemaphoreGive(adj_ramp_mutex);
      }
      ledcWrite(ADJUSTABLE_CONVERTER_PIN, duty_cycle);
    }
  } else {
    // Cancel any in-progress ramp - a still-active ramp must not keep stepping the output back up
    // after we've just been told to turn it off.
    if (adj_ramp_mutex != NULL) {
      xSemaphoreTake(adj_ramp_mutex, portMAX_DELAY);
      adj_ramp.active = false;
      xSemaphoreGive(adj_ramp_mutex);
    }
    // Set duty cycle to 0 to turn the output off
    ledcWrite(ADJUSTABLE_CONVERTER_PIN, 0);
  }
}

void service_adj_soft_start_ramp() {
  if (adj_ramp_mutex == NULL) return; // nothing ever started a ramp yet

  bool do_write = false;
  uint32_t duty = 0;
  unsigned long now = millis();

  xSemaphoreTake(adj_ramp_mutex, portMAX_DELAY);
  if (adj_ramp.active) {
    unsigned long elapsed = now - adj_ramp.start_ms;
    do_write = true;
    if (elapsed >= adj_ramp.duration_ms) {
      duty = adj_ramp.target_duty;
      adj_ramp.active = false; // ramp complete
    } else {
      duty = (uint32_t)(((float)elapsed / (float)adj_ramp.duration_ms) * adj_ramp.target_duty);
    }
  }
  xSemaphoreGive(adj_ramp_mutex);

  // Same known, accepted race window as service_dc_soft_start_ramps() in power_control.cpp (read
  // `active` under the mutex, write the duty only after releasing it - an Off landing exactly in
  // between could be overwritten by this stale write). Details and the fix, should it ever be
  // needed, are documented there; the converter's 50 kHz PWM makes the ledcWrite() block even shorter.
  if (do_write) {
    ledcWrite(ADJUSTABLE_CONVERTER_PIN, duty);
  }
}

void set_adjustable_voltage_ram(float voltage) {
    if (voltage < 0.0f) voltage = 0.0f;
    if (voltage > ADJUSTABLE_CONVERTER_MAX_VOLTAGE) voltage = ADJUSTABLE_CONVERTER_MAX_VOLTAGE;
    
    ram_voltage_target = voltage;
    
    // If currently on, apply immediately
    // We need to know if it's currently on? 
    // Usually power_control tracks state. We can just re-apply 'true' if we assume it might be on,
    // or we can let the caller handle it. 
    // Better: The caller (power_control) knows the state. But for convenience, let's just trigger update if ON?
    // voltage_control doesn't track ON/OFF state explicitly (stateless function).
    // So the caller must call set_adjustable_converter_state(true) to apply.
}

float get_adjustable_voltage_target() {
    float v = 0.0f;
    if (ram_voltage_target >= 0.0f) {
        v = ram_voltage_target;
    } else {
        xSemaphoreTake(config_mutex, portMAX_DELAY);
        v = config.adj_conv_preset_v;
        xSemaphoreGive(config_mutex);
    }
    return v;
}