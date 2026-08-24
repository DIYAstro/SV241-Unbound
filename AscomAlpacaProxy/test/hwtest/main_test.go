//go:build hwtest

// Package hwtest is a hardware-in-the-loop test suite for the SV241 firmware and Proxy. It
// requires a real box connected over serial (default COM5, override via HWTEST_PORT) and is
// gated behind the "hwtest" build tag so normal `go build`/`go test ./...` never touches it.
//
// Run: go test -tags hwtest ./test/hwtest/... -v
// See README.md in this directory for details, safety notes, and the burn-in test.
package hwtest

import (
	"fmt"
	"log"
	"os"
	"testing"
)

// configSnapshot holds the full firmware config captured before any test runs, so TestMain can
// restore it exactly afterwards - see restoreConfigSnapshot's doc comment for why this is more
// robust than tracking/undoing each test's individual changes.
var configSnapshot string

func TestMain(m *testing.M) {
	port := hwtestPort()

	conn, err := openSerialConn(port)
	if err != nil {
		log.Fatalf("hwtest: could not connect to the box before running tests: %v", err)
	}

	snapshot, err := getConfigSnapshot(conn)
	if err != nil {
		conn.Close()
		log.Fatalf("hwtest: could not capture baseline config snapshot: %v", err)
	}
	configSnapshot = snapshot
	fmt.Printf("hwtest: captured baseline config snapshot (%d bytes) from %s\n", len(snapshot), port)
	conn.Close() // Release the port; individual tests (and the Proxy subprocess) open it themselves.

	code := m.Run()

	restoreConn, err := openSerialConn(port)
	if err != nil {
		log.Printf("hwtest: WARNING - could not reconnect to restore the baseline config: %v", err)
		log.Printf("hwtest: the device may be left in a test-modified state. Baseline snapshot was:\n%s", configSnapshot)
		os.Exit(code)
	}
	if err := restoreConfigSnapshot(restoreConn, configSnapshot); err != nil {
		log.Printf("hwtest: WARNING - failed to restore baseline config: %v", err)
		log.Printf("hwtest: baseline snapshot was:\n%s", configSnapshot)
	} else {
		fmt.Println("hwtest: baseline config snapshot restored successfully.")
	}
	restoreConn.Close()

	os.Exit(code)
}
