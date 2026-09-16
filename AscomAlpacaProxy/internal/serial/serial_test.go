package serial

import (
	"bytes"
	"log"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"testing"
)

// withSafetyConditions sets config.Get().SafetyMonitorConditions for the duration of a test and
// restores the previous value afterward, so these tests don't leak config state into others.
func withSafetyConditions(t *testing.T, conditions ...config.SafetyCondition) {
	t.Helper()
	conf := config.Get()
	orig := conf.SafetyMonitorConditions
	conf.SafetyMonitorConditions = conditions
	t.Cleanup(func() { conf.SafetyMonitorConditions = orig })
}

// resetStatus clears the package-level Status cache before a test and restores whatever was
// there beforehand once the test finishes, so these tests don't leak state into each other.
func resetStatus() func() {
	Status.Lock()
	orig := Status.Data
	Status.Data = nil
	Status.Unlock()
	return func() {
		Status.Lock()
		Status.Data = orig
		Status.Unlock()
	}
}

// captureLog redirects the standard logger (which logger.Warn/Debug/etc. write through) during
// fn and returns everything it wrote, so tests can assert on which log lines did or didn't fire.
func captureLog(fn func()) string {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	fn()
	return buf.String()
}

// A real {"get":"status"} / {"set":...} response - "status" is a JSON object - should still
// populate the cache exactly as before, with no warning.
func TestUpdateStatusCacheFromJSON_ObjectStatus(t *testing.T) {
	defer resetStatus()()

	out := captureLog(func() {
		updateStatusCacheFromJSON(`{"status":{"v":12.1,"pwm1":true}}`)
	})

	Status.RLock()
	defer Status.RUnlock()
	if Status.Data == nil {
		t.Fatal("expected Status.Data to be populated from a real status object")
	}
	if Status.Data["v"] != 12.1 {
		t.Errorf("expected v=12.1, got %v", Status.Data["v"])
	}
	if strings.Contains(out, "missing 'status' object") {
		t.Errorf("did not expect a missing-status warning for a real status object, got log: %s", out)
	}
}

// dry_sensor/reboot/factory_reset acks reuse the "status" key for a plain string, e.g.
// {"status":"starting SHT40 drying cycle"}. This is an expected shape, not an error - it must
// not warn, and must leave the cache untouched rather than corrupt it.
func TestUpdateStatusCacheFromJSON_StringAck(t *testing.T) {
	defer resetStatus()()

	out := captureLog(func() {
		updateStatusCacheFromJSON(`{"status":"starting SHT40 drying cycle"}`)
	})

	Status.RLock()
	defer Status.RUnlock()
	if Status.Data != nil {
		t.Errorf("expected a plain-text command ack to leave the status cache untouched, got %v", Status.Data)
	}
	if strings.Contains(out, "missing 'status' object") {
		t.Errorf("a plain-text ack like dry_sensor's should not log the missing-status warning, got log: %s", out)
	}
}

// A line that genuinely has no "status" key at all (e.g. a conditions/sensor payload routed
// here by mistake) should still warn - this is the one case that really is unexpected.
func TestUpdateStatusCacheFromJSON_MissingStatus(t *testing.T) {
	defer resetStatus()()

	out := captureLog(func() {
		updateStatusCacheFromJSON(`{"sht_temperature":21.5}`)
	})

	Status.RLock()
	defer Status.RUnlock()
	if Status.Data != nil {
		t.Errorf("expected the status cache to stay untouched when 'status' is absent, got %v", Status.Data)
	}
	if !strings.Contains(out, "missing 'status' object") {
		t.Errorf("expected the missing-status warning when 'status' is genuinely absent, got log: %s", out)
	}
}

func TestComputeSafetyStatus(t *testing.T) {
	t.Run("empty condition list is always safe on both channels", func(t *testing.T) {
		withSafetyConditions(t)
		status := computeSafetyStatus(map[string]interface{}{"v": 5.0})
		if status.UIUnsafe || status.AlpacaUnsafe || status.UIReason != "" || status.AlpacaReason != "" {
			t.Errorf("expected fully safe with no reasons, got %+v", status)
		}
	})

	t.Run("condition with Notify only affects UIUnsafe, not AlpacaUnsafe", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0, Notify: true})
		status := computeSafetyStatus(map[string]interface{}{"v": 10.5})
		if !status.UIUnsafe || status.UIReason == "" {
			t.Errorf("expected UIUnsafe with a reason, got %+v", status)
		}
		if status.AlpacaUnsafe {
			t.Errorf("expected AlpacaUnsafe to stay false when the condition isn't IncludeInSafetyMonitor, got %+v", status)
		}
	})

	t.Run("condition with IncludeInSafetyMonitor only affects AlpacaUnsafe, not UIUnsafe", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0, IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"v": 10.5})
		if !status.AlpacaUnsafe || status.AlpacaReason == "" {
			t.Errorf("expected AlpacaUnsafe with a reason, got %+v", status)
		}
		if status.UIUnsafe {
			t.Errorf("expected UIUnsafe to stay false when the condition isn't Notify, got %+v", status)
		}
	})

	t.Run("condition with neither flag set is skipped entirely, even if it would match", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0})
		status := computeSafetyStatus(map[string]interface{}{"v": 5.0})
		if status.UIUnsafe || status.AlpacaUnsafe {
			t.Errorf("expected a condition with no channels enabled to never trip anything, got %+v", status)
		}
	})

	t.Run("single non-matching condition is safe", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0, Notify: true, IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"v": 12.0})
		if status.UIUnsafe || status.AlpacaUnsafe {
			t.Errorf("expected safe when voltage is above the threshold, got %+v", status)
		}
	})

	t.Run("conditions are OR'd per channel - only the second one tripping is still unsafe", func(t *testing.T) {
		withSafetyConditions(t,
			config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0, Notify: true},  // does not match
			config.SafetyCondition{Metric: "humidity", Operator: ">=", Threshold: 90.0, Notify: true}, // matches
		)
		status := computeSafetyStatus(map[string]interface{}{"v": 12.0, "h_amb": 95.0})
		if !status.UIUnsafe {
			t.Errorf("expected unsafe when the second of two OR'd conditions matches, got %+v", status)
		}
	})

	t.Run("unknown metric is skipped, not treated as a match", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "windSpeed", Operator: ">", Threshold: 50, Notify: true})
		status := computeSafetyStatus(map[string]interface{}{"v": 12.0})
		if status.UIUnsafe {
			t.Errorf("expected an unrecognized metric to be skipped, not matched, got %+v", status)
		}
	})

	t.Run("missing reading is skipped, not treated as a match", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "voltage", Operator: "<=", Threshold: 11.0, Notify: true})
		status := computeSafetyStatus(map[string]interface{}{})
		if status.UIUnsafe {
			t.Errorf("expected a missing reading to be skipped, not matched, got %+v", status)
		}
	})

	t.Run("current is converted from mA to A before comparing", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "current", Operator: ">=", Threshold: 5.0, Notify: true})
		// 5500 mA = 5.5 A, at or above a 5 A threshold.
		status := computeSafetyStatus(map[string]interface{}{"i": 5500.0})
		if !status.UIUnsafe {
			t.Errorf("expected 5500 mA to trip a >= 5 A threshold, got %+v", status)
		}
	})

	t.Run("heaterLimitEngaged with IncludeInSafetyMonitor trips AlpacaUnsafe when cl is true", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": true})
		if !status.AlpacaUnsafe || status.AlpacaReason == "" {
			t.Errorf("expected AlpacaUnsafe with a reason when cl is true, got %+v", status)
		}
	})

	t.Run("heaterLimitEngaged does not trip AlpacaUnsafe when cl is false", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": false})
		if status.AlpacaUnsafe {
			t.Errorf("expected safe when cl is false, got %+v", status)
		}
	})

	t.Run("heaterLimitEngaged without IncludeInSafetyMonitor never trips AlpacaUnsafe, even if cl is true", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", Notify: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": true})
		if status.AlpacaUnsafe {
			t.Errorf("expected AlpacaUnsafe to stay false without IncludeInSafetyMonitor, got %+v", status)
		}
		if status.UIUnsafe {
			t.Errorf("expected UIUnsafe to stay false too - Notify for this metric is handled by fireSafetyEvent, not computeSafetyStatus, got %+v", status)
		}
	})
}
