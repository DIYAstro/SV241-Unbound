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

// --- Public Helper ---
int get_heater_power(int heater_index) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return 0;

    // If in Manual Mode and a RAM override is pending/active, report THAT value 
    // to provide immediate feedback to the UI, rather than waiting for the PID loop.
    if (get_dew_heater_mode(heater_index) == 0 && ram_pwm_overrides[heater_index] >= 0) {
        return ram_pwm_overrides[heater_index];
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
const int HEATER_LEDC_CHANNELS[MAX_DEW_HEATERS] = {2, 3}; // Use LEDC channels 2 and 3
const int HEATER_PINS[MAX_DEW_HEATERS] = {DEW_HEATER_1_PIN, DEW_HEATER_2_PIN};

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

        // Configure PWM Channel
        ledcSetup(HEATER_LEDC_CHANNELS[i], PWM_FREQUENCY, PWM_RESOLUTION);
        ledcAttachPin(HEATER_PINS[i], HEATER_LEDC_CHANNELS[i]);

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
        ledcWrite(HEATER_LEDC_CHANNELS[heater_index], 0); // Turn off PWM
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
                ledcWrite(HEATER_LEDC_CHANNELS[i], 0);
                continue; // Skip to the next heater
            }
            // --- End Safety Check ---

            switch (heater_config.mode) {
                case 0: { // Manual Mode
                    // Use RAM override if set, otherwise config default
                    if (ram_pwm_overrides[i] >= 0) {
                        heater_power[i] = ram_pwm_overrides[i];
                    } else {
                        heater_power[i] = heater_config.manual_power;
                    }
                    
                    uint32_t duty_cycle = get_corrected_duty_cycle(heater_power[i]);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
                    break;
                }

                case 1: { // PID Mode
                    float lens_temp = sensor_values.ds18b20_temperature;
                    pid_input[i] = lens_temp;
                    pid_setpoint[i] = dew_point + heater_config.target_offset;
                    
                    // Update PID tunings in case they changed
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    
                    heater_pids[i].Compute(); // pid_output[i] is now a value from 0-100

                    int power_percentage = (int)pid_output[i];
                    power_percentage = constrain(power_percentage, 0, 100);
                    heater_power[i] = power_percentage;

                    uint32_t duty_cycle = get_corrected_duty_cycle(power_percentage);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
                    break;
                }
                
                case 4: { // Minimum Temperature Mode
                    float lens_temp = sensor_values.ds18b20_temperature;
                    pid_input[i] = lens_temp;

                    // The setpoint is the HIGHER of the minimum temp or the dew point target
                    float dew_point_target = dew_point + heater_config.target_offset;
                    pid_setpoint[i] = max(heater_config.min_temp, dew_point_target);
                    
                    // Update PID tunings in case they changed
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    
                    heater_pids[i].Compute();

                    int power_percentage = (int)pid_output[i];
                    power_percentage = constrain(power_percentage, 0, 100);
                    heater_power[i] = power_percentage;

                    uint32_t duty_cycle = get_corrected_duty_cycle(power_percentage);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
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
                    heater_power[i] = (int)power_percentage;

                    uint32_t duty_cycle = get_corrected_duty_cycle((int)power_percentage);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
                    break;
                }

                case 5: { // Disabled Mode (Hidden)
                    heater_power[i] = 0;
                    ledcWrite(HEATER_LEDC_CHANNELS[i], 0);
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
            if (leader_mode == 1 || leader_mode == 4) {
                float leader_power = (float)heater_power[leader_index];
                float follower_power = leader_power * heater_config.pid_sync_factor;
                
                heater_power[i] = constrain((int)round(follower_power), 0, 100);
            } else {
                // If the leader is not in PID mode, we turn off for safety.
                heater_power[i] = 0;
            }

            uint32_t duty_cycle = get_corrected_duty_cycle(heater_power[i]);
            ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
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
