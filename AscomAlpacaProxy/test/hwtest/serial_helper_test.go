//go:build hwtest

package hwtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.bug.st/serial"
)

// hwtestPort returns the COM port to test against, overridable via the HWTEST_PORT env var so
// this suite isn't hardcoded to whichever port the box happens to be on right now.
func hwtestPort() string {
	if p := os.Getenv("HWTEST_PORT"); p != "" {
		return p
	}
	return "COM5"
}

// SerialConn is a minimal, self-contained wrapper around the firmware's line-based JSON serial
// protocol (see the project README's "Serial Protocol" section). Deliberately independent of
// AscomAlpacaProxy/internal/serial, which is built for the long-running proxy app (reconnect
// logic, background cache-updater goroutines, connection singleton) - unnecessary statefulness
// for a short-lived test connection. Read handling mirrors internal/serial's proven readLine()
// (serial.go:262-286): chunked 256-byte reads bounded by an overall deadline, not byte-by-byte.
type SerialConn struct {
	port serial.Port
}

// openSerialConn opens the port with DTR/RTS left low on connect, matching internal/serial's
// probePortWithTimeout()/reconnect() (serial.go:388-392, 512-516) - the ESP32's auto-reset
// circuit ties DTR/RTS to EN/GPIO0, so toggling them on open would reset the board on every
// single connection instead of just once at the start of the whole test run.
func openSerialConn(portName string) (*SerialConn, error) {
	mode := &serial.Mode{
		BaudRate:          115200,
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false},
	}
	port, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s (is the box connected, and not held open by a running Proxy instance or another test?): %w", portName, err)
	}
	// Drain whatever might already be sitting in the input buffer (e.g. a boot banner from a
	// prior reset) so the first real command doesn't accidentally pair with a stale response.
	port.SetReadTimeout(50 * time.Millisecond)
	drainBuf := make([]byte, 256)
	for {
		n, err := port.Read(drainBuf)
		if err != nil || n == 0 {
			break
		}
	}
	return &SerialConn{port: port}, nil
}

func (s *SerialConn) Close() {
	if s.port != nil {
		s.port.Close()
	}
}

// readLine reads until a newline or the overall timeout elapses.
func (s *SerialConn) readLine(timeout time.Duration) (string, error) {
	s.port.SetReadTimeout(timeout)
	var result []byte
	buf := make([]byte, 256)
	start := time.Now()

	for {
		if time.Since(start) > timeout {
			return "", errors.New("read timeout")
		}
		n, err := s.port.Read(buf)
		if err != nil {
			return "", err
		}
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					return string(result), nil
				}
				result = append(result, buf[i])
			}
		}
	}
}

// drainStale discards any bytes already sitting in the input buffer before a new command is
// sent. Needed because some commands produce more than one response line - e.g. a "set" that
// tries to enable a Disabled output first prints a standalone {"error":...} line (see
// power_control.cpp's set_power_output()), then main.cpp still emits the usual status echo
// afterwards regardless. Without this, that second line would be misread as the response to the
// *next* command this test sends.
func (s *SerialConn) drainStale() {
	s.port.SetReadTimeout(30 * time.Millisecond)
	buf := make([]byte, 256)
	for {
		n, err := s.port.Read(buf)
		if err != nil || n == 0 {
			return
		}
	}
}

// sendCommand writes a single JSON command line and reads back one JSON response line, skipping
// any stray non-JSON lines (e.g. leftover boot output) until a valid one arrives or timeout.
func (s *SerialConn) sendCommand(cmd string, timeout time.Duration) (string, error) {
	s.drainStale()
	if _, err := s.port.Write([]byte(cmd + "\n")); err != nil {
		return "", fmt.Errorf("failed to write command %q: %w", cmd, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", fmt.Errorf("timed out waiting for response to %q", cmd)
		}
		line, err := s.readLine(remaining)
		if err != nil {
			return "", fmt.Errorf("no response to %q within %s: %w", cmd, timeout, err)
		}
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		return line, nil
	}
}

// mustSendCommand is the test-facing wrapper: fails the test immediately via t.Fatalf on error.
func mustSendCommand(t *testing.T, s *SerialConn, cmd string, timeout time.Duration) string {
	t.Helper()
	resp, err := s.sendCommand(cmd, timeout)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return resp
}

// getConfigSnapshot fetches the full current firmware config as a raw JSON object string, to be
// restored later via restoreConfigSnapshot. Kept as a raw string (not unmarshaled into a Go
// struct) so restore is exact and immune to this test suite's own field knowledge going stale -
// same principle as the firmware's own JSON config store (see config_manager.cpp).
func getConfigSnapshot(s *SerialConn) (string, error) {
	return s.sendCommand(`{"get":"config"}`, 3*time.Second)
}

// restoreConfigSnapshot sends a previously captured config snapshot back via "sc", leaving the
// device exactly as found regardless of what individual tests changed in between.
func restoreConfigSnapshot(s *SerialConn, snapshot string) error {
	cmd := fmt.Sprintf(`{"sc":%s}`, snapshot)
	_, err := s.sendCommand(cmd, 5*time.Second)
	return err
}

// getSensors fetches {"get":"sensors"} and returns it parsed as a generic map, for tests that
// need heap metrics (hf/hmf/hma/hs) or environmental readings (t_amb, h_amb, d, t_lens, v, i).
func getSensors(s *SerialConn) (map[string]interface{}, error) {
	resp, err := s.sendCommand(`{"get":"sensors"}`, 3*time.Second)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		return nil, fmt.Errorf("sensors response was not a JSON object: %w (raw: %s)", err, resp)
	}
	return m, nil
}
