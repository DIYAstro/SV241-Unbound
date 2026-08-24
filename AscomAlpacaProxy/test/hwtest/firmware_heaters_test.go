//go:build hwtest

package hwtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heaterModeCase describes a dew heater mode and a full, distinguishable set of values for
// every field that mode actually uses - mirrors HeaterConfig.vue's per-mode fields. Using
// non-default, non-round values (not just "does the mode switch work") catches a field being
// silently dropped or mismapped in updateConfig()/serializeConfig(), not just the mode itself.
type heaterModeCase struct {
	name   string
	mode   int
	fields map[string]float64
}

var heaterModeCases = []heaterModeCase{
	{"Manual", 0, map[string]float64{"mp": 73}},
	{"PID", 1, map[string]float64{"to": 4.5, "kp": 12.5, "ki": 0.8, "kd": 3.2}},
	{"Ambient", 2, map[string]float64{"sd": 6.5, "ed": 1.5, "xp": 65}},
	{"MinTemp", 4, map[string]float64{"mt": -2.5, "to": 2.5, "kp": 15, "ki": 0.5, "kd": 2}},
	{"Disabled", 5, map[string]float64{}},
	// "Sync" (mode 3) is deliberately not in this table - it depends on the *other* heater being
	// in a compatible leader mode (1 or 4), so it's covered separately by
	// TestHeaters_SyncFollower_Basic rather than being tested in isolation here.
}

// fieldsJSON renders a mode + fields map as the inner "key":value pairs of a dh[i] object.
func fieldsJSON(mode int, fields map[string]float64) string {
	s := fmt.Sprintf(`"m":%d`, mode)
	for k, v := range fields {
		s += fmt.Sprintf(`,%q:%v`, k, v)
	}
	return s
}

// setHeaterField sends {"dh":[...]} with only the given heater index populated; the other index
// is sent as {} which config_manager.cpp's updateConfig() treats as a complete no-op (every
// field there is guarded by an isNull() check).
func setHeaterField(t *testing.T, conn *SerialConn, heaterIdx int, fields string) map[string]interface{} {
	t.Helper()
	dh := []string{"{}", "{}"}
	dh[heaterIdx] = "{" + fields + "}"
	return setConfig(t, conn, fmt.Sprintf(`{"dh":[%s,%s]}`, dh[0], dh[1]))
}

func dewHeaterObj(t *testing.T, resp map[string]interface{}, heaterIdx int) map[string]interface{} {
	t.Helper()
	dhArr, ok := resp["dh"].([]interface{})
	require.True(t, ok, "config response missing dh array: %v", resp)
	require.Greater(t, len(dhArr), heaterIdx)
	obj, ok := dhArr[heaterIdx].(map[string]interface{})
	require.True(t, ok, "dh[%d] was not an object: %v", heaterIdx, dhArr[heaterIdx])
	return obj
}

func TestHeaters_AllModes(t *testing.T) {
	conn := openConnForTest(t)

	for _, heaterIdx := range []int{0, 1} {
		for _, tc := range heaterModeCases {
			heaterIdx, tc := heaterIdx, tc
			t.Run(fmt.Sprintf("heater%d/%s", heaterIdx+1, tc.name), func(t *testing.T) {
				resp := setHeaterField(t, conn, heaterIdx, fieldsJSON(tc.mode, tc.fields))
				obj := dewHeaterObj(t, resp, heaterIdx)
				assert.Equal(t, float64(tc.mode), obj["m"], "heater %d mode should be %d after set", heaterIdx+1, tc.mode)
				for field, want := range tc.fields {
					got, ok := obj[field].(float64)
					if assert.True(t, ok, "heater %d field %q missing or not numeric in response: %v", heaterIdx+1, field, obj) {
						assert.InDelta(t, want, got, 0.01, "heater %d field %q: set %v, got %v back", heaterIdx+1, field, want, got)
					}
				}
			})
		}
	}

	t.Run("CustomName", func(t *testing.T) {
		const name = "Test Heater A"
		resp := setConfig(t, conn, fmt.Sprintf(`{"dh":[{"n":%q},{}]}`, name))
		obj := dewHeaterObj(t, resp, 0)
		assert.Equal(t, name, obj["n"], "heater name should round-trip exactly")
	})

	t.Run("PsfRoundTrip", func(t *testing.T) {
		resp := setHeaterField(t, conn, 1, `"psf":1.75`)
		obj := dewHeaterObj(t, resp, 1)
		got, ok := obj["psf"].(float64)
		if assert.True(t, ok, "psf missing or not numeric: %v", obj) {
			assert.InDelta(t, 1.75, got, 0.01, "psf (sync factor) should round-trip")
		}
	})

	// Leave both heaters disabled/safe for subsequent tests.
	setConfig(t, conn, `{"dh":[{"m":5},{"m":5}]}`)
}

// TestHeaters_MaxDutyClamp is a regression test for this session's duty-domain clamp fix and the
// xd:0 "falsy" pitfall - see dew_control.cpp's compute_heater_output()/duty_limit_to_raw_duty().
func TestHeaters_MaxDutyClamp(t *testing.T) {
	conn := openConnForTest(t)

	cases := []struct {
		input, expected int
	}{
		{0, 0}, // must NOT silently become 100
		{1, 1},
		{50, 50},
		{100, 100},
		{150, 100}, // clamps at upper bound
		{-5, 0},    // clamps at lower bound
	}

	for _, heaterIdx := range []int{0, 1} {
		heaterIdx := heaterIdx
		t.Run(fmt.Sprintf("heater%d", heaterIdx+1), func(t *testing.T) {
			for _, c := range cases {
				resp := setHeaterField(t, conn, heaterIdx, fmt.Sprintf(`"xd":%d`, c.input))
				obj := dewHeaterObj(t, resp, heaterIdx)
				assert.Equal(t, float64(c.expected), obj["xd"], "xd=%d should clamp to %d for heater %d", c.input, c.expected, heaterIdx+1)
			}
		})
	}

	// Restore both heaters to "no limit" for subsequent tests.
	setConfig(t, conn, `{"dh":[{"xd":100},{"xd":100}]}`)
}

// TestHeaters_SyncFollower_Basic exercises the PID-Sync (mode 3) follower against a Manual
// leader with a known, fixed output. Full verification of the leader/follower anti-windup
// separation fixed this session (heater_demand vs heater_power in dew_control.cpp), and of the
// follower's actual tracked duty cycle, needs controlled thermal stimulus and isn't practical as
// a hardware-in-the-loop test - this covers what is: mode/psf round-trip, and that the follower
// reports itself active while being driven by a non-zero leader (see the comment inside).
func TestHeaters_SyncFollower_Basic(t *testing.T) {
	conn := openConnForTest(t)
	// "en" in the "dh" config object is enabled_on_startup, not the live running state - the
	// heater still needs an explicit {"set":{"pwmN":true}} to actually start, same as a user
	// flipping the switch in the UI (see power_control.cpp's set_power_output()).
	setConfig(t, conn, `{"dh":[{"m":0,"mp":40,"en":1,"xd":100},{"m":3,"psf":1.0,"en":1,"xd":100}]}`)
	setSwitch(t, conn, "pwm1", true)
	setSwitch(t, conn, "pwm2", true)
	t.Cleanup(func() {
		// Explicitly stop both heaters (the live running flag is not part of the config
		// snapshot TestMain restores, so leaving this out would leave a heater actively running
		// in Manual/Sync mode after the suite finishes).
		setSwitch(t, conn, "pwm1", false)
		setSwitch(t, conn, "pwm2", false)
	})
	// Note on what's actually observable here: get_power_status_json() only reports a numeric
	// duty percentage for Manual mode (0). For every other mode - including Sync (3) - it
	// deliberately collapses to a plain true/false "active" flag ("This ensures the Switch UI
	// stays ON" even when computed power is momentarily 0). So the follower's actual tracked
	// duty cycle isn't observable via this protocol at all; this test can only verify it reports
	// active while the leader is producing non-zero output, not the tracked value itself.
	// Manual-mode power is only picked up on the dew_control task's next periodic tick, not
	// instantly on "set" - poll instead of a single fixed-delay sample, since a 2s sleep isn't
	// always enough margin depending on where in that tick cycle the command happened to land.
	var pwm1 float64
	var status map[string]interface{}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status = getStatus(t, conn)
		pwm1 = asFloat(t, status["pwm1"])
		if pwm1 >= 37 && pwm1 <= 43 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	assert.InDelta(t, 40, pwm1, 3, "leader (manual, mp=40) should report ~40%% power within 10s, last seen %v", pwm1)
	assert.Equal(t, true, status["pwm2"], "follower (Sync mode, enabled) should report active (true) while the leader is producing non-zero output")

	setConfig(t, conn, `{"dh":[{"m":5},{"m":5}]}`)
}

// TestHeaters_Disabled_SkippedByAll verifies a heater in mode 5 (Disabled) is skipped by
// all:true, same as a Disabled standard switch - see power_control.cpp:222-223, 235-236.
func TestHeaters_Disabled_SkippedByAll(t *testing.T) {
	conn := openConnForTest(t)
	setConfig(t, conn, `{"dh":[{"m":5},{"m":5}]}`)
	setConfig(t, conn, `{"psd":0}`)
	setAllPower(t, conn, false)
	time.Sleep(200 * time.Millisecond)

	setAllPower(t, conn, true)
	time.Sleep(500 * time.Millisecond)

	status := getStatus(t, conn)
	assert.Equal(t, false, status["pwm1"], "Disabled heater 1 (mode 5) should be skipped by all:true")
	assert.Equal(t, false, status["pwm2"], "Disabled heater 2 (mode 5) should be skipped by all:true")

	setAllPower(t, conn, false)
}
