//go:build hwtest

package hwtest

import (
	"fmt"
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

// TestDelayedAction_ConfiguredDelay_StandardSwitch covers the basic case: a configured delay_on_s/
// delay_off_s on a plain DC/USB switch defers a normal {"set":{key:state}} command, in both
// directions, without changing the switch's reported state until the delay actually elapses.
func TestDelayedAction_ConfiguredDelay_StandardSwitch(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
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

// TestDelayedAction_MasterPowerAlwaysImmediate locks in the explicit safety rule that Master Power
// itself always stays instantaneous, regardless of any configured per-switch delay - a user must
// always be able to cut all power immediately, even if individual switches are configured with a
// deliberate on/off delay for other purposes.
func TestDelayedAction_MasterPowerAlwaysImmediate(t *testing.T) {
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
	setConfig(t, conn, `{"psd":0}`)
	t.Cleanup(func() { setDelay(t, conn, "d4", 0, 0) })

	setDelay(t, conn, "d4", 10, 10) // deliberately long - if Master Power respected this, the assertions below would fail
	setSwitch(t, conn, "d4", true)
	time.Sleep(11 * time.Second) // let the delayed-on actually take effect first, so d4 is really on
	status := getStatus(t, conn)
	require.Equal(t, 1, switchStatusInt(t, status, "d4"), "precondition: d4 should be on before testing Master Power Off")

	setAllPower(t, conn, false)
	status = getStatus(t, conn) // handle_set_power_command() only returns after acting, no wait needed
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"), "Master Power Off must be immediate, ignoring d4's configured 10s off-delay")

	setAllPower(t, conn, true)
	time.Sleep(500 * time.Millisecond) // service_power_stagger_queue() drains ~100ms/tick
	status = getStatus(t, conn)
	assert.Equal(t, 1, switchStatusInt(t, status, "d4"), "Master Power On must be immediate for d4 itself, ignoring its configured 10s on-delay (stacking is opt-in per-switch behavior for staggered enable timing, not for Master Power's own decisiveness)")

	setAllPower(t, conn, false)
}

// TestDelayedAction_BootStacksOnStartupDelay is the regression test for the boot-time stacking
// bug found and fixed this session (setup_power_outputs() was missing the delay_on_s check that
// service_power_stagger_queue() already had): a switch configured to power on at boot AND carrying
// a configured on-delay must stay off for that long after boot, not turn on immediately.
func TestDelayedAction_BootStacksOnStartupDelay(t *testing.T) {
	port := hwtestPort()
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)

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

	t.Run("negative_minutes_safety", func(t *testing.T) {
		// Whatever exact value ArduinoJson's signed->unsigned conversion produces for a negative
		// "minutes" is not the point of this test (and not part of this feature's documented
		// contract) - what matters is that it's handled safely: no crash/hang, and critically, not
		// silently misinterpreted as "immediate" (0 minutes worth of delay).
		mustSendCommand(t, conn, `{"delay_set":{"port":"d4","state":true,"minutes":-5}}`, 3*time.Second)
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
