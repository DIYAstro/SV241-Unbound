package serial

import (
	"time"

	bugstserial "go.bug.st/serial"
)

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

// FlashablePort is the fuller interface a native firmware-flash session (internal/flasher) needs
// from a transport - go.bug.st/serial.Port itself (SetMode, Drain, ResetInputBuffer/OutputBuffer,
// GetModemStatusBits, Break, in addition to everything Port above already requires). On
// Windows/macOS this is satisfied automatically: the concrete value behind Port there already IS a
// real go.bug.st/serial.Port (see serial_platform_other.go), so a type assertion recovers it with
// no adapter. On Linux, ch340Port implements it directly (see ch340_linux.go) - Drain/
// ResetInputBuffer/ResetOutputBuffer/GetModemStatusBits/Break are honest no-ops/stubs there (raw
// libusb bulk transfers have no OS buffering layer beyond Read/Write themselves, and this vendor
// protocol exposes no readable modem-status lines), and SetMode/SetDTR/SetRTS only do anything
// real while a flash session has called EnableRawControl (see that method's doc comment).
type FlashablePort = bugstserial.Port

// resolvableName is optionally implemented by a Port to report the specific identity it actually
// ended up bound to, when that can differ from the name it was opened/asked for - ch340Port
// (ch340_linux.go) is the only implementer today: several distinct physical USB devices can share
// the CH340 VID/PID it searches by, so the name passed to openPort() is only a best-effort pin
// (or no pin at all), while ResolvedName() reports which specific device was actually found.
// reconnect() (serial.go) checks for this via a type assertion rather than adding a method to
// Port itself, so go.bug.st/serial's own Port value (used directly on Windows/macOS - see the
// Port doc comment above) needs no adapter and stays completely unaffected; there, the name it
// was opened with and its "resolved" identity are always simply the same.
type resolvableName interface {
	ResolvedName() string
}

// openPort opens portName and returns a Port that's already ready to have commands sent on it -
// any platform-specific reset/settle handling needed to reach that state has already happened
// internally. Implemented separately per platform:
//   - serial_platform_other.go (Windows/macOS): go.bug.st/serial, matching this project's
//     long-standing behavior there unchanged.
//   - ch340_linux.go (Linux): a from-scratch userspace CH340 driver that talks to the chip
//     directly over USB, bypassing the kernel's ch341 tty driver - see that file's doc comment
//     for the hardware investigation behind why.
