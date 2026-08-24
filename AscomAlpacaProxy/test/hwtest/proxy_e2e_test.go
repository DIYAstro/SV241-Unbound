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

// assertPowerStatusEventually polls /api/v1/power/status until key reaches want or a generous
// timeout (comfortably longer than the ~5s periodic cache-update interval) elapses.
func assertPowerStatusEventually(t *testing.T, proxy *proxyProcess, key string, want float64, context string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last interface{}
	for time.Now().Before(deadline) {
		_, body := proxy.get(t, "/api/v1/power/status")
		var status map[string]interface{}
		if json.Unmarshal(body, &status) == nil {
			last = status[key]
			if f, ok := last.(float64); ok && f == want {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("power status cache never reflected %s (key %q, want %v, last seen %v)", context, key, want, last)
}

// TestProxyE2E drives the real Proxy binary purely over HTTP (REST + Alpaca), the same paths the
// Vue frontend and real ASCOM clients use - catching Proxy-layer bugs (caching, Alpaca mapping)
// that a firmware-only test can't see. All subtests share one running proxy instance/subprocess.
func TestProxyE2E(t *testing.T) {
	proxy := startProxy(t, hwtestPort())

	t.Run("GetConfig_MatchesFirmwareShape", func(t *testing.T) {
		resp, body := proxy.get(t, "/api/v1/config")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var cfg map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &cfg))
		assert.Contains(t, cfg, "psd")
		assert.Contains(t, cfg, "ps")
		assert.Contains(t, cfg, "dh")
	})

	t.Run("SetConfig_RoundTrips", func(t *testing.T) {
		resp, body := proxy.post(t, "/api/v1/config/set", `{"psd":444}`)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var setResp map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &setResp))
		assert.Equal(t, float64(444), setResp["psd"], "config/set should echo back the applied value")

		_, body2 := proxy.get(t, "/api/v1/config")
		var getResp map[string]interface{}
		require.NoError(t, json.Unmarshal(body2, &getResp))
		assert.Equal(t, float64(444), getResp["psd"], "a fresh GET after SET should reflect the new value, not a stale cache")

		proxy.post(t, "/api/v1/config/set", `{"psd":500}`) // restore default
	})

	// Regression test for commit c5d8718: Master Power toggle used to remain reported as ON
	// after switching all outputs OFF, due to a mismatched key lookup ('dc1' vs the firmware's
	// actual 'd1').
	t.Run("PowerAll_UpdatesStatusCacheCorrectly", func(t *testing.T) {
		// handleSetAllPower (server.go) overwrites the status cache with the response snapshot
		// taken at the instant of the "set" command - before the firmware's staggered queue has
		// actually turned anything on. The cache only catches up on the next periodic poll
		// (~5s, see internal/serial's periodicCacheUpdater) - so this must poll, not sample once.
		resp, _ := proxy.post(t, "/api/v1/power/all", `{"state":true}`)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assertPowerStatusEventually(t, proxy, "d1", float64(1), "all:true")

		resp, _ = proxy.post(t, "/api/v1/power/all", `{"state":false}`)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assertPowerStatusEventually(t, proxy, "d1", float64(0), "all:false")
	})

	// maxswitch is dynamic (depends on proxy settings like enableMasterPower and which switches
	// are currently "active", e.g. a Disabled dew heater may not appear) - not a fixed count.
	// See internal/config's SwitchIDMap (up to 14 entries: 3 sensors + 10 power outputs + master
	// power) and GetSwitchMapLength(). Only assert a sane floor: the 3 read-only sensors plus
	// the 7 standard DC/USB/ADJ switches are always present regardless of config.
	var maxSwitch int
	t.Run("AlpacaSwitch_MaxSwitch", func(t *testing.T) {
		_, body := proxy.get(t, "/api/v1/switch/0/maxswitch?ClientID=1&ClientTransactionID=1")
		var r struct {
			Value int `json:"Value"`
		}
		require.NoError(t, json.Unmarshal(body, &r))
		maxSwitch = r.Value
		assert.GreaterOrEqual(t, maxSwitch, 10, "maxswitch should be at least 10 (3 sensors + 7 standard switches), got %d", maxSwitch)
	})

	t.Run("AlpacaSwitch_GetSwitchValue_AllIDs", func(t *testing.T) {
		require.Greater(t, maxSwitch, 0, "AlpacaSwitch_MaxSwitch must run first and succeed")
		for id := 0; id < maxSwitch; id++ {
			id := id
			t.Run(fmt.Sprintf("id_%d", id), func(t *testing.T) {
				resp, body := proxy.get(t, fmt.Sprintf("/api/v1/switch/0/getswitchvalue?Id=%d&ClientID=1&ClientTransactionID=1", id))
				require.Equal(t, http.StatusOK, resp.StatusCode)
				var r struct {
					Value       interface{} `json:"Value"`
					ErrorNumber int         `json:"ErrorNumber"`
				}
				require.NoError(t, json.Unmarshal(body, &r), "raw body: %s", body)
				assert.Equal(t, 0, r.ErrorNumber, "switch id %d should not report an Alpaca error", id)
			})
		}
	})

	// Regression test for commit 48909a7: ObservingConditions DewPoint used to map from the
	// wrong hardware key ('dp_amb' instead of the firmware's actual 'd').
	t.Run("AlpacaObsCond_DewPointMatchesFirmware", func(t *testing.T) {
		_, fwBody := proxy.get(t, "/api/v1/status")
		var fwConditions map[string]interface{}
		require.NoError(t, json.Unmarshal(fwBody, &fwConditions))

		_, body := proxy.get(t, "/api/v1/observingconditions/0/dewpoint?ClientID=1&ClientTransactionID=1")
		var r struct {
			Value float64 `json:"Value"`
		}
		require.NoError(t, json.Unmarshal(body, &r))

		if d, ok := fwConditions["d"].(float64); ok {
			assert.InDelta(t, d, r.Value, 0.2, "Alpaca dewpoint should match the firmware's raw 'd' field")
		} else {
			t.Skip("firmware conditions cache did not yet contain 'd' - cache may not have populated yet")
		}
	})

	// Alpaca write operations - GetSwitchValue_AllIDs above only ever reads. d1 is switch ID 3
	// (see config.SwitchIDMap: IDs 0-2 are the read-only sensors, power switches start at 3).
	const d1SwitchID = 3

	t.Run("AlpacaSwitch_SetSwitchValue", func(t *testing.T) {
		resp, body := proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&Value=1&ClientID=1&ClientTransactionID=1", d1SwitchID), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var r struct {
			ErrorNumber int `json:"ErrorNumber"`
		}
		require.NoError(t, json.Unmarshal(body, &r), "raw body: %s", body)
		assert.Equal(t, 0, r.ErrorNumber, "setswitchvalue(d1, 1) should succeed: %s", body)
		assertPowerStatusEventually(t, proxy, "d1", float64(1), "Alpaca setswitchvalue(d1, on)")

		proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchvalue?Id=%d&Value=0&ClientID=1&ClientTransactionID=1", d1SwitchID), "")
		assertPowerStatusEventually(t, proxy, "d1", float64(0), "Alpaca setswitchvalue(d1, off)")
	})

	t.Run("AlpacaSwitch_SetSwitchName", func(t *testing.T) {
		const newName = "Test Name via Alpaca"
		resp, _ := proxy.post(t, fmt.Sprintf("/api/v1/switch/0/setswitchname?Id=%d&Name=%s&ClientID=1&ClientTransactionID=1", d1SwitchID, url.QueryEscape(newName)), "")
		require.Equal(t, http.StatusOK, resp.StatusCode)

		_, body := proxy.get(t, fmt.Sprintf("/api/v1/switch/0/getswitchname?Id=%d&ClientID=1&ClientTransactionID=1", d1SwitchID))
		var r struct {
			Value string `json:"Value"`
		}
		require.NoError(t, json.Unmarshal(body, &r))
		assert.Equal(t, newName, r.Value, "custom switch name should round-trip via Alpaca setswitchname/getswitchname")
	})

	t.Run("Settings_GetPost", func(t *testing.T) {
		_, body := proxy.get(t, "/api/v1/settings")
		var settings struct {
			ProxyConfig map[string]interface{} `json:"proxy_config"`
		}
		require.NoError(t, json.Unmarshal(body, &settings))
		require.NotNil(t, settings.ProxyConfig)

		settings.ProxyConfig["logLevel"] = "DEBUG"
		payload, err := json.Marshal(settings.ProxyConfig)
		require.NoError(t, err)

		resp, _ := proxy.post(t, "/api/v1/settings", string(payload))
		require.Equal(t, http.StatusOK, resp.StatusCode)

		_, body2 := proxy.get(t, "/api/v1/settings")
		var settings2 struct {
			ProxyConfig map[string]interface{} `json:"proxy_config"`
		}
		require.NoError(t, json.Unmarshal(body2, &settings2))
		assert.Equal(t, "DEBUG", settings2.ProxyConfig["logLevel"], "logLevel should persist through a full POST /api/v1/settings")
	})

	t.Run("BackupRestore", func(t *testing.T) {
		_, cfgBeforeBody := proxy.get(t, "/api/v1/config")
		var cfgBefore map[string]interface{}
		require.NoError(t, json.Unmarshal(cfgBeforeBody, &cfgBefore))
		origPsd := cfgBefore["psd"]

		_, backupBody := proxy.get(t, "/api/v1/backup/create")
		require.NotEmpty(t, backupBody)

		proxy.post(t, "/api/v1/config/set", `{"psd":888}`)
		_, dirtyBody := proxy.get(t, "/api/v1/config")
		var dirtyCfg map[string]interface{}
		require.NoError(t, json.Unmarshal(dirtyBody, &dirtyCfg))
		require.Equal(t, float64(888), dirtyCfg["psd"], "precondition: psd should be dirtied before restore")

		resp, _ := proxy.post(t, "/api/v1/backup/restore", string(backupBody))
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// handleRestoreBackup disconnects, sleeps, then re-detects/reconnects the serial port -
		// poll instead of a single fixed-delay sample.
		deadline := time.Now().Add(15 * time.Second)
		restored := false
		var lastPsd interface{}
		for time.Now().Before(deadline) {
			resp, cfgAfterBody := proxy.get(t, "/api/v1/config")
			if resp.StatusCode == http.StatusOK {
				var cfgAfter map[string]interface{}
				if json.Unmarshal(cfgAfterBody, &cfgAfter) == nil {
					lastPsd = cfgAfter["psd"]
					if lastPsd == origPsd {
						restored = true
						break
					}
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		assert.True(t, restored, "psd should be restored from the backup (want %v), last seen %v", origPsd, lastPsd)
	})
}
