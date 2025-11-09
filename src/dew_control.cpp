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

// PID Controller variables
static float pid_setpoint[MAX_DEW_HEATERS];
static float pid_input[MAX_DEW_HEATERS];
static float pid_output[MAX_DEW_HEATERS];
// Statt einem Array von Pointern deklarieren wir ein Array von PID-Objekten.
// Wir initialisieren sie mit Platzhalterwerten, da die echten Werte später aus der Konfiguration geladen werden.
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

void setup_dew_heaters() {
    for (int i = 0; i < MAX_DEW_HEATERS; i++) {
        if (HEATER_PINS[i] == -1) continue; // Skip unused heaters

        // Configure PWM Channel
        ledcSetup(HEATER_LEDC_CHANNELS[i], PWM_FREQUENCY, PWM_RESOLUTION);
        ledcAttachPin(HEATER_PINS[i], HEATER_LEDC_CHANNELS[i]);

        xSemaphoreTake(config_mutex, portMAX_DELAY);
        DewHeaterConfig heater_config_copy = config.dew_heaters[i];
        xSemaphoreGive(config_mutex);

        // Konfiguriere das bereits existierende PID-Objekt.
        // Die dynamische Allokation mit 'new' wird entfernt, um das Speicherleck zu beheben.
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

int get_heater_power(int heater_index) {
    if (heater_index < 0 || heater_index >= MAX_DEW_HEATERS) return 0;
    return heater_power[heater_index];
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


            switch (heater_config.mode) {
                case 0: { // Manual Mode
                    heater_power[i] = heater_config.manual_power;
                    uint32_t duty_cycle = get_corrected_duty_cycle(heater_power[i]);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
                    break;
                }

                case 1: { // PID Mode
                    // Use the dedicated lens temperature sensor
                    float lens_temp = sensor_values.ds18b20_temperature;

                    pid_input[i] = lens_temp;
                    pid_setpoint[i] = dew_point + heater_config.target_offset;
                    
                    // Update PID tunings in case they changed
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    
                    heater_pids[i].Compute(); // pid_output[i] is now a value from 0-100

                    // The output of the PID is the desired power percentage.
                    int power_percentage = (int)pid_output[i];
                    power_percentage = constrain(power_percentage, 0, 100); // Clamp for safety

                    heater_power[i] = power_percentage; // Store the live power percentage

                    // Get the gamma-corrected duty cycle for the calculated power.
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

                case 3: { // PID-Sync Mode
                    int leader_index = 1 - i; // Der andere Heizer ist der Leader
                    
                    // Sicherstellen, dass der Leader tatsächlich im PID-Modus ist
                    if (config.dew_heaters[leader_index].mode == 1) {
                        float leader_power = (float)heater_power[leader_index];
                        float follower_power = leader_power * heater_config.pid_sync_factor;
                        
                        heater_power[i] = constrain((int)round(follower_power), 0, 100);
                    } else {
                        // Wenn der Leader nicht im PID-Modus ist, schalten wir sicherheitshalber ab.
                        heater_power[i] = 0;
                    }

                    uint32_t duty_cycle = get_corrected_duty_cycle(heater_power[i]);
                    ledcWrite(HEATER_LEDC_CHANNELS[i], duty_cycle);
                    break;
                }
            }
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
