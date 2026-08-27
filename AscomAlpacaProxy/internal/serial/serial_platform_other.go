//go:build !linux

package serial

import (
	"errors"
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

// openPort opens portName via go.bug.st/serial - unchanged from this project's long-standing
// Windows/macOS behavior. The serial driver on these platforms doesn't assert DTR/RTS by default
// on open the way Linux's tty layer does (see ch340_linux.go), but explicitly clearing them
// afterward keeps the ESP32 out of bootloader mode regardless.
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
