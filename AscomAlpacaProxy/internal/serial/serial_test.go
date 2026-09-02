package serial

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

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
