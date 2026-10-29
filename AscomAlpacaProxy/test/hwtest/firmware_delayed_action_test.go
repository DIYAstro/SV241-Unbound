//go:build hwtest

package hwtest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Formalizes the manual verification performed while building the delayed on/off feature
// (config-driven per-switch delay_on_s/delay_off_s, the explicit one-shot "delay_set" command
// behind the proxy's DelayedOn/DelayedOff ASCOM Actions, and the interaction between both of
// those and Master Power / boot / the stagger queue) into permanent, repeatable tests. See
// power_control.cpp's schedule_delayed_action()/service_delayed_action_queue() and
// handle_set_power_command(), plus main.cpp's "delay_set" branch.

// setDelay sets dl.<key> = {on: onS, off: offS} (seconds) via "sc". Callers are responsible for
// restoring {0,0} afterwards - baselineEnabledOff (firmware_switches_test.go) does not touch dl.
func setDelay(t *testing.T, conn *SerialConn, key string, onS, offS int) {
	t.Helper()
	setConfig(t, conn, fmt.Sprintf(`{"dl":{%q:{"on":%d,"off":%d}}}`, key, onS, offS))
}

// baselineNoDelay clears dl (configured delay) for every switch this test file exercises - the
// standard DC/USB switches plus adj - back to {on:0, off:0}. baselineEnabledOff
// (firmware_switches_test.go) deliberately never touches dl, so any test that assumes delay is
// off everywhere except the one key it explicitly configures must clear the rest itself here.
// Not just theoretical: this box carried real leftover "dl" values from manual hands-on testing
// performed earlier in this same session, predating this formal suite and its
// TestMain-snapshot-restore safety net - TestDelayedAction_MasterPowerOnStacksStaggerAndConfiguredDelay
// failed against exactly that leftover state (a stale 10s delay on d1) the first time this suite
// ran for real, on real hardware.
func baselineNoDelay(t *testing.T, conn *SerialConn) {
	t.Helper()
	parts := make([]string, 0, len(standardSwitchKeys)+1)
	for _, k := range standardSwitchKeys {
		parts = append(parts, fmt.Sprintf(`%q:{"on":0,"off":0}`, k))
	}
	parts = append(parts, `"adj":{"on":0,"off":0}`)
	setConfig(t, conn, fmt.Sprintf(`{"dl":{%s}}`, strings.Join(parts, ",")))
}

// TestDelayedAction_DisabledOutputRejectsEvenWithConfiguredDelay is the regression test for a real
// bug found via AscomAlpacaProxy/test/hwtest's broader regression run (TestSwitches_
// StartupStatesAppliedOnBoot failed against a leftover configured delay on d1, exposing this): a
// Disabled output must reject a live enable attempt immediately, exactly like it already does
// with no delay configured - but a configured delay_on_s used to bypass the check entirely,
// because the delayed path schedules the action via schedule_delayed_action() instead of calling
// set_power_output() (where the check used to live) until the delay elapses. See
// is_output_disabled()'s doc comment in power_control.cpp.
func TestDelayedAction_DisabledOutputRejectsEvenWithConfiguredDelay(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() {
		setDelay(t, conn, "d1", 0, 0)
		setPowerStartupStates(t, conn, map[string]int{"d1": 0})
	})

	setPowerStartupStates(t, conn, map[string]int{"d1": 2}) // Disabled
	setDelay(t, conn, "d1", 5, 5)                            // configured delay must not matter

	resp := setSwitch(t, conn, "d1", true)
	_, hasError := resp["error"]
	assert.True(t, hasError, "a Disabled output with a configured delay must still reject an enable attempt immediately, got: %v", resp)

	// Also confirm nothing was silently scheduled to fire later despite the rejection.
	time.Sleep(6 * time.Second)
	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d1"), "d1 must still be off after the delay window - the rejected enable must not have been scheduled anyway")
}

// TestDelayedAction_ConfiguredDelay_StandardSwitch covers the basic case: a configured delay_on_s/
// delay_off_s on a plain DC/USB switch defers a normal {"set":{key:state}} command, in both
// directions, without changing the switch's reported state until the delay actually elapses.
func TestDelayedAction_ConfiguredDelay_StandardSwitch(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	const delayS = 3
	setDelay(t, conn, "d4", delayS, delayS)

	setSwitch(t, conn, "d4", true)
	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must stay OFF immediately after a delayed-on command")

	time.Sleep(1500 * time.Millisecond)
	status = getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must still be OFF before the configured delay elapses")

	time.Sleep(2 * time.Second) // total ~3.5s > delayS
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 should have turned ON once the configured delay elapsed")

	setSwitch(t, conn, "d4", false)
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 must stay ON immediately after a delayed-off command")

	time.Sleep(3500 * time.Millisecond)
	status = getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 should have turned OFF once the configured delay elapsed")
}

// TestDelayedAction_ConfiguredDelay_AdjConv is the regression test for this round's feature
// request ("why isn't Adj. Port delay-switched") - covers both the plain boolean on/off path and
// the voltage-value path, since POWER_ADJ_CONV has its own dedicated branch in
// handle_set_power_command() separate from the "standard handling" one other switches use.
func TestDelayedAction_ConfiguredDelay_AdjConv(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() { setDelay(t, conn, "adj", 0, 0) })

	const delayS = 3

	t.Run("boolean_path", func(t *testing.T) {
		setDelay(t, conn, "adj", delayS, delayS)
		setSwitch(t, conn, "adj", true)
		status := getStatus(t, conn)
		assert.Equal(t, false, status["adj"], "adj must stay OFF immediately after a delayed-on command")

		time.Sleep(4 * time.Second)
		status = getStatus(t, conn)
		v, ok := status["adj"].(float64)
		require.True(t, ok, "adj should report a numeric voltage once turned on, got: %v", status["adj"])
		assert.Greater(t, v, 0.0)

		setSwitch(t, conn, "adj", false)
		time.Sleep(4 * time.Second)
		status = getStatus(t, conn)
		assert.Equal(t, false, status["adj"], "adj should have turned OFF once the configured delay elapsed")
	})

	t.Run("voltage_value_path", func(t *testing.T) {
		setDelay(t, conn, "adj", delayS, 0)
		respRaw := mustSendCommand(t, conn, `{"set":{"adj":7.0}}`, 3*time.Second)
		_ = respRaw
		status := getStatus(t, conn)
		assert.Equal(t, false, status["adj"], "adj must stay OFF immediately after a delayed voltage-on command")

		time.Sleep(4 * time.Second)
		status = getStatus(t, conn)
		v, ok := status["adj"].(float64)
		require.True(t, ok, "adj should report the requested voltage once the delay elapses, got: %v", status["adj"])
		assert.InDelta(t, 7.0, v, 0.5, "adj should reflect the requested 7V once actually enabled")

		setSwitch(t, conn, "adj", false)
	})
}

// TestDelayedAction_NewestWins verifies schedule_delayed_action()'s documented overwrite
// semantics: a second delayed command for the same output, sent before the first is due, replaces
// it entirely (direction included) rather than both firing.
func TestDelayedAction_NewestWins(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	setDelay(t, conn, "d4", 5, 5)
	setSwitch(t, conn, "d4", true) // schedules ON due at t+5s

	time.Sleep(1 * time.Second)
	setSwitch(t, conn, "d4", false) // overwrites the same queue slot: OFF due at t+1s+5s=t+6s

	// At the ORIGINAL due time (~t+5s), d4 must still be off - the ON entry was replaced, not
	// merely superseded by a later independent one.
	time.Sleep(4500 * time.Millisecond) // now at ~t+5.5s
	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "the original delayed-ON must not have fired - it should have been overwritten")

	// The newest (OFF) entry is a no-op for observable state (d4 was already off), but must not
	// itself have been dropped either - re-run with the roles reversed to observe a real flip.
	setSwitch(t, conn, "d4", true) // immediate baseline: on delay is configured too, so this schedules ON due at now+5s
	time.Sleep(1 * time.Second)
	setDelay(t, conn, "d4", 1, 5) // shrink the on-delay before rescheduling
	setSwitch(t, conn, "d4", true) // reschedules: overwrites the pending entry, now due at (now)+1s
	time.Sleep(2 * time.Second)
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "the newest (shorter-delay) schedule should have won and already fired")

	setSwitch(t, conn, "d4", false)
}

// TestDelayedAction_ImmediateCancelsStaleDelayed is the regression test for the single-output
// stale-timer bug found while writing this suite (sibling of the already-fixed Master Power one,
// see clear_delayed_action_for()'s doc comment in power_control.cpp): an immediate command must
// cancel any earlier, now-contradicting delayed action still pending for that same output -
// otherwise the stale one fires later and silently undoes the immediate command.
func TestDelayedAction_ImmediateCancelsStaleDelayed(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	setDelay(t, conn, "d4", 0, 5) // only the off direction is delayed

	setSwitch(t, conn, "d4", true) // immediate (on delay = 0)
	status := getStatus(t, conn)
	require.Equal(t, 1, switchStatusInt(t, status, "d4"))

	setSwitch(t, conn, "d4", false) // schedules OFF due at t+5s (off delay = 5)
	time.Sleep(500 * time.Millisecond)

	setSwitch(t, conn, "d4", true) // immediate again (on delay = 0) - must cancel the pending OFF above
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 should be ON immediately after the second immediate command")

	time.Sleep(6 * time.Second) // past the original (now-stale) 5s off-delay window
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 must remain ON - the stale delayed OFF from before the immediate re-ON must have been cancelled")

	setSwitch(t, conn, "d4", false)
}

// TestDelayedAction_MasterPowerClearsPendingQueue is the regression test for the Master-Power
// stale-timer bug found and fixed this session (clear_delayed_action_queue(), called from the
// "all" branch): a still-pending per-switch delayed action must not survive Master Power and fire
// later, undoing what Master Power just did.
func TestDelayedAction_MasterPowerClearsPendingQueue(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	setConfig(t, conn, `{"psd":0}`)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	setDelay(t, conn, "d4", 5, 0)
	setSwitch(t, conn, "d4", true) // schedules ON due at t+5s

	setAllPower(t, conn, false) // must clear the pending entry, in addition to the immediate all-off
	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"))

	time.Sleep(6 * time.Second) // past the original 5s window
	status = getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must stay OFF - the stale delayed-ON must not have survived Master Power Off")
}

// TestDelayedAction_MasterPowerOffAlwaysImmediate locks in the explicit safety rule that Master
// Power OFF always stays instantaneous, regardless of any configured per-switch off-delay - a
// user must always be able to cut all power immediately, even if an individual switch is
// configured with a deliberate off-delay for other purposes (e.g. letting a computer's own OS
// shutdown finish first). Note this is deliberately asymmetric: Master Power ON is explicitly
// allowed (by design, per the user's own request) to stack a switch's configured on-delay on top
// of its stagger-queue slot - see TestDelayedAction_MasterPowerOnStacksStaggerAndConfiguredDelay.
// Only the OFF direction gets this always-immediate guarantee, since only OFF is the safety-
// critical "cut power now" direction.
func TestDelayedAction_MasterPowerOffAlwaysImmediate(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	setConfig(t, conn, `{"psd":0}`)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	setDelay(t, conn, "d4", 0, 10) // deliberately long off-delay - if Master Power respected this, the assertion below would fail
	setSwitch(t, conn, "d4", true) // on-delay is 0 here, so this applies immediately
	status := getStatus(t, conn)
	require.Equal(t, 1, switchStatusInt(t, status, "d4"), "precondition: d4 should be on before testing Master Power Off")

	setAllPower(t, conn, false)
	status = getStatus(t, conn) // handle_set_power_command() only returns after acting, no wait needed
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "Master Power Off must be immediate, ignoring d4's configured 10s off-delay")

	setAllPower(t, conn, false) // idempotent cleanup
}

// TestDelayedAction_BootStacksOnStartupDelay is the regression test for the boot-time stacking
// bug found and fixed this session (setup_power_outputs() was missing the delay_on_s check that
// service_power_stagger_queue() already had): a switch configured to power on at boot AND carrying
// a configured on-delay must stay off for that long after boot, not turn on immediately.
func TestDelayedAction_BootStacksOnStartupDelay(t *testing.T) {
	port := hwtestPort()
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)

	const delayS = 8
	setDelay(t, conn, "d4", delayS, 0)
	setPowerStartupStates(t, conn, map[string]int{"d4": 1}) // d4 should power ON at boot
	t.Cleanup(func() {
		c := openConnForTest(t)
		setDelay(t, c, "d4", 0, 0)
		setPowerStartupStates(t, c, map[string]int{"d4": 0})
	})

	mustSendCommand(t, conn, `{"command":"reboot"}`, 3*time.Second)
	conn.Close() // release the port - a restart is already in flight

	time.Sleep(1500 * time.Millisecond) // long enough for the ESP32 to actually reset and re-boot far enough to accept a connection
	conn2, err := openSerialConn(port)
	require.NoError(t, err, "failed to reconnect after reboot")
	t.Cleanup(conn2.Close)
	rebootDetectedAt := time.Now()

	status := getStatus(t, conn2)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must still be OFF shortly after boot - it has a configured %ds on-delay", delayS)

	deadline := rebootDetectedAt.Add(25 * time.Second)
	turnedOnAfter := time.Duration(-1)
	for time.Now().Before(deadline) {
		status = getStatus(t, conn2)
		if switchStatusInt(t, status, "d4") == 1 {
			turnedOnAfter = time.Since(rebootDetectedAt)
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	require.GreaterOrEqual(t, turnedOnAfter, time.Duration(0), "d4 never turned on within the expected window after boot")
	assert.Greater(t, turnedOnAfter, 3*time.Second, "d4 turned on too soon after boot for an %ds configured delay (turned on after %v, measured from ~2s post-reboot-command) - boot-time delay stacking regressed", delayS, turnedOnAfter)

	setSwitch(t, conn2, "d4", false)
}

// TestDelayedAction_MasterPowerOnStacksStaggerAndConfiguredDelay verifies the explicitly requested
// stacking behavior for the ON direction: a switch's own configured on-delay applies on top of
// its place in the Power-On Stagger sequence during Master Power On, without holding up other
// outputs queued after it (the stagger queue keeps advancing at its normal cadence regardless of
// any individual output's own extra delay).
func TestDelayedAction_MasterPowerOnStacksStaggerAndConfiguredDelay(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	const staggerMs = 300
	setConfig(t, conn, fmt.Sprintf(`{"psd":%d}`, staggerMs))
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	const extraDelayS = 2
	setDelay(t, conn, "d4", extraDelayS, 0) // d4 (queue position 4 of 7) gets an extra 2s on top

	start := time.Now()
	setAllPower(t, conn, true)

	turnedOnAt := make(map[string]time.Duration)
	deadline := start.Add(time.Duration(extraDelayS)*time.Second + time.Duration(len(standardSwitchKeys)+2)*staggerMs*time.Millisecond)
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
			time.Sleep(50 * time.Millisecond)
		}
	}
	require.Len(t, turnedOnAt, len(standardSwitchKeys), "not all switches turned on within the expected window: %v", turnedOnAt)

	// d5 is queued immediately after d4 but carries no extra delay, so its own stagger-driven
	// enable happens right on schedule - meanwhile d4's is pushed out by extraDelayS on top of its
	// own (earlier) stagger slot. If stacking regressed (d4 treated with no extra delay), d4 would
	// turn on at roughly the same time as d5, or before it - not clearly after.
	assert.Greater(t, turnedOnAt["d4"], turnedOnAt["d5"],
		"d4 (with a %ds configured on-delay stacked on top of its stagger slot) should turn on well after d5 (no configured delay, later stagger slot): onsets=%v", extraDelayS, turnedOnAt)
	assert.Greater(t, turnedOnAt["d4"]-turnedOnAt["d5"], time.Duration(extraDelayS)*time.Second/2,
		"the gap between d4 and d5 turning on should reflect roughly d4's stacked extra delay: onsets=%v", turnedOnAt)

	setAllPower(t, conn, false)
}

// TestDelayedAction_ExplicitDelaySet covers the raw "delay_set" wire command directly (the one the
// proxy's DelayedOn/DelayedOff ASCOM Actions send) - distinct from the configured per-switch
// default exercised by the other tests in this file. Minutes is the wire command's only supported
// granularity (see main.cpp), so the basic-fire subtest necessarily takes about a minute.
func TestDelayedAction_ExplicitDelaySet(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)

	t.Run("unknown_port_rejected", func(t *testing.T) {
		resp := mustSendCommand(t, conn, `{"delay_set":{"port":"not_a_real_port","state":true,"minutes":1}}`, 3*time.Second)
		assert.Contains(t, resp, "error", "an unknown port name must be rejected: %s", resp)
	})

	t.Run("zero_minutes_rejected", func(t *testing.T) {
		resp := mustSendCommand(t, conn, `{"delay_set":{"port":"d4","state":true,"minutes":0}}`, 3*time.Second)
		assert.Contains(t, resp, "error", "minutes:0 must be rejected rather than firing immediately: %s", resp)
		status := getStatus(t, conn)
		assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "a rejected delay_set must not have changed d4's state")
	})

	t.Run("minutes_over_24h_rejected", func(t *testing.T) {
		// 1441 minutes is one over the 24h cap (MAX_SWITCH_DELAY_S) - rejected rather than scheduled.
		resp := mustSendCommand(t, conn, `{"delay_set":{"port":"d4","state":true,"minutes":1441}}`, 3*time.Second)
		assert.Contains(t, resp, "error", "minutes beyond the 24h cap must be rejected: %s", resp)
		status := getStatus(t, conn)
		assert.NotContains(t, status, "pd", "a rejected delay_set must not have scheduled anything: %v", status)
		assert.Equal(t, 0, switchStatusInt(t, status, "d4"))
	})
	t.Run("negative_minutes_safety", func(t *testing.T) {
		// Whatever exact value ArduinoJson's signed->unsigned conversion produces for a negative
		// "minutes" is not the point of this test (and not part of this feature's documented
		// contract) - what matters is that it's handled safely: no crash/hang, and critically, not
		// silently misinterpreted as "immediate" (0 minutes worth of delay).
		resp := mustSendCommand(t, conn, `{"delay_set":{"port":"d4","state":true,"minutes":-5}}`, 3*time.Second)
		assert.Contains(t, resp, "error", "a negative minutes value must be rejected outright: %s", resp)
		time.Sleep(3 * time.Second)
		status := getStatus(t, conn)
		assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "a negative minutes value must not be misinterpreted as an immediate (or near-immediate) switch")
	})

	t.Run("basic_fire_one_minute", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping ~60s wait in -short mode")
		}
		start := time.Now()
		resp := mustSendCommand(t, conn, `{"delay_set":{"port":"d4","state":true,"minutes":1}}`, 3*time.Second)
		assert.Contains(t, resp, `"ok":true`, "delay_set should ack with ok:true: %s", resp)

		status := getStatus(t, conn)
		assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must not be on immediately after scheduling a 1-minute delay")

		deadline := start.Add(80 * time.Second)
		turnedOn := false
		for time.Now().Before(deadline) {
			status = getStatus(t, conn)
			if switchStatusInt(t, status, "d4") == 1 {
				turnedOn = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		assert.True(t, turnedOn, "d4 should have turned on within ~80s of a 1-minute delay_set")
		assert.Greater(t, time.Since(start), 45*time.Second, "d4 turned on suspiciously early for a 1-minute delay_set")

		setSwitch(t, conn, "d4", false)
	})
}

// --- "pd": delayed switching that is still counting down, reported in the status ---------------
// See power_control.cpp's get_power_status_json()/get_delayed_action_pending(): the status gets an
// optional "pd" object mapping each output with a pending delayed action to [direction, seconds
// left] (direction 1 = on, 0 = off). Only affected outputs are listed and the key is missing
// entirely when nothing is pending, so an idle status line is exactly as long as before.

// pendingEntry reads one output's entry from the status's optional "pd" object. ok is false when
// the output isn't listed - including when there is no "pd" key at all, the normal idle case.
func pendingEntry(t *testing.T, status map[string]interface{}, key string) (turnOn bool, restS int, ok bool) {
	t.Helper()
	raw, present := status["pd"]
	if !present {
		return false, 0, false
	}
	pd, isMap := raw.(map[string]interface{})
	require.True(t, isMap, "\"pd\" must be an object: %v", raw)
	entry, listed := pd[key]
	if !listed {
		return false, 0, false
	}
	arr, isArr := entry.([]interface{})
	require.True(t, isArr && len(arr) == 2, "pd[%q] must be [direction, seconds]: %v", key, entry)
	dir, dirOK := arr[0].(float64)
	rest, restOK := arr[1].(float64)
	require.True(t, dirOK && restOK, "pd[%q] must hold two numbers: %v", key, entry)
	return dir == 1, int(rest), true
}

// pendingKeys lists the outputs present in the status's "pd" object (nil if there is none).
func pendingKeys(t *testing.T, status map[string]interface{}) []string {
	t.Helper()
	raw, present := status["pd"]
	if !present {
		return nil
	}
	pd, isMap := raw.(map[string]interface{})
	require.True(t, isMap, "\"pd\" must be an object: %v", raw)
	keys := make([]string, 0, len(pd))
	for k := range pd {
		keys = append(keys, k)
	}
	return keys
}

// TestDelayedAction_PendingReportedInStatus covers a pending delayed OFF end to end: absent while
// idle, listed (in the set response itself and in later status reads) with a shrinking countdown
// while the output keeps its old state, gone again once the delay has fired.
func TestDelayedAction_PendingReportedInStatus(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() {
		setDelay(t, conn, "d4", 0, 0)
		setSwitch(t, conn, "d4", false)
	})

	status := getStatus(t, conn)
	assert.NotContains(t, status, "pd", "an idle box must not report anything pending - the key is absent, not empty")

	setDelay(t, conn, "d4", 0, 6) // only the off direction is delayed
	resp := setSwitch(t, conn, "d4", true)
	require.Equal(t, 1, switchStatusInt(t, resp, "d4"), "on is not delayed, d4 must be on immediately")
	assert.NotContains(t, resp, "pd", "an immediate command must not leave anything pending")

	resp = setSwitch(t, conn, "d4", false) // schedules OFF due at t+6s
	turnOn, rest, ok := pendingEntry(t, resp, "d4")
	require.True(t, ok, "the set response itself must already list the pending off: %v", resp)
	assert.False(t, turnOn, "the pending action is an off")
	assert.InDelta(t, 6, rest, 1, "the countdown must start at the configured delay")
	assert.Equal(t, 1, switchStatusInt(t, resp, "d4"), "d4 must keep its old state (on) while the delay counts down")
	assert.ElementsMatch(t, []string{"d4"}, pendingKeys(t, resp), "only the affected output may be listed")

	time.Sleep(2 * time.Second)
	status = getStatus(t, conn)
	_, rest, ok = pendingEntry(t, status, "d4")
	require.True(t, ok, "the pending off must still be listed mid-countdown: %v", status)
	assert.InDelta(t, 4, rest, 1, "the countdown must have advanced by about 2s")
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 must still be on mid-countdown")

	time.Sleep(5 * time.Second) // ~7s in total, past the 6s delay
	status = getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "d4 must be off once the delay has elapsed")
	assert.NotContains(t, status, "pd", "pd must disappear once the delayed action has fired")
}

// TestDelayedAction_PendingOnCarriesDirection checks a pending delayed ON: direction 1, the output
// stays off until the delay has elapsed.
func TestDelayedAction_PendingOnCarriesDirection(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() {
		setDelay(t, conn, "d4", 0, 0)
		setSwitch(t, conn, "d4", false)
	})

	setDelay(t, conn, "d4", 5, 0)
	resp := setSwitch(t, conn, "d4", true)
	turnOn, rest, ok := pendingEntry(t, resp, "d4")
	require.True(t, ok, "the pending on must be listed: %v", resp)
	assert.True(t, turnOn, "the pending action is an on")
	assert.InDelta(t, 5, rest, 1)
	assert.Equal(t, 0, switchStatusInt(t, resp, "d4"), "d4 must stay off until the delay has elapsed")

	time.Sleep(6 * time.Second)
	status := getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "d4 must be on once the delay has elapsed")
	assert.NotContains(t, status, "pd", "pd must disappear once the delayed action has fired")
}

// TestDelayedAction_PendingOnlyListsAffectedOutputs makes sure the key stays small: with two
// outputs pending it lists exactly those two, and drops one as soon as its action is cancelled.
func TestDelayedAction_PendingOnlyListsAffectedOutputs(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() {
		setDelay(t, conn, "d4", 0, 0)
		setDelay(t, conn, "d5", 0, 0)
		setAllPower(t, conn, false)
	})

	setDelay(t, conn, "d4", 0, 8)
	setDelay(t, conn, "d5", 0, 8)
	setSwitch(t, conn, "d4", true)
	setSwitch(t, conn, "d5", true)
	setSwitch(t, conn, "d4", false)
	resp := setSwitch(t, conn, "d5", false)
	assert.ElementsMatch(t, []string{"d4", "d5"}, pendingKeys(t, resp), "exactly the two pending outputs must be listed, nothing else: %v", resp)

	setDelay(t, conn, "d4", 0, 0)
	resp = setSwitch(t, conn, "d4", true) // immediate again: cancels d4's pending off
	assert.ElementsMatch(t, []string{"d5"}, pendingKeys(t, resp), "d4 must drop out once its pending action is cancelled: %v", resp)
}

// TestDelayedAction_PendingEndsWhenCancelledOrRestarted covers the three ways a pending action
// stops being what the status says: an immediate command for the same output cancels it, Master
// Power cancels everything, and repeating the delayed command restarts the countdown.
func TestDelayedAction_PendingEndsWhenCancelledOrRestarted(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	setConfig(t, conn, `{"psd":0}`)
	t.Cleanup(func() {
		setDelay(t, conn, "d4", 0, 0)
		setAllPower(t, conn, false)
	})

	t.Run("immediate_command_cancels", func(t *testing.T) {
		setAllPower(t, conn, false)
		setDelay(t, conn, "d4", 0, 5)
		setSwitch(t, conn, "d4", true)
		resp := setSwitch(t, conn, "d4", false)
		_, _, ok := pendingEntry(t, resp, "d4")
		require.True(t, ok, "precondition: the off must be pending: %v", resp)

		resp = setSwitch(t, conn, "d4", true) // on is not delayed: immediate, cancels the pending off
		assert.NotContains(t, resp, "pd", "an immediate command for the same output must cancel the pending one: %v", resp)
		assert.Equal(t, 1, switchStatusInt(t, resp, "d4"))
	})

	t.Run("master_power_cancels", func(t *testing.T) {
		setAllPower(t, conn, false)
		setDelay(t, conn, "d4", 5, 0)
		resp := setSwitch(t, conn, "d4", true)
		_, _, ok := pendingEntry(t, resp, "d4")
		require.True(t, ok, "precondition: the on must be pending: %v", resp)

		setAllPower(t, conn, false)
		status := getStatus(t, conn)
		assert.NotContains(t, status, "pd", "Master Power must clear every pending action: %v", status)
	})

	t.Run("repeated_command_restarts_countdown", func(t *testing.T) {
		setAllPower(t, conn, false)
		setDelay(t, conn, "d4", 0, 6)
		setSwitch(t, conn, "d4", true)
		resp := setSwitch(t, conn, "d4", false)
		_, rest, ok := pendingEntry(t, resp, "d4")
		require.True(t, ok)
		assert.InDelta(t, 6, rest, 1)

		time.Sleep(3 * time.Second)
		resp = setSwitch(t, conn, "d4", false) // same command again: the newest request restarts the delay
		_, rest, ok = pendingEntry(t, resp, "d4")
		require.True(t, ok, "the repeated off must still be pending: %v", resp)
		assert.InDelta(t, 6, rest, 1, "repeating the command must restart the countdown from the full delay")

		setSwitch(t, conn, "d4", true) // cancel
	})
}

// TestDelayedAction_DelayCappedAt24Hours covers the defensive clamp in updateConfig(): a delay
// beyond 86400s (24h) - or one that would not even fit 32 bit milliseconds - is capped rather than
// stored, and negative values count as 0 (no delay).
func TestDelayedAction_DelayCappedAt24Hours(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	baselineNoDelay(t, conn)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	delaysOf := func(cfg map[string]interface{}) (on, off float64) {
		dl, ok := cfg["dl"].(map[string]interface{})
		require.True(t, ok, "config echo must contain dl: %v", cfg)
		d4, ok := dl["d4"].(map[string]interface{})
		require.True(t, ok, "dl must contain d4: %v", dl)
		return d4["on"].(float64), d4["off"].(float64)
	}

	on, off := delaysOf(setConfig(t, conn, `{"dl":{"d4":{"on":999999999,"off":86401}}}`))
	assert.Equal(t, float64(86400), on, "an absurd on-delay must be capped at 24h, not wrap or become 0")
	assert.Equal(t, float64(86400), off, "one second over the cap must be capped")

	on, off = delaysOf(setConfig(t, conn, `{"dl":{"d4":{"on":86400,"off":-5}}}`))
	assert.Equal(t, float64(86400), on, "exactly the cap must be kept as is")
	assert.Equal(t, float64(0), off, "a negative delay must count as 0 (no delay)")

	on, _ = delaysOf(setConfig(t, conn, `{"dl":{"d4":{"on":5000000000,"off":0}}}`))
	assert.Equal(t, float64(86400), on, "a value beyond 32 bit must be capped too, not converted to 0")
}