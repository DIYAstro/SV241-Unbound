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
static int heater_power[MAX_DEW_HEATERS] = {0}; // Live power percentage (0-100), INCLUDING this heater's own max_duty_percent clamp - used for reporting/telemetry and hardware writes.
static int heater_demand[MAX_DEW_HEATERS] = {0}; // Raw demand computed by this heater's own control logic, BEFORE its own max_duty_percent clamp. Used as the sync basis for PID-Sync followers, so a leader's own hardware limit never leaks into what a follower is allowed to output.
static int ram_pwm_overrides[MAX_DEW_HEATERS] = {-1, -1}; // RAM overrides

// PWM settings
// Moved up (ahead of get_heater_power() below) because the duty-domain helper functions that
// follow depend on PWM_MAX, and get_heater_power() needs those helpers for its immediate
// RAM-override feedback path.
const int PWM_FREQUENCY = 100; // 100 Hz. A good compromise for measurement while still being safe for MOSFETs.
const int PWM_RESOLUTION = 10; // 10-bit (0-1023). Increased resolution for more stable PWM output.
const int PWM_MAX = (1 << PWM_RESOLUTION) - 1;

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

// Converts a "Max PWM Duty" hardware safety limit (0-100%) directly into a raw duty-cycle
// ceiling (0-PWM_MAX), linearly - no gamma curve involved. This is the actual electrical
// quantity xd is documented to mean. Clamping here (rather than in the coarse 0-100
// power-percent domain) avoids small-but-nonzero xd values rounding down to a power-percent
// ceiling of 0, which would permanently disable the heater even though some real duty cycle
// was configured and physically achievable. Truncates, so the ceiling never exceeds xd%.
static inline uint32_t duty_limit_to_raw_duty(int max_duty_percent) {
    max_duty_percent = constrain(max_duty_percent, 0, 100);
    if (max_duty_percent >= 100) return PWM_MAX;
    if (max_duty_percent <= 0) return 0;
    return (uint32_t)(max_duty_percent / 100.0f * PWM_MAX);
}

// Inverse of get_corrected_duty_cycle(): given a raw duty value, returns the "power %" that
// would have produced it. Used only to keep the reported power percentage truthful when a
// duty-domain clamp actually changed what's being output.
static inline int raw_duty_to_power_percent(uint32_t duty) {
    if (duty == 0) return 0;
    if (duty >= (uint32_t)PWM_MAX) return 100;
    const float gamma = 2.5f; // inverse of get_corrected_duty_cycle's 1/2.5
    float duty_ratio = duty / (float)PWM_MAX;
    return (int)(pow(duty_ratio, gamma) * 100.0f);
}

// Single source of truth for turning a desired power percentage and a heater's own
// max_duty_percent limit into (a) the raw duty cycle to actually write to hardware, and (b)
// the power percentage to report/store - exact when nothing was clamped, truthfully reflects
// the real clamped output otherwise (never optimistic). Used both by the control loop's write
// path and by get_heater_power()'s immediate RAM-override feedback, so both stay consistent.
struct HeaterOutput {
    uint32_t duty;
    int reported_power_percent;
};

static inline HeaterOutput compute_heater_output(int power_percentage, int max_duty_percent) {
    power_percentage = constrain(power_percentage, 0, 100);
    uint32_t desired_duty = get_corrected_duty_cycle(power_percentage);
    uint32_t duty_ceiling = duty_limit_to_raw_duty(max_duty_percent);
    uint32_t final_duty = min(desired_duty, duty_ceiling);

    HeaterOutput out;
    out.duty = final_duty;
    out.reported_power_percent = (final_duty < desired_duty)
        ? raw_duty_to_power_percent(final_duty)
        : power_percentage; // unclamped: report the exact requested value, no round-trip rounding
    return out;
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
            return compute_heater_output(ram_pwm_overrides[heater_index], max_duty).reported_power_percent;
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

const int HEATER_PINS[MAX_DEW_HEATERS] = {DEW_HEATER_1_PIN, DEW_HEATER_2_PIN};
// Note: Arduino-ESP32 core 3.x manages LEDC channel assignment internally per pin
// (ledcAttach/ledcWrite address the pin directly), so no explicit channel array is needed here.

// --- Current-limit ramp state ---
// Set once per dew_control_task() tick (see compute_current_limit_cap()) from the shared
// total-current reading, then read back by is_current_limit_active() for reporting (sensors.cpp,
// live status / telemetry). A plain bool shared across tasks without a mutex matches how
// heater_power[]/heater_enabled[] above are already read cross-task - single-word writes/reads
// on this target don't tear, and a status flag has no strict consistency requirement.
static bool current_limit_active_flag = false;

// Damped output of compute_current_limit_cap() - persists across ticks, see that function's
// doc comment for why. Starts unthrottled.
static float smoothed_current_limit_cap = 100.0f;

// The ramp starts this many amps below current_limit_amps and reaches 0% duty exactly at
// current_limit_amps - same linear-ramp shape as Mode 2 (Ambient Tracking)'s start_delta/
// end_delta below. A hard on/off at the limit would oscillate (throttle -> current drops ->
// unthrottle -> current rises -> repeat); the ramp instead settles at whatever duty keeps
// current near the limit.
const float CURRENT_LIMIT_RAMP_WIDTH_A = 1.0f;

// Returns the box-wide dynamic duty-cycle ceiling (0-100) for the current tick, derived from the
// measured total input current vs. config.current_limit_amps. 100 (no-op) whenever the feature
// is disabled or misconfigured (limit <= 0) - deliberately an immediate, undamped return, so
// disabling the feature is exactly the instant no-op it was before this smoothing was added.
// Both heaters call this with the same measured_current_a on the same tick, so they throttle
// and relax together rather than fighting over a shared current budget.
static int compute_current_limit_cap(float measured_current_a) {
    xSemaphoreTake(config_mutex, portMAX_DELAY);
    bool enabled = config.current_limit_enabled;
    float limit = config.current_limit_amps;
    xSemaphoreGive(config_mutex);

    if (!enabled || limit <= 0) return 100;

    float ramp_start = limit - CURRENT_LIMIT_RAMP_WIDTH_A;
    int target_cap;
    if (measured_current_a <= ramp_start) {
        target_cap = 100;
    } else if (measured_current_a >= limit) {
        target_cap = 0;
    } else {
        float fraction = (limit - measured_current_a) / CURRENT_LIMIT_RAMP_WIDTH_A; // 1..0
        target_cap = (int)(fraction * 100.0f);
    }

    // Damping: real-hardware testing under actual heater load (~3.4A swing against this 1A-wide
    // ramp) showed that jumping straight to a freshly computed target_cap every 5s tick, with no
    // memory of the previous value, produces a sustained limit-cycle oscillation - current (and
    // the PWM duty that caused it) changes within the same tick, much faster than the 5s sample
    // rate, so a memory-less proportional response has nothing damping it. Widening the ramp
    // instead would only mask this for whatever specific heater happened to be tested with - the
    // right width depends on that heater's own current-per-duty slope, which varies by
    // installation - so only moving partway toward the target each tick, regardless of how large
    // a swing the connected heater causes, actually generalizes.
    const float SMOOTHING_ALPHA = 0.3f; // lower = smoother/slower, higher = snappier/twitchier
    smoothed_current_limit_cap += SMOOTHING_ALPHA * (target_cap - smoothed_current_limit_cap);
    return (int)smoothed_current_limit_cap;
}

// See dew_control.h's doc comment.
bool is_current_limit_active() {
    return current_limit_active_flag;
}

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

// Centralized write path: derives the actual duty cycle and the truthful reported power
// percentage from compute_heater_output(), stores the latter, and writes the former to hardware.
static inline void set_heater_power_output(int heater_index, int power_percentage, int max_duty_percent) {
    HeaterOutput out = compute_heater_output(power_percentage, max_duty_percent);
    heater_power[heater_index] = out.reported_power_percent;
    ledcWrite(HEATER_PINS[heater_index], out.duty);
}

// --- Control Task ---

void dew_control_task(void *pvParameters) {
    esp_task_wdt_add(NULL); // Register this task with the watchdog
    for (;;) {
        SensorValues sensor_values;
        get_sensor_values(sensor_values); // Get thread-safe copy of all sensor data

        float dew_point = calculate_dew_point(sensor_values.sht_temperature, sensor_values.sht_humidity);

        // Box-wide current-limit ceiling for this tick - combined (min()) with each heater's own
        // static max_duty_percent at every set_heater_power_output() call below.
        // sensor_values.ina_current is in mA (see sensors.cpp's final_current_mA), but
        // current_limit_amps is user-facing and documented in A - convert here, once, at the
        // one call site, rather than inside compute_current_limit_cap() where "measured_current"
        // could otherwise be silently re-misread as already being in the right unit again later.
        int current_limit_cap = compute_current_limit_cap(sensor_values.ina_current / 1000.0f);
        current_limit_active_flag = (current_limit_cap < 100);

        // Two-phase calculation to solve PID-Sync timing issue:
        // Phase 1: Calculate power for all non-follower heaters (modes 0, 1, 2, 4, 5)
        // Phase 2: Calculate power for follower heaters (mode 3)
        // This ensures followers always have up-to-date leader power values.

        // Phase 1: Non-followers
        for (int i = 0; i < MAX_DEW_HEATERS; i++) {
            if (!heater_enabled[i] || HEATER_PINS[i] == -1) {
                heater_power[i] = 0; // Store 0 if disabled
                heater_demand[i] = 0;
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
                heater_demand[i] = 0;
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

                    heater_demand[i] = power_percentage;
                    set_heater_power_output(i, power_percentage, min(heater_config.max_duty_percent, current_limit_cap));
                    break;
                }

                case 1: { // PID Mode
                    float lens_temp = sensor_values.ds18b20_temperature;
                    pid_input[i] = lens_temp;
                    pid_setpoint[i] = dew_point + heater_config.target_offset;

                    // Update PID tunings in case they changed
                    heater_pids[i].SetTunings(heater_config.pid_kp, heater_config.pid_ki, heater_config.pid_kd);
                    // Keep the PID's own output range fixed at its natural 0-100 (matching how
                    // kp/ki/kd were tuned), rather than reducing it to max_duty_percent. That keeps
                    // pid_output[i] a meaningful "raw demand" signal, decoupled from this heater's
                    // own hardware limit - PID-Sync followers need that raw demand (see Phase 2
                    // below), not a value already crushed by the leader's own max_duty_percent.
                    // The hardware safety clamp is still applied unconditionally at the write stage
                    // via set_heater_power_output() below, so this never risks exceeding max_duty_percent.
                    heater_pids[i].SetOutputLimits(0, 100);

                    heater_pids[i].Compute(); // pid_output[i] is the PID's raw 0-100 demand

                    int power_percentage = (int)pid_output[i];
                    heater_demand[i] = constrain(power_percentage, 0, 100);
                    set_heater_power_output(i, power_percentage, min(heater_config.max_duty_percent, current_limit_cap));
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
                    // See Mode 1 (PID) comment: keep the PID's own output range fixed at 0-100 so
                    // pid_output[i] stays a meaningful raw demand for PID-Sync followers; the
                    // hardware safety clamp is applied separately below via set_heater_power_output().
                    heater_pids[i].SetOutputLimits(0, 100);

                    heater_pids[i].Compute();

                    int power_percentage = (int)pid_output[i];
                    heater_demand[i] = constrain(power_percentage, 0, 100);
                    set_heater_power_output(i, power_percentage, min(heater_config.max_duty_percent, current_limit_cap));
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

                    heater_demand[i] = (int)power_percentage;
                    set_heater_power_output(i, (int)power_percentage, min(heater_config.max_duty_percent, current_limit_cap));
                    break;
                }

                case 5: { // Disabled Mode (Hidden)
                    heater_power[i] = 0;
                    heater_demand[i] = 0;
                    ledcWrite(HEATER_PINS[i], 0);
                    break;
                }

                default: {
                    // Mode is validated by the Vue UI dropdown, but nothing enforces that on the
                    // wire - a raw API call can set an out-of-range value. Without this, none of
                    // the cases above run and the output keeps whatever duty cycle it last had,
                    // indefinitely, with no automatic shutoff. Fail safe: turn the output off,
                    // same as Disabled Mode.
                    heater_power[i] = 0;
                    heater_demand[i] = 0;
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
                // Use heater_demand (the leader's raw PID output, BEFORE the leader's own
                // max_duty_percent clamp), not heater_power (the leader's already-clamped actual
                // output) - otherwise a low max_duty_percent on the leader would silently crush
                // the follower too, even if the follower has no limit of its own. The follower's
                // own max_duty_percent is applied independently below, so leader and follower are
                // fully separately configurable in both directions (e.g. a 12V leader with no
                // limit and a 5V follower with a low duty cap, or vice versa).
                float leader_power = (float)heater_demand[leader_index];
                float follower_power = leader_power * heater_config.pid_sync_factor;

                follower_power_percentage = constrain((int)round(follower_power), 0, 100);
            } else {
                // If the leader is not in PID mode, we turn off for safety.
                follower_power_percentage = 0;
            }

            heater_demand[i] = follower_power_percentage;
            set_heater_power_output(i, follower_power_percentage, min(heater_config.max_duty_percent, current_limit_cap));
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
