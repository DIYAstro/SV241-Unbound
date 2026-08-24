//go:build hwtest

package hwtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// bootableSwitchKeys are the switches whose startup state (setup_power_outputs(),
// power_control.cpp:52-102) this test exercises. adj_conv is included but needs special
// handling (its status is a voltage float or `false`, not a plain 0/1 int).
var bootableSwitchKeys = []string{"d1", "d2", "d3", "d4", "d5", "u12", "u34", "adj"}

// assertBootState checks a switch's reported status against its configured startup state
// (0=Off, 1=On, 2=Disabled) right after a real device boot. Off and Disabled both mean
// physically off (see power_control.cpp:92-93's "0 -> Off, 1 -> On, 2 -> Disabled (Off)").
func assertBootState(t *testing.T, status map[string]interface{}, key string, wantState int) {
	t.Helper()
	if key == "adj" {
		v := status["adj"]
		if wantState == 1 {
			f, ok := v.(float64)
			assert.True(t, ok && f > 0, "adj configured On (1) should boot with a positive preset voltage, got %v", v)
		} else {
			assert.Equal(t, false, v, "adj configured startup state %d should boot off, got %v", wantState, v)
		}
		return
	}
	got := switchStatusInt(t, status, key)
	want := 0
	if wantState == 1 {
		want = 1
	}
	label := map[int]string{0: "Off", 1: "On", 2: "Disabled"}[wantState]
	assert.Equal(t, want, got, "%q configured startup state %s should boot to %d, got %d", key, label, want, got)
}

// TestSwitches_StartupStatesAppliedOnBoot verifies setup_power_outputs() correctly applies all
// three startup states across a real reboot - not just that "ps" round-trips through sc/config
// (already covered elsewhere), but that the device actually *boots* into the configured state.
// Covers all 8 switches (7 standard + adj_conv) in all 3 states (Off/On/Disabled) across 3 reboot
// cycles, cyclically shifting which switch gets which state each round (a small Latin square) so
// every switch is verified in every state at least once, in 3 reboots rather than 24.
func TestSwitches_StartupStatesAppliedOnBoot(t *testing.T) {
	port := hwtestPort()
	states := []int{0, 1, 2}

	for round := 0; round < 3; round++ {
		round := round
		t.Run(fmt.Sprintf("round%d", round), func(t *testing.T) {
			desired := make(map[string]int, len(bootableSwitchKeys))
			psStates := make(map[string]int, len(bootableSwitchKeys))
			for i, k := range bootableSwitchKeys {
				s := states[(i+round)%3]
				desired[k] = s
				psStates[k] = s
			}

			conn := openConnForTest(t)
			setPowerStartupStates(t, conn, psStates)
			conn.Close() // release the port before resetting

			resetDevice(t, port)
			time.Sleep(3 * time.Second) // allow full boot, including staggered startup enables

			conn2 := openConnForTest(t)
			status := getStatus(t, conn2)
			for _, k := range bootableSwitchKeys {
				assertBootState(t, status, k, desired[k])
			}

			// A switch that booted Disabled must also still reject a live enable attempt.
			for _, k := range bootableSwitchKeys {
				if desired[k] != 2 {
					continue
				}
				resp := setSwitch(t, conn2, k, true)
				_, hasError := resp["error"]
				assert.True(t, hasError, "%q booted Disabled but accepted a live enable attempt", k)
			}
		})
	}

	// Leave the device in a safe, known state for subsequent tests.
	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)
}
