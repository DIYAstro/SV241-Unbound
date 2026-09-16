package serial

import (
	"bytes"
	"log"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"testing"
)

// withSafetyThreshold sets config.Get().SafetyMonitorVoltageThreshold for the duration of a test
// and restores the previous value afterward, so these tests don't leak config state into others.
func withSafetyThreshold(t *testing.T, threshold float64) {
	t.Helper()
	conf := config.Get()
	orig := conf.SafetyMonitorVoltageThreshold
	conf.SafetyMonitorVoltageThreshold = threshold
	t.Cleanup(func() { conf.SafetyMonitorVoltageThreshold = orig })
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

func TestComputeSafetyUnsafe(t *testing.T) {
	cases := []struct {
		name      string
		threshold float64
		voltage   interface{}
		want      bool
	}{
		{"disabled threshold never unsafe", 0, 10.0, false},
		{"negative threshold never unsafe", -1, 10.0, false},
		{"voltage above threshold is safe", 11.0, 12.0, false},
		{"voltage at threshold is unsafe", 11.0, 11.0, true},
		{"voltage below threshold is unsafe", 11.0, 10.5, true},
		{"missing voltage reading is safe", 11.0, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withSafetyThreshold(t, tc.threshold)

			conditionsData := map[string]interface{}{}
			if tc.voltage != nil {
				conditionsData["v"] = tc.voltage
			}

			if got := computeSafetyUnsafe(conditionsData); got != tc.want {
				t.Errorf("computeSafetyUnsafe(threshold=%.1f, v=%v) = %v, want %v", tc.threshold, tc.voltage, got, tc.want)
			}
		})
	}
}
