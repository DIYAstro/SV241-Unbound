#include "diagnostics.h"

#include <Arduino.h>
#include <esp_system.h>
#include <esp_timer.h>
#include <freertos/FreeRTOS.h>
#include <freertos/task.h>
#include <string.h>

#define DIAG_MAX_TASKS 24
#define DIAG_WINDOW_MS 10000

static volatile int diag_core_busy[2] = {0, 0}; // 0.1 % units
static volatile uint32_t diag_stack_min = 0;    // bytes
static volatile bool diag_ready = false;
static int diag_reset_reason = 0;

struct DiagPrev {
  TaskHandle_t handle;
  uint32_t runtime;
};
static DiagPrev diag_prev[DIAG_MAX_TASKS];
static int diag_prev_count = 0;
static TaskStatus_t diag_status[DIAG_MAX_TASKS];

static int diag_tenths(float pct) {
  if (pct < 0) pct = 0;
  if (pct > 100) pct = 100;
  return (int)(pct * 10.0f + 0.5f);
}

// FreeRTOS truncates task names to 15 characters, so the application tasks are matched by prefix.
static bool diag_is_app_task(const char *name) {
  return strncmp(name, "PowerStagger", 12) == 0 || strncmp(name, "SensorUpdate", 12) == 0 ||
         strncmp(name, "SerialCommand", 13) == 0 || strncmp(name, "MemoryMonitor", 13) == 0;
}

static void diagnostics_task(void *pvParameters) {
  int64_t prev_wall = esp_timer_get_time();
  UBaseType_t n = uxTaskGetSystemState(diag_status, DIAG_MAX_TASKS, NULL);
  diag_prev_count = n;
  for (UBaseType_t i = 0; i < n; i++) {
    diag_prev[i].handle = diag_status[i].xHandle;
    diag_prev[i].runtime = diag_status[i].ulRunTimeCounter;
  }
  uint32_t stack_min = UINT32_MAX;

  for (;;) {
    vTaskDelay(pdMS_TO_TICKS(DIAG_WINDOW_MS));
    int64_t wall = esp_timer_get_time();
    float wall_delta = (float)(wall - prev_wall);
    prev_wall = wall;

    n = uxTaskGetSystemState(diag_status, DIAG_MAX_TASKS, NULL);
    float idle[2] = {0, 0};
    for (UBaseType_t i = 0; i < n; i++) {
      // The run-time counter is 32-bit microseconds and wraps after ~71 minutes, so only the
      // difference to the previous sample is used (unsigned subtraction survives the wrap).
      uint32_t prev_runtime = diag_status[i].ulRunTimeCounter; // task not seen before: delta 0
      for (int p = 0; p < diag_prev_count; p++) {
        if (diag_prev[p].handle == diag_status[i].xHandle) {
          prev_runtime = diag_prev[p].runtime;
          break;
        }
      }
      float pct = 100.0f * (float)(diag_status[i].ulRunTimeCounter - prev_runtime) / wall_delta;
      const char *name = diag_status[i].pcTaskName;

      if (strcmp(name, "IDLE0") == 0) idle[0] = pct;
      else if (strcmp(name, "IDLE1") == 0) idle[1] = pct;
      else if (diag_is_app_task(name) && diag_status[i].usStackHighWaterMark < stack_min) {
        stack_min = diag_status[i].usStackHighWaterMark;
      }
    }
    diag_core_busy[0] = diag_tenths(100.0f - idle[0]);
    diag_core_busy[1] = diag_tenths(100.0f - idle[1]);
    if (stack_min != UINT32_MAX) diag_stack_min = stack_min;
    diag_ready = true;

    diag_prev_count = n;
    for (UBaseType_t i = 0; i < n; i++) {
      diag_prev[i].handle = diag_status[i].xHandle;
      diag_prev[i].runtime = diag_status[i].ulRunTimeCounter;
    }
  }
}

void diagnostics_start() {
  diag_reset_reason = (int)esp_reset_reason();
  xTaskCreatePinnedToCore(diagnostics_task, "DiagnosticsTask", 4096, NULL, 1, NULL, 0);
}

void diagnostics_add_to_json(JsonDocument& doc) {
  if (diag_ready) {
    doc["c0"] = diag_core_busy[0];
    doc["c1"] = diag_core_busy[1];
    doc["sk"] = diag_stack_min;
  }
  // esp_timer_get_time() is a 64-bit microsecond counter - unlike millis() (49.7 days) it does not
  // wrap in any realistic uptime, and the seconds fit a uint32_t for ~136 years.
  doc["up"] = (uint32_t)(esp_timer_get_time() / 1000000);
  doc["rr"] = diag_reset_reason;
}
