//go:build linux

package serial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sv241pro-alpaca-proxy/internal/logger"

	"github.com/google/gousb"
)

// Why this file exists.
//
// On Linux, opening the SV241 box the normal way (via /dev/ttyUSB0 and the kernel's ch341 tty
// driver) makes the kernel's generic tty layer raise DTR and RTS itself on every single open
// (tty_port_raise_dtr_rts) - before any userspace code runs, and this is not something any
// termios flag can suppress (confirmed against real hardware: CLOCAL doesn't help; there's also
// no ch341/usbserial kernel module parameter or sysfs knob for it, and the one upstream kernel
// proposal for exactly this problem, O_NRESETDEV, was never merged). While DTR/RTS are asserted,
// this hardware's ESP32 auto-reset circuit holds the chip in reset - it answers nothing at all
// until they're cleared, and clearing them is what makes it boot. So going through the kernel
// driver means every single open costs one device reset, no matter how the reconnect logic on
// our side is written - dropping the user's live output states back to their startup config
// every time the Proxy (re)connects.
//
// The escape hatch: talk to the CH340 chip's own vendor-specific USB protocol directly - the
// same one drivers/usb/serial/ch341.c in the kernel implements - bypassing its tty driver
// entirely. Verified against real hardware: the chip's own internal DTR/RTS state, once cleared
// via a single CH341_REQ_MODEM_CTRL control transfer, persists in the chip's own hardware
// registers - independent of which process holds the USB interface claimed, or how many times
// it's been released and reclaimed since. A brand new process can claim the interface and start
// talking immediately without ever sending that command again, as long as the chip has already
// been through it once since it was last actually powered on.
//
// So openPort() here tries gently first - reconfigure baud/LCR only, never touch DTR/RTS, verify
// with a real command - and only falls back to the reset-inducing MODEM_CTRL call if that didn't
// get a response, which in practice only happens on the very first connection after the box is
// physically plugged in (or after a real power cycle).
const (
	ch340VID gousb.ID = 0x1a86
	ch340PID gousb.ID = 0x7523

	ch341ReqReadVersion = 0x5F
	ch341ReqWriteReg    = 0x9A
	ch341ReqSerialInit  = 0xA1
	ch341ReqModemCtrl   = 0xA4

	ch341RegPrescaler = 0x12
	ch341RegDivisor   = 0x13
	ch341RegLCR       = 0x18
	ch341RegLCR2      = 0x25

	ch341LCR8N1 = 0xC3 // ENABLE_RX(0x80) | ENABLE_TX(0x40) | CS8(0x03)

	// Divisor value for 115200 baud. Computed by hand from the kernel driver's
	// ch341_get_divisor() formula (CH341_CLKRATE=48000000, CH341_CLK_DIV(ps,fact) =
	// 1<<(12-3*ps-fact)): for 115200, ps=3 fact=0 div=52, giving (0x100-div)<<8 | fact<<2 | ps.
	// Chip versions above 0x27 additionally need bit 7 set (buffering quirk, see ch341.c) - that
	// bit is added in configureBaudAndLCR() once the chip version is known. Verified against
	// real hardware.
	ch341Baud115200Divisor = (0x100-52)<<8 | 3

	ch341BitDTR = 1 << 5
	ch341BitRTS = 1 << 6

	// The CH340 in this device has exactly one bulk IN/OUT endpoint pair, both numbered 2
	// (addresses 0x82 and 0x02), plus an interrupt endpoint (0x81, unused here) for modem status
	// - confirmed via `lsusb -v` against the real hardware.
	ch340BulkEndpointNum = 2

	ch340ControlTimeout = 2 * time.Second
)

// ch340Port implements Port by talking to the CH340 chip directly over USB via libusb (through
// the gousb wrapper), instead of through the kernel's ch341 tty driver. See the package-level
// comment above for why.
type ch340Port struct {
	ctx  *gousb.Context
	dev  *gousb.Device
	done func()

	epIn  *gousb.InEndpoint
	epOut *gousb.OutEndpoint

	mu          sync.Mutex
	readTimeout time.Duration
}

// ch340PortLabel is used in place of a real /dev/ttyUSB* path for logging - there isn't one once
// the kernel driver is bypassed, and FindPort()/openPort() below always locate the device by
// VID/PID directly rather than by a device node name.
const ch340PortLabel = "ch340(usb)"

// FindPort looks for the SV241 device directly by USB VID/PID, bypassing the tty-node
// enumeration go.bug.st/serial/enumerator relies on entirely. That enumeration depends on the
// kernel's ch341 driver actually being bound (so /dev/ttyUSB* exists) - once this file has
// bypassed it even once, that device node is gone, so there's nothing for it to find.
func FindPort() (string, Port, error) {
	if p, success := probePortWithTimeout(ch340PortLabel, 4*time.Second); success {
		return ch340PortLabel, p, nil
	}
	return "", nil, errors.New("could not find SV241 device (CH340 1a86:7523) on the USB bus")
}

// openPort opens the SV241 box's CH340 chip directly by VID/PID, bypassing the kernel's serial
// driver. portName is accepted only for logging/interface-compatibility with the Windows
// implementation - the actual device is always found by VID/PID, since /dev/ttyUSB0 (and the
// kernel driver behind it) is exactly what this file exists to avoid.
func openPort(portName string) (Port, error) {
	ctx := gousb.NewContext()

	dev, err := ctx.OpenDeviceWithVIDPID(ch340VID, ch340PID)
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("open CH340 device: %w", err)
	}
	if dev == nil {
		ctx.Close()
		return nil, errors.New("no CH340 device found (is the SV241 box connected?)")
	}

	// Detach (and, on Close, reattach) the kernel's ch341 driver as needed so we can claim the
	// interface ourselves - this is the actual bypass.
	if err := dev.SetAutoDetach(true); err != nil {
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("set auto-detach: %w", err)
	}
	dev.ControlTimeout = ch340ControlTimeout

	intf, done, err := dev.DefaultInterface()
	if err != nil {
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("claim interface: %w", err)
	}

	epIn, err := intf.InEndpoint(ch340BulkEndpointNum)
	if err != nil {
		done()
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("open bulk IN endpoint: %w", err)
	}
	epOut, err := intf.OutEndpoint(ch340BulkEndpointNum)
	if err != nil {
		done()
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("open bulk OUT endpoint: %w", err)
	}

	p := &ch340Port{
		ctx:         ctx,
		dev:         dev,
		done:        done,
		epIn:        epIn,
		epOut:       epOut,
		readTimeout: 2 * time.Second,
	}

	version, err := p.readChipVersion()
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("read chip version: %w", err)
	}

	if err := p.configureBaudAndLCR(version); err != nil {
		p.Close()
		return nil, fmt.Errorf("configure baud/LCR: %w", err)
	}

	// Gentle path: never touch DTR/RTS, just check whether the device is already awake and
	// responsive - the common case for any reconnect after the very first one.
	if p.verifyResponsive(2 * time.Second) {
		logger.Info("CH340 (%s): gentle open succeeded, device already responsive - no reset needed.", portName)
		return p, nil
	}

	// Forceful fallback: release DTR/RTS. On real hardware this is the one call that actually
	// resets the ESP32 - unavoidable the first time, since we don't know the chip's state yet.
	logger.Info("CH340 (%s): gentle open got no response - releasing DTR/RTS and retrying.", portName)
	if err := p.setModemCtrl(false, false); err != nil {
		p.Close()
		return nil, fmt.Errorf("release DTR/RTS: %w", err)
	}

	// The device just went through the release-induced reset. Swallow the FreeRTOS boot log
	// before returning so the caller's first real command doesn't read garbage.
	time.Sleep(1500 * time.Millisecond)
	p.drain(100 * time.Millisecond)

	if !p.verifyResponsive(2 * time.Second) {
		p.Close()
		return nil, errors.New("device did not respond even after releasing DTR/RTS")
	}

	return p, nil
}

func (p *ch340Port) controlOut(request uint8, val, idx uint16) error {
	_, err := p.dev.Control(gousb.ControlOut|gousb.ControlVendor|gousb.ControlDevice, request, val, idx, nil)
	return err
}

func (p *ch340Port) controlIn(request uint8, val, idx uint16, length int) ([]byte, error) {
	buf := make([]byte, length)
	n, err := p.dev.Control(gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice, request, val, idx, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (p *ch340Port) readChipVersion() (byte, error) {
	buf, err := p.controlIn(ch341ReqReadVersion, 0, 0, 2)
	if err != nil {
		return 0, err
	}
	if len(buf) < 1 {
		return 0, errors.New("short response reading chip version")
	}
	return buf[0], nil
}

// configureBaudAndLCR mirrors ch341_configure()/ch341_set_baudrate_lcr() from the kernel driver:
// serial-init, then baud rate (prescaler+divisor in one register write), then line control
// (8N1) for chip versions that support the LCR register (>= 0x30 - matches this device, version
// 0x34, confirmed against real hardware).
func (p *ch340Port) configureBaudAndLCR(version byte) error {
	if err := p.controlOut(ch341ReqSerialInit, 0, 0); err != nil {
		return fmt.Errorf("serial init: %w", err)
	}

	val := uint16(ch341Baud115200Divisor)
	if version > 0x27 {
		val |= 0x80
	}
	if err := p.controlOut(ch341ReqWriteReg, ch341RegDivisor<<8|ch341RegPrescaler, val); err != nil {
		return fmt.Errorf("write baud rate: %w", err)
	}

	if version >= 0x30 {
		if err := p.controlOut(ch341ReqWriteReg, ch341RegLCR2<<8|ch341RegLCR, ch341LCR8N1); err != nil {
			return fmt.Errorf("write LCR: %w", err)
		}
	}
	return nil
}

// setModemCtrl sets DTR/RTS via CH341_REQ_MODEM_CTRL. This is the call that resets the ESP32 on
// this hardware - see the package doc comment. openPort() only ever calls this once per physical
// connection, on the forceful fallback path.
func (p *ch340Port) setModemCtrl(dtr, rts bool) error {
	var control uint16
	if dtr {
		control |= ch341BitDTR
	}
	if rts {
		control |= ch341BitRTS
	}
	return p.controlOut(ch341ReqModemCtrl, ^control&0xFF, 0)
}

// verifyResponsive sends a lightweight status query and checks for a valid JSON response within
// timeout - used both to decide whether the gentle open path worked, and (implicitly, via
// serial.go's own probePortWithTimeout) to confirm this is really a live SV241 device.
func (p *ch340Port) verifyResponsive(timeout time.Duration) bool {
	if err := p.SetReadTimeout(timeout); err != nil {
		return false
	}
	if _, err := p.Write([]byte("{\"get\":\"sensors\"}\n")); err != nil {
		return false
	}
	line, err := readLine(p, timeout)
	if err != nil {
		return false
	}
	trimmed := strings.TrimSpace(line)
	var js json.RawMessage
	return json.Unmarshal([]byte(trimmed), &js) == nil
}

func (p *ch340Port) drain(perReadTimeout time.Duration) {
	_ = p.SetReadTimeout(perReadTimeout)
	buf := make([]byte, 4096)
	for {
		n, _ := p.Read(buf)
		if n == 0 {
			break
		}
	}
	_ = p.SetReadTimeout(2 * time.Second)
}

// Read implements Port. Like go.bug.st/serial's Read() (which the rest of this package, e.g.
// readLine() and the various drain loops, is written against), a per-call timeout with no data
// available is reported as (0, nil), not an error - only a genuine transfer failure is returned
// as an error.
func (p *ch340Port) Read(buf []byte) (int, error) {
	p.mu.Lock()
	timeout := p.readTimeout
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	n, err := p.epIn.ReadContext(ctx, buf)
	if err != nil && ctx.Err() != nil {
		return n, nil
	}
	return n, err
}

// Write implements Port.
func (p *ch340Port) Write(buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return p.epOut.WriteContext(ctx, buf)
}

// SetReadTimeout implements Port.
func (p *ch340Port) SetReadTimeout(d time.Duration) error {
	p.mu.Lock()
	p.readTimeout = d
	p.mu.Unlock()
	return nil
}

// SetDTR and SetRTS are deliberate no-ops here. DTR/RTS are only ever touched once, internally,
// by openPort()'s forceful fallback (see the package doc comment) - if the generic reconnect
// logic in serial.go called through to a real DTR/RTS toggle here on every open (the way it does
// on Windows), we'd reintroduce the exact reset-on-every-reconnect problem this file exists to
// solve.
func (p *ch340Port) SetDTR(dtr bool) error { return nil }
func (p *ch340Port) SetRTS(rts bool) error { return nil }

// Close implements Port.
func (p *ch340Port) Close() error {
	if p.done != nil {
		p.done()
	}
	if p.dev != nil {
		p.dev.Close()
	}
	if p.ctx != nil {
		p.ctx.Close()
	}
	return nil
}
