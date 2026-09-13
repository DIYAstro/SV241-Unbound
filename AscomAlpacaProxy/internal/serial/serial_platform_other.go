//go:build !linux

package serial

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"sv241pro-alpaca-proxy/internal/logger"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// FindPort enumerates every USB serial port on the system and probes each one until it finds
// the SV241 device. Unchanged from this project's long-standing behavior.
func FindPort() (string, Port, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		logger.Warn("FindPort: enumerator.GetDetailedPortsList returned an error: %v.", err)
	}
	if len(ports) == 0 {
		return "", nil, errors.New("no serial ports found on the system")
	}

	logger.Info("Found %d serial ports. Probing for SV241 device...", len(ports))
	for _, port := range ports {
		logger.Debug("Checking port: %s (IsUSB: %t, VID: %s, PID: %s)", port.Name, port.IsUSB, port.VID, port.PID)
		if port.IsUSB {
			logger.Info("Probing port: %s", port.Name)

			if p, success := probePortWithTimeout(port.Name, 4*time.Second); success {
				return port.Name, p, nil
			}
		} else {
			logger.Debug("Skipping port %s: Not a USB port.", port.Name)
		}
	}
	return "", nil, errors.New("could not find SV241 device on any USB serial port")
}

// maxConsecutiveReadFailures is how many commands in a row may fail to get a response before we
// give up on the connection, close the port and let the watchdog reconnect.
//
// 1 keeps the long-standing Windows behavior exactly as it was: disconnect as soon as a command
// exhausts its retries. Windows does not have the Linux reset-on-open problem that makes
// reconnecting expensive there (see ch340_linux.go), and this path has been reliable in
// practice, so it is left alone on purpose.
const maxConsecutiveReadFailures = 1

// findPortForFlashing is the Windows/macOS implementation of the cross-platform hook serial.go's
// AcquirePortForFlashing calls (see ch340_linux.go for the Linux implementation). Unlike FindPort
// below, it never sends the ordinary JSON ping - a device whose application firmware is
// corrupted, erased, or never flashed can't answer that, but must still be locatable and
// flashable. Filters to the SV241's known CH340 VID/PID (1A86:7523) via active USB probing so an
// unrelated USB-serial adapter plugged into the same machine never shows up as a candidate.
//
// preferredName pins to a specific COM/tty name if it's still present among the enumerated
// ports. If more than one candidate is present and none is pinned (or the pin no longer matches
// anything connected), returns an *AmbiguousPortError listing every candidate found - resolving
// that ambiguity by guessing risks flashing the wrong physical device. On a single match (pinned
// or the only candidate present), returns an already-open, 115200-baud Port ready for
// espflasher's own ROM-bootloader sync to verify.
func findPortForFlashing(preferredName string) (Port, string, error) {
	ports, err := enumerator.GetDetailedPortsList(isCH340VIDPID)
	if err != nil {
		return nil, "", fmt.Errorf("enumerate serial ports: %w", err)
	}

	var candidates []string
	for _, p := range ports {
		if p.IsUSB && isCH340VIDPID(p.VID, p.PID) {
			candidates = append(candidates, p.Name)
		}
	}
	if len(candidates) == 0 {
		return nil, "", errors.New("no CH340 device found (is the SV241 box connected?)")
	}

	chosen := candidates[0]
	pinned := false
	for _, c := range candidates {
		if c == preferredName {
			chosen = c
			pinned = true
			break
		}
	}
	if len(candidates) > 1 && !pinned {
		return nil, "", &AmbiguousPortError{Candidates: candidates}
	}

	// InitialStatusBits matters here: go.bug.st/serial defaults DTR/RTS to *true* on open when
	// left nil (see Mode's doc comment) - true holds this hardware's ESP32 in reset (see
	// openPort's own doc comment below, and ch340_linux.go's for why). Confirmed against real
	// hardware: leaving this nil here made every espflasher connect attempt fail to sync, since
	// the chip sat held in reset before espflasher's own reset sequence ever got a chance to run
	// its intended transitions from a known-clear starting state.
	mode := &serial.Mode{
		BaudRate:          115200,
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false},
	}
	p, err := serial.Open(chosen, mode)
	if err != nil {
		return nil, "", fmt.Errorf("open port %s: %w", chosen, err)
	}
	// Belt-and-braces, matching openPort() below: some drivers still pulse the lines briefly on
	// open regardless of InitialStatusBits, so clear them explicitly too.
	if err := p.SetDTR(false); err != nil {
		logger.Warn("findPortForFlashing: could not disable DTR on port %s: %v", chosen, err)
	}
	if err := p.SetRTS(false); err != nil {
		logger.Warn("findPortForFlashing: could not disable RTS on port %s: %v", chosen, err)
	}
	return p, chosen, nil
}

// isCH340VIDPID reports whether vid/pid (as reported by go.bug.st/serial/enumerator, hex strings
// with no fixed case or "0x" prefix guaranteed) identify the SV241's CH340 chip (1A86:7523) - the
// same VID/PID ch340_linux.go's ch340Candidates matches on Linux.
func isCH340VIDPID(vid, pid string) bool {
	norm := func(s string) string {
		return strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	}
	return norm(vid) == "1A86" && norm(pid) == "7523"
}

// openPort opens portName via go.bug.st/serial - unchanged from this project's long-standing
// Windows/macOS behavior. The serial driver on these platforms doesn't assert DTR/RTS by default
// on open the way Linux's tty layer does (see ch340_linux.go), but explicitly clearing them
// afterward keeps the ESP32 out of bootloader mode regardless. The returned go.bug.st/serial.Port
// value already satisfies FlashablePort (port.go) directly - no adapter needed for a native flash
// session to use it as espflasher's transport, including its SetMode-driven baud switch (unused
// today - see internal/flasher's doc comment for why FlashBaudRate is kept equal to BaudRate on
// this hardware - but functional here regardless, unlike ch340Port's baud-locked implementation).
func openPort(portName string) (Port, error) {
	mode := &serial.Mode{
		BaudRate:          115200,
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false},
	}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, err
	}

	if err := p.SetDTR(false); err != nil {
		logger.Warn("Could not disable DTR on port %s: %v", portName, err)
	}
	if err := p.SetRTS(false); err != nil {
		logger.Warn("Could not disable RTS on port %s: %v", portName, err)
	}

	// Swallow any boot-log bytes before returning, exactly as before this refactor.
	time.Sleep(1500 * time.Millisecond)
	p.SetReadTimeout(100 * time.Millisecond)
	drainBuf := make([]byte, 4096)
	for {
		n, _ := p.Read(drainBuf)
		if n == 0 {
			break
		}
	}
	p.SetReadTimeout(2 * time.Second)

	return p, nil
}
