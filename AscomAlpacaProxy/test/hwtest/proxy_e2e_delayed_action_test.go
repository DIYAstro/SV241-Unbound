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
	const d4SwitchID = 6 // config.SwitchIDMap: sensors 0-2, dc1-dc5 = 3-7 - see that map's doc comment

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
