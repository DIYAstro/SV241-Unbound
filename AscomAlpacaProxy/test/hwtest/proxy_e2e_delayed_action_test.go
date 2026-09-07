//go:build hwtest

package hwtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alpacaActionResult is the subset of the standard Alpaca response shape (responses.go's
// Response/ValueResponse) these tests care about.
type alpacaActionResult struct {
	Value        string `json:"Value"`
	ErrorNumber  int    `json:"ErrorNumber"`
	ErrorMessage string `json:"ErrorMessage"`
}

// switchAction calls the Switch device's custom "action" endpoint (Action/Parameters query
// params, matching the SetSwitchName pattern already used elsewhere in this package - ASCOM
// normally sends these as a PUT body, but HandleSwitchAction reads via r.Form, which ParseForm()
// populates from the URL query regardless of method/Content-Type).
func (p *proxyProcess) switchAction(t *testing.T, action, paramsJSON string) (*http.Response, alpacaActionResult) {
	t.Helper()
	path := fmt.Sprintf("/api/v1/switch/0/action?Action=%s&Parameters=%s&ClientID=1&ClientTransactionID=1",
		url.QueryEscape(action), url.QueryEscape(paramsJSON))
	resp, body := p.post(t, path, "")
	var r alpacaActionResult
	require.NoError(t, json.Unmarshal(body, &r), "raw body: %s", body)
	return resp, r
}

// setSwitchNames replaces the proxy's entire switchNames map via POST /api/v1/settings, following
// the same read-modify-write pattern as TestProxyE2E's Settings_GetPost subtest. Returns the raw
// HTTP response so callers can assert success or rejection (e.g. a name collision).
func setSwitchNames(t *testing.T, proxy *proxyProcess, names map[string]string) (*http.Response, []byte) {
	t.Helper()
	_, body := proxy.get(t, "/api/v1/settings")
	var settings struct {
		ProxyConfig map[string]interface{} `json:"proxy_config"`
	}
	require.NoError(t, json.Unmarshal(body, &settings))
	settings.ProxyConfig["switchNames"] = names
	payload, err := json.Marshal(settings.ProxyConfig)
	require.NoError(t, err)
	return proxy.post(t, "/api/v1/settings", string(payload))
}

// setProxySetting sets a single top-level field in the proxy's settings via the same
// read-modify-write pattern, for tests that only need to flip one flag (e.g.
// enableAlpacaVoltageControl) without touching anything else currently configured.
func setProxySetting(t *testing.T, proxy *proxyProcess, key string, value interface{}) (*http.Response, []byte) {
	t.Helper()
	_, body := proxy.get(t, "/api/v1/settings")
	var settings struct {
		ProxyConfig map[string]interface{} `json:"proxy_config"`
	}
	require.NoError(t, json.Unmarshal(body, &settings))
	settings.ProxyConfig[key] = value
	payload, err := json.Marshal(settings.ProxyConfig)
	require.NoError(t, err)
	return proxy.post(t, "/api/v1/settings", string(payload))
}

// TestProxyE2E_DelayedAction covers the proxy-layer half of the delayed on/off feature: the
// "DelayedOn"/"DelayedOff" custom ASCOM Actions (handleDelayedAction, internal/alpaca/handlers.go)
// - resolving a switch by the display name an ASCOM client actually shows the user (not its
// internal/short key or numeric switch Id), and the name-collision guard that keeps that
// resolution unambiguous. The delayed action's actual firmware-side firing is covered by
// firmware_delayed_action_test.go (TestDelayedAction_ExplicitDelaySet) and by
// TestProxyE2E_DelayedAction_HostIndependence below - these subtests only exercise the proxy's own
// request handling, so they don't need to wait out a real delay.
func TestProxyE2E_DelayedAction(t *testing.T) {
	proxy := startProxy(t, hwtestPort())

	t.Run("ResolvesByCustomDisplayName", func(t *testing.T) {
		resp, body := setSwitchNames(t, proxy, map[string]string{"dc4": "My Delay Switch"})
		require.Equal(t, http.StatusOK, resp.StatusCode, "raw body: %s", body)

		resp2, r := proxy.switchAction(t, "DelayedOn", `{"switch":"My Delay Switch","minutes":1}`)
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		assert.Equal(t, 0, r.ErrorNumber, "DelayedOn should resolve a switch by its current custom display name: %+v", r)
	})

	t.Run("ResolvesByDefaultIdentityName", func(t *testing.T) {
		// A switch that was never explicitly renamed still has an entry in SwitchNames seeded to
		// its own internal long key (see config.go's SwitchNames defaulting, syncActiveProfileFromFlatLocked) -
		// so the Action must also resolve a switch addressed by that default identity name.
		resp, body := setSwitchNames(t, proxy, map[string]string{"dc5": "dc5"})
		require.Equal(t, http.StatusOK, resp.StatusCode, "raw body: %s", body)

		resp2, r := proxy.switchAction(t, "DelayedOff", `{"switch":"dc5","minutes":1}`)
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		assert.Equal(t, 0, r.ErrorNumber, "DelayedOff should resolve a switch by its default (unrenamed) identity name: %+v", r)
	})

	t.Run("UnknownSwitchNameRejected", func(t *testing.T) {
		resp, r := proxy.switchAction(t, "DelayedOn", `{"switch":"Not A Real Switch Name","minutes":1}`)
		require.Equal(t, http.StatusOK, resp.StatusCode, "Alpaca errors are reported via ErrorNumber, not HTTP status")
		assert.NotEqual(t, 0, r.ErrorNumber, "an unresolvable switch name must be rejected: %+v", r)
	})

	t.Run("InvalidParametersRejected", func(t *testing.T) {
		t.Run("missing_switch", func(t *testing.T) {
			_, r := proxy.switchAction(t, "DelayedOn", `{"minutes":1}`)
			assert.NotEqual(t, 0, r.ErrorNumber, "Parameters without a switch name must be rejected: %+v", r)
		})
		t.Run("zero_minutes", func(t *testing.T) {
			_, r := proxy.switchAction(t, "DelayedOn", `{"switch":"dc4","minutes":0}`)
			assert.NotEqual(t, 0, r.ErrorNumber, "minutes:0 must be rejected: %+v", r)
		})
		t.Run("negative_minutes", func(t *testing.T) {
			_, r := proxy.switchAction(t, "DelayedOn", `{"switch":"dc4","minutes":-5}`)
			assert.NotEqual(t, 0, r.ErrorNumber, "negative minutes must be rejected: %+v", r)
		})
		t.Run("malformed_json", func(t *testing.T) {
			_, r := proxy.switchAction(t, "DelayedOn", `not json`)
			assert.NotEqual(t, 0, r.ErrorNumber, "malformed Parameters must be rejected: %+v", r)
		})
	})

	t.Run("UnsupportedActionRejected", func(t *testing.T) {
		_, r := proxy.switchAction(t, "SomeUnknownAction", `{}`)
		assert.NotEqual(t, 0, r.ErrorNumber, "an unsupported Action name must be rejected: %+v", r)
	})

	// Regression test for the collision-prevention rule added alongside display-name resolution:
	// a switch must not be nameable to another switch's own long or short key, or the reverse
	// lookup GetInternalNameForDisplayName relies on would become ambiguous.
	t.Run("NameCollisionRejected", func(t *testing.T) {
		resp, body := setSwitchNames(t, proxy, map[string]string{"dc4": "dc5"}) // dc5's own long key
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "renaming dc4 to dc5's own key must be rejected: %s", body)

		resp2, body2 := setSwitchNames(t, proxy, map[string]string{"dc4": "u12"}) // usbc12's short key
		assert.Equal(t, http.StatusBadRequest, resp2.StatusCode, "renaming dc4 to u12's own short key must be rejected: %s", body2)

		// A switch renaming itself to its own key is explicitly allowed (that's just the
		// unmodified default - see reservedIdentifiersExcluding's doc comment).
		resp3, body3 := setSwitchNames(t, proxy, map[string]string{"dc4": "dc4"})
		assert.Equal(t, http.StatusOK, resp3.StatusCode, "a switch may always be named its own key: %s", body3)
	})

	// Restore identity names so no custom name leaks into other tests in this package.
	setSwitchNames(t, proxy, map[string]string{"dc4": "dc4", "dc5": "dc5"})
}

// TestProxyE2E_DelayedAction_HostIndependence is the formalized version of this feature's most
// important manual test performed while building it: a delayed action scheduled through the
// proxy's DelayedOff Action must keep counting down and actually fire in the ESP32's own firmware
// even if the proxy process is killed entirely partway through - the whole point of living in
// firmware rather than the proxy (see backlog/delayed-off-action.md's rationale: a NINA sequencer
// step can't "wait then switch off" if the host running the sequence is itself what's being cut).
func TestProxyE2E_DelayedAction_HostIndependence(t *testing.T) {
	port := hwtestPort()
	proxy := startProxy(t, port)
	// The switch ID layout is dynamic (see findSwitchIDByName's doc comment) - resolve d4's actual
	// Id rather than assuming a fixed one.
	proxy.post(t, "/api/v1/config/set", `{"ps":{"d4":0}}`)
	d4SwitchID := findSwitchIDByName(t, proxy, "dc4")

	// Baseline: d4 on, immediately (no leftover configured delay at this point).
	proxy.post(t, "/api/v1/config/set", `{"dl":{"d4":{"on":0,"off":0}}}`)
	proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&Value=1&ClientID=1&ClientTransactionID=1", d4SwitchID), "")
	assertPowerStatusEventually(t, proxy, "d4", 1, "baseline setup before scheduling DelayedOff")

	setSwitchNames(t, proxy, map[string]string{"dc4": "Host Independence Test Switch"})

	start := time.Now()
	resp, r := proxy.switchAction(t, "DelayedOff", `{"switch":"Host Independence Test Switch","minutes":1}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 0, r.ErrorNumber, "DelayedOff should have been accepted: %+v", r)

	// Give the proxy a brief moment to actually flush the delay_set command over serial before
	// killing it - the Action responds immediately per the ASCOM spec and sends the command from
	// its own background goroutine (see handleDelayedAction).
	time.Sleep(1 * time.Second)

	require.NotNil(t, proxy.cmd.Process, "proxy process handle missing")
	require.NoError(t, proxy.cmd.Process.Kill(), "failed to kill the proxy process")
	_, _ = proxy.cmd.Process.Wait()

	// Confirm the proxy is really gone before relying on its absence being the point of this test.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := proxy.client.Get(proxy.baseURL + "/api/v1/proxy/version"); err != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Wait out the rest of the 1-minute delay with the proxy dead the entire time, then connect
	// directly to the device over serial (bypassing the proxy entirely, since it no longer exists)
	// to confirm the firmware turned d4 off on its own.
	remaining := 65*time.Second - time.Since(start)
	if remaining > 0 {
		time.Sleep(remaining)
	}

	conn, err := openSerialConn(port)
	require.NoError(t, err, "failed to open a direct serial connection after killing the proxy")
	defer conn.Close()

	status := getStatus(t, conn)
	assert.Equal(t, 0, switchStatusInt(t, status, "d4"),
		"d4 should have turned off on its own (~1 minute after DelayedOff was scheduled) even though the proxy process was killed and never ran during the wait")
}

// waitAdjStatus polls /api/v1/power/status until status["adj"] satisfies match, or fails the
// test. handleGetPowerStatus serves a periodically-refreshed cache (~5s tick, same as
// assertPowerStatusEventually above covers for other keys) rather than a live re-query - a single
// immediate GET right after a "set" call can read a stale pre-command snapshot, so only polling
// is a reliable way to observe a change here (found the hard way: an early version of this test
// read adj as still-6.5V-from-a-previous-subtest's-cache immediately after what should have been
// a fresh, verified-off baseline).
func waitAdjStatus(t *testing.T, proxy *proxyProcess, match func(v interface{}) bool, context string) interface{} {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last interface{}
	for time.Now().Before(deadline) {
		_, body := proxy.get(t, "/api/v1/power/status")
		var status map[string]interface{}
		if json.Unmarshal(body, &status) == nil {
			last = status["adj"]
			if match(last) {
				return last
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("power status cache never reflected %s (key \"adj\", last seen %v)", context, last)
	return nil
}

func adjIsOff(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return !b
	}
	if f, ok := v.(float64); ok {
		return f == 0
	}
	return false
}

func adjIsNear(want float64) func(interface{}) bool {
	return func(v interface{}) bool {
		f, ok := v.(float64)
		return ok && f >= want-0.5 && f <= want+0.5
	}
}

func adjIsOnAtAll(v interface{}) bool {
	_, ok := v.(float64)
	return ok
}

// findSwitchIDByName returns the Alpaca switch Id whose getswitchname matches wantName, polling
// for up to 5s. The switch ID layout is dynamic - serial.SyncFirmwareConfig rebuilds it
// contiguously based on which switches/sensors are currently active (a Disabled standard switch,
// or a heater in a mode that hides its lens-temp slot, shifts every later Id down), and that
// rebuild runs in its own background goroutine after every /api/v1/config/set call rather than
// synchronously - so a freshly-changed configuration isn't reflected in maxswitch/getswitchname
// immediately. Hardcoding an Id (as an earlier version of this test did, assuming the package's
// default/pre-sync SwitchIDMap literal) is not reliable once a real sync has run at least once.
// scanForSwitchID does one pass over 0..maxswitch-1 via getswitchname, returning the first Id
// matching wantName (-1 if none).
func scanForSwitchID(t *testing.T, proxy *proxyProcess, wantName string) int {
	t.Helper()
	_, body := proxy.get(t, "/api/v1/switch/0/maxswitch?ClientID=1&ClientTransactionID=1")
	var mr struct {
		Value int `json:"Value"`
	}
	if json.Unmarshal(body, &mr) != nil {
		return -1
	}
	for id := 0; id < mr.Value; id++ {
		_, nb := proxy.get(t, fmt.Sprintf("/api/v1/switch/0/getswitchname?Id=%d&ClientID=1&ClientTransactionID=1", id))
		var nr struct {
			Value string `json:"Value"`
		}
		if json.Unmarshal(nb, &nr) == nil && nr.Value == wantName {
			return id
		}
	}
	return -1
}

// findSwitchIDByName returns the Alpaca switch Id whose getswitchname matches wantName, polling
// for up to 8s. The switch ID layout is dynamic - serial.SyncFirmwareConfig rebuilds it
// contiguously based on which switches/sensors are currently active (a Disabled standard switch,
// or a heater in a mode that hides its lens-temp slot, shifts every later Id down) - and, found
// the hard way, more than one SyncFirmwareConfig call can end up in flight at once (each
// /api/v1/config/set - and evidently /api/v1/settings too - triggers its own resync goroutine),
// briefly producing a transient/intermediate mapping before the final one settles. A single
// getswitchname match isn't proof of THAT: it only proves the name matched at that one instant, so
// this requires the SAME Id twice in a row (500ms apart) before trusting it - filters out exactly
// that churn. Hardcoding an Id (as an earlier version of this test did, assuming the package's
// default/pre-sync SwitchIDMap literal) is not reliable once a real sync has run at least once.
func findSwitchIDByName(t *testing.T, proxy *proxyProcess, wantName string) int {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	lastID := -1
	for time.Now().Before(deadline) {
		id := scanForSwitchID(t, proxy, wantName)
		if id >= 0 && id == lastID {
			return id
		}
		lastID = id
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("could not find a STABLE switch Id named %q within the timeout (switch ID layout is dynamic - see serial.SyncFirmwareConfig; last seen Id was %d)", wantName, lastID)
	return -1
}

// TestProxyE2E_DelayedAction_AdjConvVoltageControl covers the interaction between the proxy's
// "Enable Variable Voltage Control (Alpaca & WebUI)" setting (EnableAlpacaVoltageControl) and
// adj_conv's now-delay-aware handling: the Alpaca Switch endpoint reaches adj_conv through two
// different commands depending on that setting (a specific "Value" voltage when enabled, vs a
// plain boolean on/off using the configured preset voltage when disabled - see
// HandleSwitchSetSwitchValue), and both must still honor a configured delay identically to a
// direct firmware "set" command (already covered by
// firmware_delayed_action_test.go/TestDelayedAction_ConfiguredDelay_AdjConv - that test, talking
// directly to the firmware over serial, is what actually verifies the delay is honored moment-to-
// moment; the /api/v1/power/status cache's own refresh lag here makes this proxy-level test unfit
// for that same immediate-timing check, so it instead confirms both proxy code paths correctly
// reach the firmware and the change is eventually and correctly reflected through Alpaca).
func TestProxyE2E_DelayedAction_AdjConvVoltageControl(t *testing.T) {
	proxy := startProxy(t, hwtestPort())
	const delayS = 3

	// adjID resolves adj_conv's current Alpaca switch Id fresh, on demand - found the hard way
	// (see findSwitchIDByName's doc comment): every /api/v1/config/set call in this test (dl
	// resets, delay config) triggers its own serial.SyncFirmwareConfig() resync in the
	// background, and consecutive resyncs do not reliably agree on the same mapping while a real
	// device is involved - resolving once and reusing a cached Id across several such calls (as an
	// earlier version of this test did) can silently end up targeting a completely different
	// switch by the time it's actually used. Calling this immediately before each id-dependent
	// request instead is the robust pattern: findSwitchIDByName's own stability wait means it's
	// always correct for that request's own moment, however the mapping got there.
	adjID := func() int { return findSwitchIDByName(t, proxy, "adj_conv") }

	// Force adj_conv (and every other standard switch, for a clean/known baseline) enabled - a
	// Disabled standard switch is skipped entirely from the dynamic switch ID map, which would
	// otherwise make adj_conv unreachable by any Alpaca Id at all.
	proxy.post(t, "/api/v1/config/set", `{"ps":{"d1":0,"d2":0,"d3":0,"d4":0,"d5":0,"u12":0,"u34":0,"adj":0}}`)

	t.Cleanup(func() {
		proxy.post(t, "/api/v1/config/set", `{"dl":{"adj":{"on":0,"off":0}}}`)
	})

	t.Run("VoltageControlEnabled_SpecificValueRespectsDelay", func(t *testing.T) {
		resp, body := setProxySetting(t, proxy, "enableAlpacaVoltageControl", true)
		require.Equal(t, http.StatusOK, resp.StatusCode, "raw body: %s", body)

		// Establish (and confirm) a clean off baseline first - immediate, no delay configured yet.
		proxy.post(t, "/api/v1/config/set", `{"dl":{"adj":{"on":0,"off":0}}}`)
		proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&State=false&ClientID=1&ClientTransactionID=1", adjID()), "")
		waitAdjStatus(t, proxy, adjIsOff, "a verified-off baseline before scheduling a delayed voltage set")

		proxy.post(t, "/api/v1/config/set", fmt.Sprintf(`{"dl":{"adj":{"on":%d,"off":0}}}`, delayS))
		resp2, respBody := proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&Value=6.5&ClientID=1&ClientTransactionID=1", adjID()), "")
		require.Equal(t, http.StatusOK, resp2.StatusCode, "raw body: %s", respBody)

		waitAdjStatus(t, proxy, adjIsNear(6.5), "the requested 6.5V once the delayed enable applied")

		_, gswBody := proxy.get(t, fmt.Sprintf("/api/v1/switch/0/getswitchvalue?Id=%d&ClientID=1&ClientTransactionID=1", adjID()))
		var gsw struct {
			Value float64 `json:"Value"`
		}
		require.NoError(t, json.Unmarshal(gswBody, &gsw))
		assert.InDelta(t, 6.5, gsw.Value, 0.5, "Alpaca getswitchvalue should reflect the applied voltage target")

		proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&State=false&ClientID=1&ClientTransactionID=1", adjID()), "")
		waitAdjStatus(t, proxy, adjIsOff, "off again after this subtest's own cleanup")
	})

	t.Run("VoltageControlDisabled_BooleanPathRespectsDelay", func(t *testing.T) {
		resp, body := setProxySetting(t, proxy, "enableAlpacaVoltageControl", false)
		require.Equal(t, http.StatusOK, resp.StatusCode, "raw body: %s", body)

		proxy.post(t, "/api/v1/config/set", `{"dl":{"adj":{"on":0,"off":0}}}`)
		proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&State=false&ClientID=1&ClientTransactionID=1", adjID()), "")
		waitAdjStatus(t, proxy, adjIsOff, "a verified-off baseline before scheduling a delayed boolean set")

		proxy.post(t, "/api/v1/config/set", fmt.Sprintf(`{"dl":{"adj":{"on":%d,"off":0}}}`, delayS))
		resp2, respBody := proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&State=true&ClientID=1&ClientTransactionID=1", adjID()), "")
		require.Equal(t, http.StatusOK, resp2.StatusCode, "raw body: %s", respBody)

		waitAdjStatus(t, proxy, adjIsOnAtAll, "adj turning on (at its configured preset voltage) once the delay elapsed")

		proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&State=false&ClientID=1&ClientTransactionID=1", adjID()), "")
	})
}
