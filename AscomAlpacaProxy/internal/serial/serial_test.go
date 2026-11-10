package serial

import (
	"bytes"
	"log"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"testing"
	"time"
)

// scriptedPort is a Port whose Read hands out one scripted chunk per call, so a test controls
// exactly how the bytes are split across reads (readLine drops what follows the first newline of a
// chunk). Once the script is used up it behaves like a real port with nothing to read: it waits
// out the read timeout and returns (0, nil).
type scriptedPort struct {
	chunks  []string
	timeout time.Duration
}

func (p *scriptedPort) Read(b []byte) (int, error) {
	if len(p.chunks) == 0 {
		time.Sleep(p.timeout)
		return 0, nil
	}
	n := copy(b, p.chunks[0])
	p.chunks = p.chunks[1:]
	return n, nil
}

func (p *scriptedPort) Write(b []byte) (int, error) { return len(b), nil }
func (p *scriptedPort) Close() error                { return nil }
func (p *scriptedPort) SetDTR(bool) error           { return nil }
func (p *scriptedPort) SetRTS(bool) error           { return nil }
func (p *scriptedPort) SetReadTimeout(d time.Duration) error {
	p.timeout = d
	return nil
}

// The firmware's own log lines are skipped, but anything that can be a command's reply - the
// {"info":...} acks of reboot/factory_reset/dry_sensor and every real error reply - must not be:
// skipping those would turn the reply into a timeout and resend the command.
func TestIsUnsolicitedLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`{"warn":"INA219 read failed"}`, true},
		{`{"warn":"Attempting I2C bus recovery after repeated sensor read failures"}` + "\r", true},
		{`  {"warn":"SHT40 sensor not found"}  `, true},
		{`{"info":"SHT40 drying cycle complete"}`, true},
		{`{"info":"SHT40 auto-drying cycle started"}`, true},

		{`{"info":"rebooting"}`, false},
		{`{"info":"performing factory reset"}`, false},
		{`{"info":"starting SHT40 drying cycle"}`, false},
		{`{"error":"Cannot enable disabled output: DC3"}`, false},
		{`{"error":"Invalid port or minutes (1-1440) for delay_set"}`, false},
		{`{"error":"unknown command in valid JSON"}`, false},
		{`{"error":"invalid command"}`, false},
		{`{"status":{"d1":1,"d2":0}}`, false},
		{`{"v":12.1,"i":340}`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := isUnsolicitedLine(c.line); got != c.want {
			t.Errorf("isUnsolicitedLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// A log line that arrives between the command and its reply must be skipped, and the reply read
// right after it (here as separate chunks - see readLine's doc comment for the limitation when
// both land in one).
func TestReadResponseLine_SkipsUnsolicitedLines(t *testing.T) {
	p := &scriptedPort{chunks: []string{
		`{"warn":"INA219 read failed"}` + "\r\n",
		`{"info":"SHT40 drying cycle complete"}` + "\r\n",
		`{"status":{"d1":1}}` + "\r\n",
	}}
	var got string
	var err error
	captureLog(func() { got, err = readResponseLine(p, time.Second) })
	if err != nil {
		t.Fatalf("readResponseLine returned an error: %v", err)
	}
	if strings.TrimSpace(got) != `{"status":{"d1":1}}` {
		t.Errorf("got %q, want the status reply", got)
	}
}

// The ack of reboot/factory_reset/dry_sensor is the reply itself and has to come through.
func TestReadResponseLine_KeepsInfoAck(t *testing.T) {
	p := &scriptedPort{chunks: []string{`{"info":"starting SHT40 drying cycle"}` + "\n"}}
	var got string
	var err error
	captureLog(func() { got, err = readResponseLine(p, time.Second) })
	if err != nil {
		t.Fatalf("readResponseLine returned an error: %v", err)
	}
	if strings.TrimSpace(got) != `{"info":"starting SHT40 drying cycle"}` {
		t.Errorf("got %q, want the ack", got)
	}
}

// Skipped lines share the one overall timeout - a stream of log lines without a reply must not
// stretch the wait.
func TestReadResponseLine_OnlyLogLinesTimesOut(t *testing.T) {
	p := &scriptedPort{chunks: []string{
		`{"warn":"INA219 read failed"}` + "\n",
		`{"warn":"INA219 read failed"}` + "\n",
	}}
	start := time.Now()
	var err error
	captureLog(func() { _, err = readResponseLine(p, 150*time.Millisecond) })
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected a read timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v, expected about the 150ms overall timeout", elapsed)
	}
}

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

	t.Run("heaterLimitEngaged with IncludeInSafetyMonitor trips AlpacaUnsafe when cl is 1", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": 1.0})
		if !status.AlpacaUnsafe || status.AlpacaReason == "" {
			t.Errorf("expected AlpacaUnsafe with a reason when cl is 1, got %+v", status)
		}
	})

	t.Run("heaterLimitEngaged does not trip AlpacaUnsafe when cl is 0", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", IncludeInSafetyMonitor: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": 0.0})
		if status.AlpacaUnsafe {
			t.Errorf("expected safe when cl is 0, got %+v", status)
		}
	})

	t.Run("heaterLimitEngaged without IncludeInSafetyMonitor never trips AlpacaUnsafe, even if cl is 1", func(t *testing.T) {
		withSafetyConditions(t, config.SafetyCondition{Metric: "heaterLimitEngaged", Notify: true})
		status := computeSafetyStatus(map[string]interface{}{"cl": 1.0})
		if status.AlpacaUnsafe {
			t.Errorf("expected AlpacaUnsafe to stay false without IncludeInSafetyMonitor, got %+v", status)
		}
		if status.UIUnsafe {
			t.Errorf("expected UIUnsafe to stay false too - Notify for this metric is handled by fireSafetyEvent, not computeSafetyStatus, got %+v", status)
		}
	})
}
