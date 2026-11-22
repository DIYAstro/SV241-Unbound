//go:build hwtest

package hwtest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probeInfo struct {
	ID string   `json:"id"`
	T  *float64 `json:"t"`
	H  int      `json:"h"`
}

// getProbes fetches {"get":"probes"} and returns the found probes.
func getProbes(t *testing.T, conn *SerialConn) []probeInfo {
	t.Helper()
	resp := mustSendCommand(t, conn, `{"get":"probes"}`, 3*time.Second)
	var out struct {
		Probes []probeInfo `json:"probes"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp), &out), "probes response was not valid JSON: %s", resp)
	return out.Probes
}

// requireProbe skips the test unless at least one probe is on the bus, and returns its ID.
func requireProbe(t *testing.T, conn *SerialConn) string {
	t.Helper()
	probes := getProbes(t, conn)
	if len(probes) == 0 {
		t.Skip("no DS18B20 lens probe found on the bus")
	}
	return probes[0].ID
}

// dallasCRC8 is the Dallas/Maxim 1-Wire CRC8 (poly 0x31 reflected), used to build a ROM id that is
// valid but not present on the bus.
func dallasCRC8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8C
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// fakeValidID returns a CRC-valid ROM id (28-family) that is unlikely to exist on the bus.
func fakeValidID() string {
	rom := []byte{0x28, 0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02}
	return fmt.Sprintf("%X%02X", rom, dallasCRC8(rom))
}

func lpOf(cfg map[string]interface{}) []interface{} {
	lp, _ := cfg["lp"].([]interface{})
	return lp
}

func TestProbes_GetProbesReturnsValidID(t *testing.T) {
	conn := openConnForTest(t)
	id := requireProbe(t, conn)
	assert.Len(t, id, 16)
	assert.Equal(t, strings.ToUpper(id), id)
}

func TestProbes_DuplicateIDRejected(t *testing.T) {
	conn := openConnForTest(t)
	id := requireProbe(t, conn)
	snapshot, err := getConfigSnapshot(conn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restoreConfigSnapshot(conn, snapshot) })

	before := getConfig(t, conn)
	resp := mustSendCommand(t, conn, fmt.Sprintf(`{"sc":{"lp":[{"id":%q,"o":0,"h":0},{"id":%q,"o":0,"h":1}]}}`, id, id), 5*time.Second)
	assert.Contains(t, resp, `"error"`)
	assert.Equal(t, before["lp"], getConfig(t, conn)["lp"], "config must stay unchanged after a rejected table")
}

func TestProbes_SameHeaterTwiceRejected(t *testing.T) {
	conn := openConnForTest(t)
	id := requireProbe(t, conn)
	snapshot, err := getConfigSnapshot(conn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restoreConfigSnapshot(conn, snapshot) })

	before := getConfig(t, conn)
	resp := mustSendCommand(t, conn, fmt.Sprintf(`{"sc":{"lp":[{"id":%q,"o":0,"h":0},{"id":%q,"o":0,"h":0}]}}`, id, fakeValidID()), 5*time.Second)
	assert.Contains(t, resp, `"error"`)
	assert.Equal(t, before["lp"], getConfig(t, conn)["lp"])
}

func TestProbes_InvalidCRCRejected(t *testing.T) {
	conn := openConnForTest(t)
	snapshot, err := getConfigSnapshot(conn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restoreConfigSnapshot(conn, snapshot) })

	good := fakeValidID()
	bad := good[:14] + "00"
	if bad == good {
		bad = good[:14] + "01"
	}
	before := getConfig(t, conn)
	resp := mustSendCommand(t, conn, fmt.Sprintf(`{"sc":{"lp":[{"id":%q,"o":0,"h":0}]}}`, bad), 5*time.Second)
	assert.Contains(t, resp, `"error"`)
	assert.Equal(t, before["lp"], getConfig(t, conn)["lp"])
}

func TestProbes_AssignmentControlsLensTemp(t *testing.T) {
	conn := openConnForTest(t)
	id := requireProbe(t, conn)
	snapshot, err := getConfigSnapshot(conn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restoreConfigSnapshot(conn, snapshot) })

	// Unassigned: t_lens must be null.
	setConfig(t, conn, `{"lp":[]}`)
	time.Sleep(3 * time.Second)
	sensors, err := getSensors(conn)
	require.NoError(t, err)
	assert.Nil(t, sensors["t_lens"], "without an assignment t_lens must be null")

	// Assigned to heater 1: t_lens must be a number.
	cfg := setConfig(t, conn, fmt.Sprintf(`{"lp":[{"id":%q,"o":0,"h":0}]}`, id))
	require.Len(t, lpOf(cfg), 1, "assignment should be stored: %v", cfg)
	time.Sleep(3 * time.Second)
	sensors, err = getSensors(conn)
	require.NoError(t, err)
	assert.NotNil(t, sensors["t_lens"], "with an assignment t_lens must have a value")
}

func TestProbes_FactoryResetClearsAssignment(t *testing.T) {
	conn := openConnForTest(t)
	id := requireProbe(t, conn)
	snapshot, err := getConfigSnapshot(conn)
	require.NoError(t, err)

	setConfig(t, conn, fmt.Sprintf(`{"lp":[{"id":%q,"o":0,"h":0}]}`, id))
	_, err = conn.sendCommand(`{"command":"factory_reset"}`, 3*time.Second)
	require.NoError(t, err)
	conn.Close()
	time.Sleep(4 * time.Second) // device restarts

	conn2 := openConnForTest(t)
	assert.Empty(t, lpOf(getConfig(t, conn2)), "factory_reset must clear the lens probe table")
	t.Cleanup(func() { _ = restoreConfigSnapshot(conn2, snapshot) })
}
