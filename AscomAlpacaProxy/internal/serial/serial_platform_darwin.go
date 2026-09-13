//go:build !linux && !windows

package serial

// PrepareFlashConnection is the macOS (and any other non-Linux, non-Windows Unix) implementation
// of the cross-platform hook internal/flasher calls right before connecting via espflasher - see
// serial_platform_windows.go for the contract and the confirmed-on-hardware Windows-specific
// problem this exists to work around there. No equivalent problem is known on macOS - the
// original community hardware report this whole native-flasher feature grew out of specifically
// noted this hardware is "rock-stable on macOS" for DTR/RTS handling in general, and unlike
// Windows's WCH VCP driver, go.bug.st/serial's SetDTR/SetRTS on macOS go through a standard
// termios ioctl path with no equivalent documented "reports success but drops the USB control
// message" issue. So this is a no-op, same as ch340_linux.go's: espflasher's own ResetDefault
// sequence, driven through go.bug.st/serial's Port interface exactly as openPort() below already
// returns it, is expected to work correctly here. Revisit if real macOS hardware testing ever
// shows otherwise.
func PrepareFlashConnection(port Port, portName string) (Port, bool, error) {
	return port, false, nil
}

// HardResetToApp is the macOS (and any other non-Linux, non-Windows Unix) implementation of the
// cross-platform hook internal/flasher calls right after a successful flash - see
// serial_platform_windows.go for why Windows needs a real implementation here. Returns port
// unchanged for the same reason PrepareFlashConnection above does: no equivalent DTR/RTS problem
// is known on macOS, so espflasher's own Flasher.Reset() already did the real work.
func HardResetToApp(port Port, portName string) (Port, error) {
	return port, nil
}
