//go:build hwtest

package hwtest

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBurnIn formalizes this session's manual ~20-minute heap-stability check (repeated
// switch/heater/config operations while watching ESP.getMaxAllocHeap() for fragmentation).
// Opt-in only (BURNIN=1) since it's meant to run far longer than the rest of the suite; duration
// via BURNIN_DURATION (default 5m smoke test; 30-60m+ recommended before a firmware release).
//
// Unlike every other test in this suite, actions here are deliberately fired back-to-back with
// no pacing beyond each command's own round-trip - a genuine worst-case flood no real client
// would produce, but useful for finding rare timing issues. A single slow/dropped response under
// that load is tolerated and counted rather than treated as a hard failure (that's what
// mustSendCommand's t.Fatalf would do); only a run of several *consecutive* failures, or an
// overall failure rate above a small threshold, fails the test - either is a sign the device
// stopped responding properly rather than one response merely arriving late.
func TestBurnIn(t *testing.T) {
	if os.Getenv("BURNIN") != "1" {
		t.Skip("burn-in test skipped by default - set BURNIN=1 to run it (see README.md)")
	}
	duration := 5 * time.Minute
	if d := os.Getenv("BURNIN_DURATION"); d != "" {
		parsed, err := time.ParseDuration(d)
		require.NoError(t, err, "invalid BURNIN_DURATION %q (expected a Go duration like \"5m\" or \"1h\")", d)
		duration = parsed
	}

	conn := openConnForTest(t)
	baselineEnabledOff(t, conn)

	type heapSample struct {
		at                       time.Duration
		hf, hmf, hma, hs int64
	}
	var samples []heapSample

	baseline, err := getSensors(conn)
	require.NoError(t, err)
	baselineHma := int64(asFloat(t, baseline["hma"]))
	t.Logf("burn-in: baseline heap - hf=%v hmf=%v hma=%v hs=%v", baseline["hf"], baseline["hmf"], baseline["hma"], baseline["hs"])

	const cmdTimeout = 8 * time.Second // generous: this loop hammers the device harder than any real client

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Each action returns an error instead of failing the test directly (see doc comment above).
	actions := []func() error{
		func() error {
			k := standardSwitchKeys[rng.Intn(len(standardSwitchKeys))]
			cmd := fmt.Sprintf(`{"set":{%q:%v}}`, k, rng.Intn(2) == 1)
			_, err := conn.sendCommand(cmd, cmdTimeout)
			return err
		},
		func() error {
			cmd := fmt.Sprintf(`{"set":{"all":%v}}`, rng.Intn(2) == 1)
			_, err := conn.sendCommand(cmd, cmdTimeout)
			return err
		},
		func() error {
			modes := []int{0, 1, 2, 4, 5} // skip Sync (3) here - it depends on the other heater's mode
			hi := rng.Intn(2)
			m := modes[rng.Intn(len(modes))]
			fields := fmt.Sprintf(`"m":%d`, m)
			if m == 0 {
				fields += fmt.Sprintf(`,"mp":%d`, rng.Intn(101))
			}
			dh := []string{"{}", "{}"}
			dh[hi] = "{" + fields + "}"
			cmd := fmt.Sprintf(`{"sc":{"dh":[%s,%s]}}`, dh[0], dh[1])
			_, err := conn.sendCommand(cmd, cmdTimeout)
			return err
		},
		func() error {
			cmd := fmt.Sprintf(`{"sc":{"psd":%d}}`, rng.Intn(1001))
			_, err := conn.sendCommand(cmd, cmdTimeout)
			return err
		},
		func() error { _, err := conn.sendCommand(`{"get":"sensors"}`, cmdTimeout); return err },
		func() error { _, err := conn.sendCommand(`{"get":"status"}`, cmdTimeout); return err },
		func() error { _, err := conn.sendCommand(`{"get":"config"}`, cmdTimeout); return err },
	}

	start := time.Now()
	lastSample := start
	iterations := 0
	failures := 0
	consecutiveFailures := 0
	const maxConsecutiveFailures = 5
	const maxFailureRate = 0.02 // 2%

	for time.Since(start) < duration {
		if err := actions[rng.Intn(len(actions))](); err != nil {
			failures++
			consecutiveFailures++
			t.Logf("burn-in: action failed (iteration %d, %d consecutive so far): %v", iterations, consecutiveFailures, err)
			if consecutiveFailures >= maxConsecutiveFailures {
				t.Fatalf("burn-in: %d consecutive command failures at iteration %d - device appears unresponsive: %v", consecutiveFailures, iterations, err)
			}
		} else {
			consecutiveFailures = 0
		}
		iterations++

		if time.Since(lastSample) >= 5*time.Second {
			if s, err := getSensors(conn); err == nil {
				sample := heapSample{
					at:  time.Since(start),
					hf:  int64(asFloat(t, s["hf"])),
					hmf: int64(asFloat(t, s["hmf"])),
					hma: int64(asFloat(t, s["hma"])),
					hs:  int64(asFloat(t, s["hs"])),
				}
				samples = append(samples, sample)
				t.Logf("burn-in: t=%v hf=%d hmf=%d hma=%d hs=%d (iteration %d, %d failures so far)",
					sample.at.Round(time.Second), sample.hf, sample.hmf, sample.hma, sample.hs, iterations, failures)
			}
			lastSample = time.Now()
		}
	}

	failureRate := float64(failures) / float64(iterations)
	t.Logf("burn-in: completed %d iterations over %v (%d failures, %.2f%% failure rate)",
		iterations, time.Since(start).Round(time.Second), failures, failureRate*100)
	assert.LessOrEqual(t, failureRate, maxFailureRate,
		"command failure rate %.2f%% exceeded %.0f%% threshold under sustained load (%d/%d failed)",
		failureRate*100, maxFailureRate*100, failures, iterations)

	final, err := getSensors(conn)
	require.NoError(t, err)
	finalHma := int64(asFloat(t, final["hma"]))
	t.Logf("burn-in: final heap - hf=%v hmf=%v hma=%v hs=%v", final["hf"], final["hmf"], final["hma"], final["hs"])

	// hma (ESP.getMaxAllocHeap - largest contiguous free block) is the actual fragmentation
	// indicator. It stayed byte-identical across ~20 minutes of manual mixed/abusive testing
	// this session, so a small tolerance here is just for measurement noise, not expected drift.
	const hmaTolerance = 2048
	assert.GreaterOrEqual(t, finalHma, baselineHma-hmaTolerance,
		"hma (largest contiguous free heap block) dropped from %d to %d - possible heap fragmentation", baselineHma, finalHma)

	if len(samples) >= 4 {
		half := len(samples) / 2
		var firstHf, secondHf, firstHmf, secondHmf int64
		for i, s := range samples {
			if i < half {
				firstHf += s.hf
				firstHmf += s.hmf
			} else {
				secondHf += s.hf
				secondHmf += s.hmf
			}
		}
		firstHfAvg := float64(firstHf) / float64(half)
		secondHfAvg := float64(secondHf) / float64(len(samples)-half)
		firstHmfAvg := float64(firstHmf) / float64(half)
		secondHmfAvg := float64(secondHmf) / float64(len(samples)-half)

		const leakTolerancePct = 0.05 // allow up to 5% drift before flagging a possible leak trend
		assert.GreaterOrEqual(t, secondHfAvg, firstHfAvg*(1-leakTolerancePct),
			"free heap (hf) trended downward across the run (first-half avg %.0f -> second-half avg %.0f) - possible leak", firstHfAvg, secondHfAvg)
		assert.GreaterOrEqual(t, secondHmfAvg, firstHmfAvg*(1-leakTolerancePct),
			"min-ever-free heap (hmf) trended downward across the run (first-half avg %.0f -> second-half avg %.0f) - possible leak", firstHmfAvg, secondHmfAvg)
	}

	setAllPower(t, conn, false)
	setConfig(t, conn, `{"dh":[{"m":5},{"m":5}],"psd":500}`)
}
