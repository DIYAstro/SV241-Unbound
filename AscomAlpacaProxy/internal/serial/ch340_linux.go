//go:build linux

package serial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

	// resolvedID is the ch340PathID() of the specific physical device this port ended up talking
	// to - set once in openPort(), read back by FindPort() so it can return it in place of the
	// old, non-disambiguating ch340PortLabel constant. See ch340PathID for why this matters.
	resolvedID string
}

// ch340PortLabel is used in place of a real /dev/ttyUSB* path for logging when nothing more
// specific is known yet (no candidate device found, or - historically, before ch340PathID existed
// - as the value persisted to conf.SerialPortName). Not a valid ch340PathID() value itself, so it
// never accidentally matches a real device in ch340PathIDMatches() below.
const ch340PortLabel = "ch340(usb)"

// ch340PathIDPrefix marks a conf.SerialPortName value as a USB bus/port-path pin produced by
// ch340PathID, as opposed to the pre-existing ch340PortLabel constant (persisted by earlier
// versions of this file, before per-device pinning existed) or a Windows COM-port name that might
// end up here if a config file were ever copied across platforms. Never expected in practice, but
// distinguishing cleanly means ch340PathIDMatches can't be fooled by either.
const ch340PathIDPrefix = "ch340-usb:bus"

// ch340PathID builds a stable identifier for one specific physical USB device from its bus number
// and hub-port path (gousb.DeviceDesc.Bus / .Path) - e.g. "ch340-usb:bus1:1.4.2" for a device on
// bus 1, plugged into port 2 of a hub on port 4 of a hub on port 1 of the root hub. Both fields
// come straight from the device descriptor gousb already reads while enumerating - no extra sysfs
// parsing needed. Deliberately excludes DeviceDesc.Address (the address libusb assigns during
// enumeration): unlike Bus/Path, that's reassigned on every (re)enumeration and isn't stable even
// when the device stays in the same physical port.
//
// Stable across reboots and reconnects as long as the device stays in the same physical USB port;
// changes if it's moved to a different port (different hub, different port on the same hub, or a
// different machine entirely). That's an accepted limitation, not a bug - see ch340Candidates for
// what happens when a persisted identifier no longer matches anything currently connected.
func ch340PathID(desc *gousb.DeviceDesc) string {
	parts := make([]string, len(desc.Path))
	for i, p := range desc.Path {
		parts[i] = strconv.Itoa(p)
	}
	return fmt.Sprintf("%s%d:%s", ch340PathIDPrefix, desc.Bus, strings.Join(parts, "."))
}

// ch340PathIDMatches reports whether desc is the specific physical device a previously persisted
// identifier refers to. portName that isn't a valid ch340PathID value at all (the old
// ch340PortLabel constant from before this existed, an empty string, or anything else) simply
// means "no preference" rather than an error - selectCH340Device falls back to its pre-existing
// arbitrary-pick behavior in that case, so no migration step is needed: the first successful
// connect after upgrading naturally starts persisting a real identifier via FindPort()'s return
// value, same mechanism serial.go's reconnect() already uses for conf.SerialPortName.
func ch340PathIDMatches(portName string, desc *gousb.DeviceDesc) bool {
	return portName != "" && portName == ch340PathID(desc)
}

// ch340Candidates enumerates every USB device matching the SV241's CH340 VID/PID and returns them
// in the order openPort() below should try them: the device matching preferredID first, if one is
// given and currently present, then every other candidate in whatever order gousb enumerated them.
// Every returned *gousb.Device is open (via libusb_open) but not yet claimed - the caller owns
// closing all of them once it's done (see openPort).
//
// ctx.OpenDevices only opens a libusb handle per candidate - unlike claiming an interface, that
// never detaches a kernel driver or touches DTR/RTS, so opening (and, if unused, later closing)
// every candidate here is safe even for a device that turns out to be unrelated to the SV241
// (extremely common for this VID/PID - CH340 has no serial number, and 1a86:7523 shows up in
// countless cheap USB-serial adapters and Arduino clones).
func ch340Candidates(ctx *gousb.Context, preferredID string) ([]*gousb.Device, error) {
	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == ch340VID && desc.Product == ch340PID
	})
	if len(devs) == 0 {
		if err != nil {
			return nil, fmt.Errorf("enumerate CH340 devices: %w", err)
		}
		return nil, errors.New("no CH340 device found (is the SV241 box connected?)")
	}
	if err != nil {
		logger.Warn("ch340Candidates: error enumerating some USB devices (continuing with %d found): %v", len(devs), err)
	}

	if preferredID != "" {
		matched := false
		for i, d := range devs {
			if ch340PathIDMatches(preferredID, d.Desc) {
				devs[0], devs[i] = devs[i], devs[0] // move the pinned match to the front
				matched = true
				break
			}
		}
		if !matched && len(devs) > 1 {
			logger.Info("ch340Candidates: pinned device %q not found among %d CH340 candidate(s) - device moved to a different USB port?", preferredID, len(devs))
		}
	}
	return devs, nil
}

// tryOpenCH340 attempts to bring up one already-enumerated, still-open CH340 candidate (dev) and
// verify it's actually answering as the SV241, rather than just any device that happens to share
// its VID/PID. If forceful is false, this only tries the gentle path (reconfigure baud/LCR, verify
// with a real command - never touches DTR/RTS); a candidate that doesn't respond has its claimed
// interface released again (kernel driver reattached) but dev itself is left open, so a later
// forceful pass can reclaim the very same device without re-enumerating. If forceful is true, a
// gentle failure escalates to releasing DTR/RTS (see the package doc comment) before giving up.
//
// On failure, dev's interface (if it got that far) has been released, but dev itself is still
// open - the caller decides what happens to it next (retry forcefully, or dev.Close() once truly
// done with it). On success, the returned port owns dev and its claimed interface; its ctx field
// is left nil (this function was never given the *gousb.Context to set it) - the caller must set
// it before returning the port to anyone.
func tryOpenCH340(dev *gousb.Device, forceful bool) (*ch340Port, bool) {
	id := ch340PathID(dev.Desc)

	if err := dev.SetAutoDetach(true); err != nil {
		logger.Debug("CH340 candidate %s: set auto-detach failed: %v", id, err)
		return nil, false
	}
	dev.ControlTimeout = ch340ControlTimeout

	// Detach (and, on release, reattach) the kernel's ch341 driver as needed so we can claim the
	// interface ourselves - this is the actual bypass.
	intf, done, err := dev.DefaultInterface()
	if err != nil {
		logger.Debug("CH340 candidate %s: claim interface failed: %v", id, err)
		return nil, false
	}

	epIn, err := intf.InEndpoint(ch340BulkEndpointNum)
	if err != nil {
		done()
		logger.Debug("CH340 candidate %s: open bulk IN endpoint failed: %v", id, err)
		return nil, false
	}
	epOut, err := intf.OutEndpoint(ch340BulkEndpointNum)
	if err != nil {
		done()
		logger.Debug("CH340 candidate %s: open bulk OUT endpoint failed: %v", id, err)
		return nil, false
	}

	p := &ch340Port{
		dev:         dev,
		done:        done,
		epIn:        epIn,
		epOut:       epOut,
		readTimeout: 2 * time.Second,
		resolvedID:  id,
	}

	version, err := p.readChipVersion()
	if err != nil {
		logger.Debug("CH340 candidate %s: read chip version failed: %v", id, err)
		done()
		return nil, false
	}
	if err := p.configureBaudAndLCR(version); err != nil {
		logger.Debug("CH340 candidate %s: configure baud/LCR failed: %v", id, err)
		done()
		return nil, false
	}

	// Gentle path: never touch DTR/RTS, just check whether the device is already awake and
	// responsive - the common case for any reconnect after the very first one.
	if p.verifyResponsive(2 * time.Second) {
		logger.Info("CH340 (%s): gentle probe succeeded, device already responsive - no reset needed.", id)
		return p, true
	}
	if !forceful {
		done()
		return nil, false
	}

	// Forceful fallback: release DTR/RTS. On real hardware this is the one call that actually
	// resets the ESP32 - unavoidable the first time, since we don't know the chip's state yet.
	logger.Info("CH340 (%s): gentle probe got no response - releasing DTR/RTS and retrying.", id)
	if err := p.setModemCtrl(false, false); err != nil {
		logger.Debug("CH340 candidate %s: release DTR/RTS failed: %v", id, err)
		done()
		return nil, false
	}

	// The device just went through the release-induced reset. Swallow the FreeRTOS boot log
	// before checking again so a real caller's first command doesn't read garbage.
	time.Sleep(1500 * time.Millisecond)
	p.drain(100 * time.Millisecond)

	if !p.verifyResponsive(2 * time.Second) {
		done()
		return nil, false
	}
	return p, true
}

// FindPort looks for the SV241 device directly by USB VID/PID, bypassing the tty-node
// enumeration go.bug.st/serial/enumerator relies on entirely. That enumeration depends on the
// kernel's ch341 driver actually being bound (so /dev/ttyUSB* exists) - once this file has
// bypassed it even once, that device node is gone, so there's nothing for it to find.
//
// Called by serial.go only when there's no conf.SerialPortName to try first (see reconnect()), so
// openPort() below is always reached here with no preferred device pinned. Uses a considerably
// longer timeout than a single-candidate probe would need: with several CH340 devices connected
// and no pin to go on, openPort()'s two-pass, try-every-candidate approach can legitimately take
// several times as long as probing just one - see openPort's doc comment. On success, returns the
// actual physical device's ch340PathID (read back off the opened ch340Port) rather than the old
// constant label, so that whatever caller persists this as conf.SerialPortName (serial.go's
// reconnect()) ends up storing a real, disambiguating pin for next time.
func FindPort() (string, Port, error) {
	if p, success := probePortWithTimeout(ch340PortLabel, 30*time.Second); success {
		id := ch340PortLabel
		if cp, ok := p.(*ch340Port); ok && cp.resolvedID != "" {
			id = cp.resolvedID
		}
		return id, p, nil
	}
	return "", nil, errors.New("could not find SV241 device (CH340 1a86:7523) on the USB bus")
}

// openPort opens the SV241 box's CH340 chip directly by VID/PID, bypassing the kernel's serial
// driver. portName, if it's a ch340PathID previously returned by a successful open (persisted via
// conf.SerialPortName by serial.go's reconnect()), pins the search to that specific physical USB
// device - tried first, and, if it responds at all, used without ever looking at any other
// candidate. Anything else (no prior connect yet, the pre-pinning ch340PortLabel constant, or the
// pinned device having moved to a different port) means every currently-connected CH340 candidate
// is in play.
//
// When more than one candidate is in play, this tries all of them gently (no DTR/RTS) before
// forcefully resetting *any* of them: first every candidate's gentle path in turn, and only if
// none of them respond does it go back and try the DTR/RTS-releasing fallback on each in turn.
// That ordering matters - verified against real hardware with two CH340 devices connected: forcing
// DTR/RTS on whichever candidate happens to be tried first, before even checking whether a later
// candidate would have answered gently, means an unrelated device can get reset for no reason, and
// - since a single openPort() call only ever tried one candidate before this - meant the SV241
// could go permanently unfound if that first, wrong pick was consistent across retries (which it
// typically is; USB enumeration order doesn't change on its own). Trying every candidate gently
// first, then every candidate forcefully, guarantees the SV241 is found within one call as long as
// it's actually connected, at the cost of at most one unnecessary DTR/RTS toggle per unrelated
// candidate that's present - a one-time, bounded cost, and strictly better than the alternative.
func openPort(portName string) (Port, error) {
	ctx := gousb.NewContext()

	candidates, err := ch340Candidates(ctx, portName)
	if err != nil {
		ctx.Close()
		return nil, err
	}

	// closeExcept releases every enumerated candidate except keep (nil = all of them). Called on
	// every exit path below so nothing ch340Candidates opened ever leaks, regardless of how many
	// candidates actually got tried before a winner (or total failure) was found. Safe to call
	// even on a candidate tryOpenCH340 already released the *interface* of (dev.Close() closes
	// the underlying device handle, which is a separate, still-open resource until this runs).
	closeExcept := func(keep *gousb.Device) {
		for _, d := range candidates {
			if d != keep {
				d.Close()
			}
		}
	}

	for _, dev := range candidates {
		if p, ok := tryOpenCH340(dev, false); ok {
			closeExcept(dev)
			p.ctx = ctx
			return p, nil
		}
	}

	if len(candidates) > 1 {
		logger.Info("CH340: no candidate responded to a gentle probe among %d found - trying DTR/RTS release on each in turn.", len(candidates))
	}
	for _, dev := range candidates {
		if p, ok := tryOpenCH340(dev, true); ok {
			closeExcept(dev)
			p.ctx = ctx
			return p, nil
		}
	}

	closeExcept(nil)
	ctx.Close()
	return nil, errors.New("no CH340 device responded (is the SV241 box connected and powered?)")
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

// ResolvedName implements the optional resolvableName interface (see port.go) - lets reconnect()
// persist the specific physical device this port actually ended up bound to, even when it was
// opened via a name that didn't pin to it (the pre-pinning ch340PortLabel constant, an empty
// string on the very first-ever connect, or a pin that no longer matched anything currently
// connected - see ch340Candidates).
func (p *ch340Port) ResolvedName() string { return p.resolvedID }

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
