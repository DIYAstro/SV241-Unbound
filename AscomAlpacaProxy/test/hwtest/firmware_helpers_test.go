//go:build hwtest

package hwtest

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// openConnForTest opens a fresh serial connection for a single test and registers cleanup to
// close it - each firmware-level test owns the port only for its own duration, so the port is
// always free again (for the next test, or for the Proxy E2E subprocess) once a test finishes.
func openConnForTest(t *testing.T) *SerialConn {
	t.Helper()
	conn, err := openSerialConn(hwtestPort())
	require.NoError(t, err, "failed to open serial connection")
	t.Cleanup(conn.Close)
	return conn
}

// getStatus fetches {"get":"status"} and returns the nested "status" object (switch/heater
// states), discarding the sibling "dm" (dew heater modes) array - see main.cpp's status_doc
// construction: get_power_status_json() populates doc["status"], "dm" is piggybacked alongside.
func getStatus(t *testing.T, conn *SerialConn) map[string]interface{} {
	t.Helper()
	resp := mustSendCommand(t, conn, `{"get":"status"}`, 3*time.Second)
	var full map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resp), &full), "status response was not valid JSON: %s", resp)
	status, ok := full["status"].(map[string]interface{})
	require.True(t, ok, "status response missing nested \"status\" object: %s", resp)
	return status
}

// getConfig fetches {"get":"config"} and returns it parsed as a generic map.
func getConfig(t *testing.T, conn *SerialConn) map[string]interface{} {
	t.Helper()
	resp := mustSendCommand(t, conn, `{"get":"config"}`, 3*time.Second)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resp), &m), "config response was not valid JSON: %s", resp)
	return m
}

// setSwitch sends {"set":{key:state}} for a single boolean-style output. On success the
// firmware echoes the same nested {"status":{...}} shape as {"get":"status"} (both go through
// get_power_status_json()) - unwrapped here so callers can index switch keys directly. On
// rejection (e.g. a Disabled output) the firmware instead prints a standalone, unwrapped
// {"error":"..."} line - returned as-is so callers can check resp["error"].
func setSwitch(t *testing.T, conn *SerialConn, key string, on bool) map[string]interface{} {
	t.Helper()
	cmd := fmt.Sprintf(`{"set":{%q:%v}}`, key, on)
	resp := mustSendCommand(t, conn, cmd, 3*time.Second)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resp), &m), "set response was not valid JSON: %s", resp)
	if status, ok := m["status"].(map[string]interface{}); ok {
		return status
	}
	return m
}

// setAllPower sends {"set":{"all":true/false}}.
func setAllPower(t *testing.T, conn *SerialConn, on bool) {
	t.Helper()
	cmd := fmt.Sprintf(`{"set":{"all":%v}}`, on)
	mustSendCommand(t, conn, cmd, 3*time.Second)
}

// setConfig sends an arbitrary partial config object via {"sc":...} and returns the full
// resulting config (the firmware echoes the complete config back after applying an "sc").
func setConfig(t *testing.T, conn *SerialConn, partial string) map[string]interface{} {
	t.Helper()
	cmd := fmt.Sprintf(`{"sc":%s}`, partial)
	resp := mustSendCommand(t, conn, cmd, 5*time.Second)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resp), &m), "sc response was not valid JSON: %s", resp)
	return m
}

// setPowerStartupStates sets the "ps" object (startup states for the standard switches).
// state: 0=Off, 1=On, 2=Disabled - see config_manager.h's PowerStartupStates.
func setPowerStartupStates(t *testing.T, conn *SerialConn, states map[string]int) {
	t.Helper()
	b, err := json.Marshal(states)
	require.NoError(t, err)
	setConfig(t, conn, fmt.Sprintf(`{"ps":%s}`, b))
}

// switchStatusInt reads a standard (non-adj_conv, non-pwm) switch's status value as an int.
func switchStatusInt(t *testing.T, status map[string]interface{}, key string) int {
	t.Helper()
	v, ok := status[key]
	require.True(t, ok, "status response missing key %q: %v", key, status)
	f, ok := v.(float64)
	require.True(t, ok, "status[%q] was not a number: %v (%T)", key, v, v)
	return int(f)
}
