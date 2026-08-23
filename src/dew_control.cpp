#include <Arduino.h>
#include <QuickPID.h>
#include <math.h>
#include <esp_task_wdt.h>

#include "dew_control.h"
#include "config_manager.h"
#include "hardware_pins.h"
#include "sensors.h" // For getting ambient sensor values

// --- Private State ---
static bool heater_enabled[MAX_DEW_HEATERS];
static int heater_power[MAX_DEW_HEATERS] = {0}; // Live power percentage (0-100)
static int ram_pwm_overrides[MAX_DEW_HEATERS] = {-1, -1}; // RAM overrides

// Converts a "Max PWM Duty" hardware safety limit (0-100%, the raw electrical duty cycle)
// into the equivalent "power %" ceiling in the pre-gamma-correction domain used by manual_power,
// PID output, etc. This is the inverse of get_corrected_duty_cycle()'s gamma curve.
// Truncates (never rounds up) so the resulting duty cycle never exceeds the configured limit.
// No dependency on anything declared later in this file, so it's safe to define this early -
// it's needed by get_heater_power() below, which sits above get_corrected_duty_cycle().
static inline int duty_limit_to_power_percent(int max_duty_percent) {
    max_duty_percent = constrain(max_duty_percent, 0, 100);
    if (max_duty_percent >= 100) return 100;
    if (max_duty_percent <= 0) return 0;
    const float gamma = 2.5f; // inverse of get_corrected_duty_cycle's 1/2.5
    float max_duty_ratio = max_duty_percent / 100.0f;
    return (int)(pow(max_duty_ratio, gamma) * 100.0f); // truncate = never exceeds limit
}

// --- Public Helper ---
int get_heater_power(int heater_index) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return 0;

    // If in Manual Mode and a RAM override is pending/active, report THAT value
    // to provide immediate feedback to the UI, rather than waiting for the control loop tick.
    // The override is clamped to the heater's max_duty_percent safety limit here too, so the
    // reported value never optimistically exceeds what the hardware will actually output.
    if (ram_pwm_overrides[heater_index] >= 0) {
        xSemaphoreTake(config_mutex, portMAX_DELAY);
        int mode = config.dew_heaters[heater_index].mode;
        int max_duty = config.dew_heaters[heater_index].max_duty_percent;
        xSemaphoreGive(config_mutex);
        if (mode == 0) {
            return min(ram_pwm_overrides[heater_index], duty_limit_to_power_percent(max_duty));
        }
    }

    return heater_power[heater_index];
}

void set_dew_heater_pwm_ram(int heater_index, int pwm) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return;
    ram_pwm_overrides[heater_index] = pwm;
}

// PID Controller variables
static float pid_setpoint[MAX_DEW_HEATERS];
static float pid_input[MAX_DEW_HEATERS];
static float pid_output[MAX_DEW_HEATERS];
// Instead of an array of pointers, we declare an array of PID objects.
// We initialize them with placeholder values, as the real values will be loaded from the configuration later.
static QuickPID heater_pids[MAX_DEW_HEATERS] = {
    QuickPID(&pid_input[0], &pid_output[0], &pid_setpoint[0]),
    QuickPID(&pid_input[1], &pid_output[1], &pid_setpoint[1])};

// PWM settings
const int PWM_FREQUENCY = 100; // 100 Hz. A good compromise for measurement while still being safe for MOSFETs.
const int PWM_RESOLUTION = 10; // 10-bit (0-1023). Increased resolution for more stable PWM output.
const int PWM_MAX = (1 << PWM_RESOLUTION) - 1;
const int HEATER_PINS[MAX_DEW_HEATERS] = {DEW_HEATER_1_PIN, DEW_HEATER_2_PIN};
// Note: Arduino-ESP32 core 3.x manages LEDC channel assignment internally per pin
// (ledcAttach/ledcWrite address the pin directly), so no explicit channel array is needed here.

// --- Task Handle ---
static TaskHandle_t dew_control_task_handle = NULL;

// --- Forward Declarations ---
void dew_control_task(void *pvParameters);
float calculate_dew_point(float temperature, float humidity);

// --- Public Functions ---

// Get the current mode of the dew heater (0=Manual, 1=Auto)
int get_dew_heater_mode(int heater_index) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return 0; // Default to manual/safe
    xSemaphoreTake(config_mutex, portMAX_DELAY);
    int mode = config.dew_heaters[heater_index].mode;
    xSemaphoreGive(config_mutex);
    return mode;
}

void setup_dew_heaters() {
    for (int i = 0; i < MAX_DEW_HEATERS; i++) {
        if (HEATER_PINS[i] == -1) continue; // Skip unused heaters

        // Configure the LEDC peripheral and attach it to the GPIO pin in one call (core 3.x API)
        ledcAttach(HEATER_PINS[i], PWM_FREQUENCY, PWM_RESOLUTION);

        xSemaphoreTake(config_mutex, portMAX_DELAY);
        DewHeaterConfig heater_config_copy = config.dew_heaters[i];
        xSemaphoreGive(config_mutex);

        // Configure the existing PID object.
        // Dynamic allocation with 'new' is removed to fix the memory leak.
        heater_pids[i].SetTunings(heater_config_copy.pid_kp, heater_config_copy.pid_ki, heater_config_copy.pid_kd);
        heater_pids[i].SetControllerDirection(QuickPID::Action::direct);
        // The PID now controls power percentage (0-100), not the raw PWM value.
        // This allows it to benefit from the gamma correction.
        heater_pids[i].SetOutputLimits(0, 100);
        heater_pids[i].SetMode(QuickPID::Control::automatic);

        // Set initial state
        set_dew_heater_state(i, heater_config_copy.enabled_on_startup);
    }

    // Create the control task
    xTaskCreatePinnedToCore(
        dew_control_task,
        "DewControlTask",
        4096,
        NULL,
        1,
        &dew_control_task_handle,
        1);
}

void set_dew_heater_state(int heater_index, bool enabled) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return;
    heater_enabled[heater_index] = enabled;

    if (!enabled) {
        ledcWrite(HEATER_PINS[heater_index], 0); // Turn off PWM
    }
}

bool get_dew_heater_state(int heater_index) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return false;
    return heater_enabled[heater_index];
}



// --- Helper Functions ---

static inline uint32_t get_corrected_duty_cycle(int power_percentage) {
    if (power_percentage <= 0) return 0;
    if (power_percentage >= 100) return PWM_MAX;
    
    // Use a calculated gamma curve instead of a lookup table.
    // This avoids specific problematic duty cycle values that the LUT might contain
    // and provides a smoother, more reliable output curve.
    // To linearize a power curve (P ~ V^2), the duty cycle needs to be corrected
    // with an exponent < 1. The previous attempts with gamma > 1 were incorrect.
    // We use the reciprocal of a gamma value. A display has gamma ~2.2, so we'd use 1/2.2.
    // After testing, a gamma of 2.2 is slightly too weak (power is ~11% too low).
    // A gamma of 2.8 was too strong. The ideal value lies in between.
    // We'll use 2.5 as the final value to center the power curve.
    const float gamma = 1.0 / 2.5;
    float power_ratio = power_percentage / 100.0f;
    float corrected_ratio = pow(power_ratio, gamma);
    return (uint32_t)(corrected_ratio * PWM_MAX);
}

// Centralized write path: clamps power_percentage to max_power_equiv (defense in depth - callers
// are expected to already have applied the appropriate limit, either via duty_limit_to_power_percent()
// for stateless modes or via the PID's own SetOutputLimits() for PID-based modes), updates the
// reported live power percentage, and writes the resulting duty cycle to the hardware.
static inline void set_heater_power_output(int heater_index, int power_percentage, int max_power_equiv) {
    power_percentage = constrain(power_percentage, 0, max_power_equiv);
    heater_power[heater_index] = power_percentage;
    uint32_t duty_cycle = get_corrected_duty_cycle(power_percentage);
    ledcWrite(HEATER_PINS[heater_index], duty_cycle);
}

// --- Control Task ---

void dew_control_task(void *pvParameters) {
    esp_task_wdt_add(NULL); // Register this task with the watchdog
    for (;;) {
        SensorValues sensor_values;
        get_sensor_values(sensor_values); // Get thread-safe copy of all sensor data

        float dew_point = calculate_dew_point(sensor_values.sht_temperature, sensor_values.sht_humidity);

        // Two-phase calculation to solve PID-Sync timing issue:
        // Phase 1: Calculate power for all non-follower heaters (modes 0, 1, 2, 4, 5)
        // Phase 2: Calculate power for follower heaters (mode 3)
        // This ensures followers always have up-to-date leader power values.

        // Phase 1: Non-followers
        for (int i = 0; i < MAX_DEW_HEATERS; i++) {
            if (!heater_enabled[i] || HEATER_PINS[i] == -1) {
                heater_power[i] = 0; // Store 0 if disabled
                continue; // Skip disabled or unused heaters
            }

            // Create a thread-safe local copy of the heater's config for this loop iteration
            DewHeaterConfig heater_config;
            xSemaphoreTake(config_mutex, portMAX_DELAY);
            heater_config = config.dew_heaters[i];
            xSemaphoreGive(config_mutex);

            // Skip followers in Phase 1 - they will be processed in Phase 2
            if (heater_config.mode == 3) {
                continue;
            }

            // --- Safety Check for Automatic Modes ---
            // Before running automatic modes, ensure the required sensor data is valid.
            bool sensor_data_valid = true;
            if (heater_config.mode == 1 || heater_config.mode == 4) { // PID Mode & Min Temp Mode
                if (isnan(dew_point) || isnan(sensor_values.ds18b20_temperature)) {
                    sensor_data_valid = false;
                }
            } else if (heater_config.mode == 2) { // Ambient Tracking Mode
                if (isnan(dew_point) || isnan(sensor_values.sht_temperature)) {
                    sensor_data_valid = false;
                }
            }

            if (!sensor_data_valid) {
                // A sensor required for this automatic mode is disconnected or invalid.
                // Turn off the heater as a safety measure.
                heater_power[i] = 0;
                ledcWrite(HEATER_PINS[i], 0);
                continue; // Skip to the next heater
            }
            // --- End Safety Check ---

            switch (heater_config.mode) {
                case 0: { // Manual Mode
                    // Use RAM override if set, otherwise config default
                    int power_percentage;
                    if (ram_pwm_overrides[i] >= 0) {
                        power_percentage = ram_pwm_overrides[i];
                    } else {
                        power_percentage = heater_config.manual_power;
                    }

                    set_heater_power_output(i, power_percentage, duty_limit_to_power_percent(heater_config.max_duty_percent));
                    break;
                }

                case 1: { // PID Mode
                    float lens_temp = sensor_values.ds18b20_temperature;
                    pid_input[i] = lens_temp;
                    pid_setpoint[i] = dew_point + heater_config.target_offset;

                    // Update PID tunings in case they changed
                    int max_power_equiv = duty_limit_to_power_percent(heater_config.max_duty_percent);
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    // Cap the PID's own output range to the max_duty_percent limit. This is essential -
                    // it lets QuickPID's internal anti-windup clamp the integral term to the real ceiling,
                    // instead of letting it wind up against an unreachable 100% and only clamping afterwards.
                    heater_pids[i].SetOutputLimits(0, max_power_equiv);

                    heater_pids[i].Compute(); // pid_output[i] is now a value from 0-max_power_equiv

                    int power_percentage = (int)pid_output[i];
                    set_heater_power_output(i, power_percentage, max_power_equiv);
                    break;
                }
                
                case 4: { // Minimum Temperature Mode
                    float lens_temp = sensor_values.ds18b20_temperature;
                    pid_input[i] = lens_temp;

                    // The setpoint is the HIGHER of the minimum temp or the dew point target
                    float dew_point_target = dew_point + heater_config.target_offset;
                    pid_setpoint[i] = max(heater_config.min_temp, dew_point_target);

                    // Update PID tunings in case they changed
                    int max_power_equiv = duty_limit_to_power_percent(heater_config.max_duty_percent);
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    // See Mode 1 (PID) comment: caps PID's own output range for correct anti-windup.
                    heater_pids[i].SetOutputLimits(0, max_power_equiv);

                    heater_pids[i].Compute();

                    int power_percentage = (int)pid_output[i];
                    set_heater_power_output(i, power_percentage, max_power_equiv);
                    break;
                }

                case 2: { // Ambient Tracking Mode
                    float ambient_temp = sensor_values.sht_temperature;
                    float delta = ambient_temp - dew_point;

                    float power_percentage = 0.0f;
                    // Check if the delta is within the ramp range
                    if (delta <= heater_config.end_delta) {
                        power_percentage = heater_config.max_power;
                    } else if (delta < heater_config.start_delta) {
                        // Linear interpolation between start_delta and end_delta
                        power_percentage = ((heater_config.start_delta - delta) / (heater_config.start_delta - heater_config.end_delta)) * heater_config.max_power;
                    }

                    // Clamp the value just in case
                    power_percentage = constrain(power_percentage, 0, heater_config.max_power);

                    set_heater_power_output(i, (int)power_percentage, duty_limit_to_power_percent(heater_config.max_duty_percent));
                    break;
                }

                case 5: { // Disabled Mode (Hidden)
                    heater_power[i] = 0;
                    ledcWrite(HEATER_PINS[i], 0);
                    break;
                }
            }
        }

        // Phase 2: Followers (mode 3) - now all leader powers are calculated
        for (int i = 0; i < MAX_DEW_HEATERS; i++) {
            if (!heater_enabled[i] || HEATER_PINS[i] == -1) {
                continue; // Already handled in Phase 1
            }

            // Get heater config with mutex protection
            DewHeaterConfig heater_config;
            xSemaphoreTake(config_mutex, portMAX_DELAY);
            heater_config = config.dew_heaters[i];
            int leader_index = 1 - i; // The other heater is the leader
            int leader_mode = config.dew_heaters[leader_index].mode;
            xSemaphoreGive(config_mutex);

            // Only process followers in Phase 2
            if (heater_config.mode != 3) {
                continue;
            }

            // PID-Sync Mode
            // Make sure the leader is actually in PID mode or MinTemp mode
            int follower_power_percentage;
            if (leader_mode == 1 || leader_mode == 4) {
                // leader_power already reflects the leader's OWN max_duty_percent clamp (applied
                // in Phase 1). The follower's max_duty_percent below is applied independently on
                // top of that, so leader and follower are fully separately configurable (e.g. a
                // 12V leader with no limit and a 5V follower with a low duty cap).
                float leader_power = (float)heater_power[leader_index];
                float follower_power = leader_power * heater_config.pid_sync_factor;

                follower_power_percentage = constrain((int)round(follower_power), 0, 100);
            } else {
                // If the leader is not in PID mode, we turn off for safety.
                follower_power_percentage = 0;
            }

            set_heater_power_output(i, follower_power_percentage, duty_limit_to_power_percent(heater_config.max_duty_percent));
        }

        esp_task_wdt_reset(); // Feed the watchdog
        vTaskDelay(pdMS_TO_TICKS(5000)); // Run every 5 seconds
    }
}


// --- Helper Functions ---

float calculate_dew_point(float temperature, float humidity) {
    if (humidity <= 0) return -273.15; // Avoid log(0)
    // Magnus formula
    const float a = 17.62;
    const float b = 243.12;
    float gamma = log(humidity / 100.0) + (a * temperature) / (b + temperature);
    return (b * gamma) / (a - gamma);
}
