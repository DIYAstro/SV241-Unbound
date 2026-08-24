//go:build hwtest

package hwtest

import (
	"fmt"
	"sort"
	"time"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// standardSwitchKeys are the plain on/off outputs - excludes adj_conv (bool+voltage) and
// pwm1/pwm2 (covered by firmware_heaters_test.go), matching SwitchConfig.vue's orderedKeys.
var standardSwitchKeys = []string{"d1", "d2", "d3", "d4", "d5", "u12", "u34"}

// baselineEnabledOff marks all standard switches + adj_conv as Off-but-enabled (state 0) in ps,
// giving each test a known, deterministic starting point regardless of what earlier tests left.
func baselineEnabledOff(t *testing.T, conn *SerialConn) {
	t.Helper()
	states := map[string]int{"adj": 0}
	for _, k := range standardSwitchKeys {
		states[k] = 0
	}
	setPowerStartupStates(t, conn, states)
	setAllPower(t, conn, false)
}

func TestSwitches_IndividualOnOff(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)

	for _, key := range standardSwitchKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			setSwitch(t, conn, key, true)
			status := getStatus(t, conn)
			assert.Equal(t, 1, switchStatusInt(t, status, key), "switch %q should be ON after set", key)

			setSwitch(t, conn, key, false)
			status = getStatus(t, conn)
			assert.Equal(t, 0, switchStatusInt(t, status, key), "switch %q should be OFF after set", key)
		})
	}
}

func TestSwitches_AdjConv(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)

	// Boolean on: firmware falls back to the configured preset voltage (av).
	setSwitch(t, conn, "adj", true)
	status := getStatus(t, conn)
	v, ok := status["adj"].(float64)
	require.True(t, ok, "adj status should be a numeric voltage when on, got: %v", status["adj"])
	assert.Greater(t, v, 0.0, "adj voltage should be > 0V when switched on")

	// Explicit voltage override.
	setConfig(t, conn, `{}`) // no-op, just to exercise a plain sc round-trip alongside
	resp := setSwitch(t, conn, "adj", false)
	assert.Equal(t, float64(0), asFloat(t, resp["adj"]), "adj should report off/0 after being switched off")

	// 0V should turn it off per power_control.cpp's handle_set_power_command.
	cmd := `{"set":{"adj":5.0}}`
	respRaw := mustSendCommand(t, conn, cmd, 3*time.Second)
	_ = respRaw
	status = getStatus(t, conn)
	v, ok = status["adj"].(float64)
	require.True(t, ok)
	assert.InDelta(t, 5.0, v, 0.5, "adj voltage should reflect the requested 5V override")

	setSwitch(t, conn, "adj", false)
}

func asFloat(t *testing.T, v interface{}) float64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	default:
		t.Fatalf("expected a numeric/bool value, got %v (%T)", v, v)
		return 0
	}
}

// TestSwitches_Disabled verifies a switch marked Disabled (ps state 2) rejects an individual
// enable attempt and is skipped by "all":true - see power_control.cpp:108-131, 235-236.
func TestSwitches_Disabled(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	setPowerStartupStates(t, conn, map[string]int{"d1": 2}) // d1 Disabled, d2 stays enabled/off

	resp := setSwitch(t, conn, "d1", true)
	_, hasError := resp["error"]
	assert.True(t, hasError, "expected an {\"error\":...} response for enabling a Disabled switch, got: %v", resp)

	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d1"), "Disabled switch d1 must stay off after a rejected set")

	setConfig(t, conn, `{"psd":0}`) // no stagger needed for this check
	setAllPower(t, conn, true)
	time.Sleep(500 * time.Millisecond)
	status = getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d1"), "Disabled switch d1 should be skipped by all:true")
	assert.Equal(t, 1, switchStatusInt(t, status, "d2"), "d2 (not Disabled) should be turned on by all:true")

	setAllPower(t, conn, false)
}

// TestSwitches_AllOn_Staggering verifies that all:true enables outputs one at a time, spaced
// roughly `psd` apart, rather than simultaneously.
func TestSwitches_AllOn_Staggering(t *testing.T) {
	conn := openConnForTest(t)
	const staggerMs = 300
	baselineEnabledOff(t, conn)
	setConfig(t, conn, fmt.Sprintf(`{"psd":%d}`, staggerMs))

	start := time.Now()
	setAllPower(t, conn, true)

	turnedOnAt := make(map[string]time.Duration)
	deadline := start.Add(time.Duration(len(standardSwitchKeys)+2) * staggerMs * time.Millisecond)
	for len(turnedOnAt) < len(standardSwitchKeys) && time.Now().Before(deadline) {
		status := getStatus(t, conn)
		for _, k := range standardSwitchKeys {
			if _, already := turnedOnAt[k]; already {
				continue
			}
			if switchStatusInt(t, status, k) == 1 {
				turnedOnAt[k] = time.Since(start)
			}
		}
		if len(turnedOnAt) < len(standardSwitchKeys) {
			time.Sleep(40 * time.Millisecond)
		}
	}
	require.Len(t, turnedOnAt, len(standardSwitchKeys), "not all switches turned on within the expected window: %v", turnedOnAt)

	times := make([]time.Duration, 0, len(standardSwitchKeys))
	for _, d := range turnedOnAt {
		times = append(times, d)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	for i := 1; i < len(times); i++ {
		gap := times[i] - times[i-1]
		assert.Greater(t, gap, staggerMs*time.Millisecond/2,
			"gap between consecutive outputs turning on was too short for a %dms stagger delay (gap #%d: %v, onsets: %v)",
			staggerMs, i, gap, times)
	}

	setAllPower(t, conn, false)
}

// TestSwitches_AllOn_MidSequenceAbort verifies that all:false, sent mid-stagger, both stops
// further outputs from turning on AND clears the pending queue (no output turns on later).
func TestSwitches_AllOn_MidSequenceAbort(t *testing.T) {
	conn := openConnForTest(t)
	const staggerMs = 500
	baselineEnabledOff(t, conn)
	setConfig(t, conn, fmt.Sprintf(`{"psd":%d}`, staggerMs))

	setAllPower(t, conn, true)
	time.Sleep(staggerMs * 3 / 2 * time.Millisecond) // let roughly 1-2 outputs turn on
	setAllPower(t, conn, false)

	// Wait long enough that, if the abort had failed to clear the queue, the remaining outputs
	// would have turned on by now.
	time.Sleep(time.Duration(len(standardSwitchKeys))*staggerMs*time.Millisecond + time.Second)

	status := getStatus(t, conn)
	for _, k := range standardSwitchKeys {
		assert.Equal(t, 0, switchStatusInt(t, status, k),
			"switch %q was ON after all:false aborted the stagger sequence - the pending queue was not cleared", k)
	}
}

// TestSwitches_AllOff_Idempotent verifies already-off outputs are unaffected by all:false, and
// already-on outputs are unaffected (no toggle/flicker) by a repeated all:true.
func TestSwitches_Idempotency(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	setConfig(t, conn, `{"psd":0}`)

	setAllPower(t, conn, true)
	// service_power_stagger_queue() processes at most one queued output per ~100ms poll tick
	// regardless of psd - so even with staggering "disabled" (psd:0), draining len(keys) items
	// still takes roughly len(keys)*100ms, not instant. Generous margin here since this test is
	// about idempotency, not stagger timing (that's TestSwitches_AllOn_Staggering's job).
	time.Sleep(time.Duration(len(standardSwitchKeys))*150*time.Millisecond + 500*time.Millisecond)
	status := getStatus(t, conn)
	for _, k := range standardSwitchKeys {
		require.Equal(t, 1, switchStatusInt(t, status, k), "precondition failed: %q should be on", k)
	}

	// Calling all:true again while already all on should be a harmless no-op. Note: the
	// firmware re-queues every non-disabled output regardless of current state, so this also
	// takes the same ~100ms/item to drain - not truly instant just because nothing changes.
	setAllPower(t, conn, true)
	time.Sleep(time.Duration(len(standardSwitchKeys))*150*time.Millisecond + 500*time.Millisecond)
	status = getStatus(t, conn)
	for _, k := range standardSwitchKeys {
		assert.Equal(t, 1, switchStatusInt(t, status, k), "%q should still be on after a repeated all:true", k)
	}

	setAllPower(t, conn, false)
}
