package serial

import "time"

// Port is the minimal serial-port interface this package actually needs. On Windows/macOS this
// is satisfied directly by go.bug.st/serial's own Port (its concrete Open() return value already
// has all these methods, so no adapter is needed - see serial_platform_other.go). On Linux it's
// satisfied by our own CH340 driver, which bypasses the kernel's ch341 tty driver entirely -
// see ch340_linux.go for why.
type Port interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	SetReadTimeout(d time.Duration) error
	SetDTR(dtr bool) error
	SetRTS(rts bool) error
}

// openPort opens portName and returns a Port that's already ready to have commands sent on it -
// any platform-specific reset/settle handling needed to reach that state has already happened
// internally. Implemented separately per platform:
//   - serial_platform_other.go (Windows/macOS): go.bug.st/serial, matching this project's
//     long-standing behavior there unchanged.
//   - ch340_linux.go (Linux): a from-scratch userspace CH340 driver that talks to the chip
//     directly over USB, bypassing the kernel's ch341 tty driver - see that file's doc comment
//     for the hardware investigation behind why.
