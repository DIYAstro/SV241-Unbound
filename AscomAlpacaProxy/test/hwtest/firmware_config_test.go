//go:build hwtest

package hwtest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetDevice performs a real hard reset via esptool (same as this session's manual hardware
// testing: connect, upload a stub, "--after hard-reset"), so persistence tests exercise an
// actual reboot + loadConfig() cycle, not just an in-memory round-trip. Skips (not fails) if
// esptool can't be located, since its path is machine-specific (PlatformIO's bundled Python).
func resetDevice(t *testing.T, port string) {
	t.Helper()
	pythonExe := os.Getenv("HWTEST_PYTHON")
	esptoolPy := os.Getenv("HWTEST_ESPTOOL_PY")
	if pythonExe == "" || esptoolPy == "" {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		if pythonExe == "" {
			pythonExe = filepath.Join(home, ".platformio", "penv", "Scripts", "python.exe")
		}
		if esptoolPy == "" {
			esptoolPy = filepath.Join(home, ".platformio", "packages", "tool-esptoolpy", "esptool.py")
		}
	}
	if _, err := os.Stat(pythonExe); err != nil {
		t.Skipf("skipping reset-based test: esptool Python not found at %s (override via HWTEST_PYTHON/HWTEST_ESPTOOL_PY)", pythonExe)
	}
	cmd := exec.Command(pythonExe, esptoolPy, "--port", port, "--after", "hard-reset", "chip-id")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "esptool reset failed: %s", out)
}

// TestConfig_PersistenceAcrossReset is a regression test for the struct-padding config
// corruption bug found this session (an upgrade silently zeroed xd because a new field landed
// in existing struct padding, defeating the old sizeof(Config) check) and for the JSON config
// storage migration meant to structurally eliminate that bug class. Exercises a real reboot.
func TestConfig_PersistenceAcrossReset(t *testing.T) {
	port := hwtestPort()
	conn := openConnForTest(t)

	const testPsd = 777
	before := setConfig(t, conn, fmt.Sprintf(`{"psd":%d}`, testPsd))
	require.Equal(t, float64(testPsd), before["psd"])
	conn.Close() // release the port before resetting the device

	resetDevice(t, port)
	time.Sleep(2 * time.Second) // let setup()/loadConfig() finish

	conn2 := openConnForTest(t)
	cfg := getConfig(t, conn2)
	assert.Equal(t, float64(testPsd), cfg["psd"], "psd should survive a real device reset via the JSON config store")

	setConfig(t, conn2, `{"psd":500}`) // restore default
}

// TestConfig_ClampBoundaries covers psd's documented 0-5000ms range, including the 0 "falsy"
// edge case (0 = staggering disabled must not be silently coerced to a default) and a negative
// input, whose exact clamped behavior had never been exercised before this suite.
func TestConfig_ClampBoundaries(t *testing.T) {
	conn := openConnForTest(t)

	t.Run("upper_bound_clamps_to_5000", func(t *testing.T) {
		resp := setConfig(t, conn, `{"psd":9999}`)
		assert.Equal(t, float64(5000), resp["psd"])
	})
	t.Run("zero_stays_zero", func(t *testing.T) {
		resp := setConfig(t, conn, `{"psd":0}`)
		assert.Equal(t, float64(0), resp["psd"], "psd:0 must not silently fall back to the 500ms default")
	})
	t.Run("negative_clamps_into_range", func(t *testing.T) {
		resp := setConfig(t, conn, `{"psd":-100}`)
		psd := asFloat(t, resp["psd"])
		assert.GreaterOrEqual(t, psd, 0.0, "a negative psd input must not produce a negative stored value")
		assert.LessOrEqual(t, psd, 5000.0)
	})

	setConfig(t, conn, `{"psd":500}`) // restore default
}

// TestConfig_UnknownKeysIgnored verifies robustness of updateConfig(): an unrecognized top-level
// key in an "sc" command must be silently ignored, without disturbing unrelated existing fields.
func TestConfig_UnknownKeysIgnored(t *testing.T) {
	conn := openConnForTest(t)
	before := getConfig(t, conn)

	resp := setConfig(t, conn, `{"totally_unknown_field_xyz":123,"psd":500}`)

	assert.Equal(t, before["av"], resp["av"], "an unrelated field must be untouched by an unknown key in the same sc command")
	assert.Equal(t, float64(500), resp["psd"], "a known field in the same command must still apply")
}

// TestConfig_SensorOffsets covers the "so" (sensor_offsets) config object: round-trips all 5
// fields with distinct non-zero values, then verifies applying the SHT40 temperature offset
// actually shifts the reported t_amb by roughly that amount - stronger than a config-only
// round-trip, since it also confirms sensors.cpp actually applies the stored offset.
func TestConfig_SensorOffsets(t *testing.T) {
	conn := openConnForTest(t)
	before := getConfig(t, conn)
	origSo, _ := before["so"].(map[string]interface{})

	baseline, err := getSensors(conn)
	require.NoError(t, err)
	baseTAmb := asFloat(t, baseline["t_amb"])

	const deltaST = 5.0
	resp := setConfig(t, conn, fmt.Sprintf(`{"so":{"st":%v,"sh":2,"dt":1.5,"iv":0.1,"ic":0.05}}`, deltaST))
	so, ok := resp["so"].(map[string]interface{})
	require.True(t, ok, "config response missing so object: %v", resp)
	assert.InDelta(t, deltaST, asFloat(t, so["st"]), 0.01, "sht40 temp offset (st)")
	assert.InDelta(t, 2.0, asFloat(t, so["sh"]), 0.01, "sht40 humidity offset (sh)")
	assert.InDelta(t, 1.5, asFloat(t, so["dt"]), 0.01, "ds18b20 temp offset (dt)")
	assert.InDelta(t, 0.1, asFloat(t, so["iv"]), 0.01, "ina219 voltage offset (iv)")
	assert.InDelta(t, 0.05, asFloat(t, so["ic"]), 0.01, "ina219 current offset (ic)")

	// The offset only takes effect on the sensor task's next averaged reading (default 1000ms
	// interval, see "ui.s") - poll instead of a single fixed-delay sample.
	deadline := time.Now().Add(5 * time.Second)
	shifted := false
	var afterTAmb float64
	for time.Now().Before(deadline) {
		after, err := getSensors(conn)
		require.NoError(t, err)
		afterTAmb = asFloat(t, after["t_amb"])
		if afterTAmb >= baseTAmb+deltaST-0.5 {
			shifted = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	assert.True(t, shifted, "t_amb should shift by ~%.1f (the applied sht40 temp offset): baseline=%v, last seen=%v", deltaST, baseTAmb, afterTAmb)

	if origSo != nil {
		setConfig(t, conn, fmt.Sprintf(`{"so":{"st":%v,"sh":%v,"dt":%v,"iv":%v,"ic":%v}}`,
			origSo["st"], origSo["sh"], origSo["dt"], origSo["iv"], origSo["ic"]))
	} else {
		setConfig(t, conn, `{"so":{"st":0,"sh":0,"dt":0,"iv":0,"ic":0}}`)
	}
}

// TestConfig_UpdateIntervalsAndAveragingCounts covers the "ui" (update_intervals_ms) and "ac"
// (averaging_counts) config objects - round-trip only, previously untested by this suite.
func TestConfig_UpdateIntervalsAndAveragingCounts(t *testing.T) {
	conn := openConnForTest(t)
	before := getConfig(t, conn)
	origUi, _ := before["ui"].(map[string]interface{})
	origAc, _ := before["ac"].(map[string]interface{})

	resp := setConfig(t, conn, `{"ui":{"i":1500,"s":1200,"d":1100},"ac":{"st":3,"sh":4,"dt":6,"iv":7,"ic":8}}`)

	ui, ok := resp["ui"].(map[string]interface{})
	require.True(t, ok, "config response missing ui object: %v", resp)
	assert.EqualValues(t, 1500, ui["i"], "ina219 update interval")
	assert.EqualValues(t, 1200, ui["s"], "sht40 update interval")
	assert.EqualValues(t, 1100, ui["d"], "ds18b20 update interval")

	ac, ok := resp["ac"].(map[string]interface{})
	require.True(t, ok, "config response missing ac object: %v", resp)
	assert.EqualValues(t, 3, ac["st"], "sht40 temp averaging count")
	assert.EqualValues(t, 4, ac["sh"], "sht40 humidity averaging count")
	assert.EqualValues(t, 6, ac["dt"], "ds18b20 temp averaging count")
	assert.EqualValues(t, 7, ac["iv"], "ina219 voltage averaging count")
	assert.EqualValues(t, 8, ac["ic"], "ina219 current averaging count")

	if origUi != nil && origAc != nil {
		setConfig(t, conn, fmt.Sprintf(`{"ui":{"i":%v,"s":%v,"d":%v},"ac":{"st":%v,"sh":%v,"dt":%v,"iv":%v,"ic":%v}}`,
			origUi["i"], origUi["s"], origUi["d"], origAc["st"], origAc["sh"], origAc["dt"], origAc["iv"], origAc["ic"]))
	}
}

// TestConfig_ResetCommands exercises the firmware's own "reboot" and "factory_reset" serial
// commands (main.cpp:104-118), each of which restarts the device.
func TestConfig_ResetCommands(t *testing.T) {
	t.Run("factory_reset", func(t *testing.T) {
		conn := openConnForTest(t)
		// Dirty the config first so the reset is actually observable, not vacuously true.
		setConfig(t, conn, `{"psd":1234,"dh":[{"xd":37},{}]}`)

		resp := mustSendCommand(t, conn, `{"command":"factory_reset"}`, 3*time.Second)
		var ack map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp), &ack))
		assert.Contains(t, fmt.Sprint(ack["status"]), "factory reset")
		conn.Close() // release the port - a restart is already in flight

		time.Sleep(3 * time.Second) // ESP.restart() (100ms delay) + boot time

		conn2 := openConnForTest(t)
		cfg := getConfig(t, conn2)
		assert.Equal(t, float64(500), cfg["psd"], "psd should be back to its 500ms default after factory_reset")
		dhArr, ok := cfg["dh"].([]interface{})
		if assert.True(t, ok) && assert.Greater(t, len(dhArr), 0) {
			heater0, ok := dhArr[0].(map[string]interface{})
			if assert.True(t, ok) {
				// Regression test for the struct-padding bug this session's JSON config
				// migration was built to eliminate: an upgrade once silently corrupted this
				// exact field (xd) to 0 instead of its intended 100 (no limit) default.
				assert.Equal(t, float64(100), heater0["xd"], "xd should be back to its 100 (no limit) default after factory_reset")
			}
		}
	})

	t.Run("reboot", func(t *testing.T) {
		conn := openConnForTest(t)
		resp := mustSendCommand(t, conn, `{"command":"reboot"}`, 3*time.Second)
		var ack map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp), &ack))
		assert.Contains(t, fmt.Sprint(ack["status"]), "rebooting")
		conn.Close()

		time.Sleep(3 * time.Second)
		conn2 := openConnForTest(t)
		_, err := getSensors(conn2)
		assert.NoError(t, err, "device should respond normally to a sensors query shortly after a reboot command")
	})
}
