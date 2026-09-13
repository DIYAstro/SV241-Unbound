package flasher

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"time"

	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"

	bugstserial "go.bug.st/serial"
	"tinygo.org/x/espflasher/pkg/espflasher"
)

// firmwareOffsets are the standard PlatformIO esp32dev default partition-table offsets this
// project's firmware.zip release (and the standalone/in-app web flashers before this feature) has
// always assumed - see platformio.ini's [env:Firmware_ESP32] (board = esp32dev, no custom
// partition table) for where these come from.
const (
	bootloaderOffset uint32 = 0x1000
	partitionsOffset uint32 = 0x8000
	appOffset        uint32 = 0x10000
)

// loadBundledImages reads the three flash images bundled with this proxy build (see flasherFS's
// own doc comment in flasher.go) directly from the embedded filesystem - never served over HTTP.
func loadBundledImages() ([]espflasher.ImagePart, error) {
	if flasherFS == nil {
		return nil, fmt.Errorf("flasher not initialized")
	}
	load := func(name string) ([]byte, error) {
		data, err := fs.ReadFile(flasherFS, "firmware/"+name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return data, nil
	}

	bootloader, err := load("bootloader.bin")
	if err != nil {
		return nil, err
	}
	partitions, err := load("partitions.bin")
	if err != nil {
		return nil, err
	}
	firmware, err := load("firmware.bin")
	if err != nil {
		return nil, err
	}

	return []espflasher.ImagePart{
		{Data: bootloader, Offset: bootloaderOffset},
		{Data: partitions, Offset: partitionsOffset},
		{Data: firmware, Offset: appOffset},
	}, nil
}

// rawControlToggle is implemented only by ch340_linux.go's ch340Port - see EnableRawControl's
// doc comment there for why Linux needs this and Windows/macOS don't (go.bug.st/serial's
// SetDTR/SetRTS already work directly and unconditionally on those platforms).
type rawControlToggle interface {
	EnableRawControl()
	DisableRawControl()
}

// nonClosingPort wraps a serial.FlashablePort so espflasher's own internal Close()/reopen calls
// (used when a reset causes the underlying USB device to briefly re-enumerate) never actually
// close the connection this package borrowed from serial.AcquirePortForFlashing - ownership of
// closing it, or handing it back to the connection manager, belongs to runFlash via
// serial.ReleaseFlashSession, not to the *espflasher.Flasher using it as a transport.
//
// This also means espflasher's own reopenPort (used after a reset that re-enumerates the USB
// device) can never succeed on this hardware - the SV241's CH340 chip doesn't disconnect from
// USB on an ESP32 reset the way a native-USB chip (ESP32-S3's USB-JTAG/Serial peripheral) would,
// so ResetDefault's classic DTR/RTS sequence never needs it in the first place.
type nonClosingPort struct {
	serial.FlashablePort
}

func (p *nonClosingPort) Close() error { return nil }

// flasherLogAdapter routes espflasher's own internal log lines into this project's logger at
// Debug level, for diagnosing a failed flash from proxy.log without cluttering Info.
type flasherLogAdapter struct{}

func (flasherLogAdapter) Logf(format string, args ...interface{}) {
	logger.Debug("espflasher: "+format, args...)
}

// runFlash performs one complete flash job on an already-acquired port (see StartFlash, which
// resolves port and portName synchronously via serial.AcquirePortForFlashing before spawning
// this): connect, optionally erase, write, verify, reset, and hand the port back - reporting
// progress via setStatus at every step. Always runs in its own goroutine; never returns an error
// to a caller directly, since there is none - failures are reported as a
// JobStatus{Phase: PhaseError} instead.
func runFlash(port serial.Port, portName string, erase bool) {
	// PrepareFlashConnection handles a platform-specific quirk before anything else touches the
	// port: on Windows, go.bug.st/serial's own SetDTR/SetRTS don't reliably reach this hardware
	// (confirmed against real hardware - see that function's doc comment), so it performs the
	// reset itself via a lower-level mechanism and hands back a fresh connection already sitting
	// in the ROM bootloader; on Linux/macOS it's a no-op and port comes back unchanged.
	port, alreadyInBootloader, err := serial.PrepareFlashConnection(port, portName)
	if err != nil {
		serial.ReleaseFlashSession(portName, nil)
		fail(fmt.Errorf("prepare connection: %w", err))
		return
	}

	fp, ok := port.(serial.FlashablePort)
	if !ok {
		// Unreachable in practice - both platform Port implementations satisfy FlashablePort (see
		// port.go's doc comment) - but fail safely and hand the port back rather than leak it.
		serial.ReleaseFlashSession(portName, port)
		fail(fmt.Errorf("internal error: %s does not support flashing", portName))
		return
	}

	if raw, ok := port.(rawControlToggle); ok {
		raw.EnableRawControl()
		defer raw.DisableRawControl()
	}

	images, err := loadBundledImages()
	if err != nil {
		serial.ReleaseFlashSession(portName, port)
		fail(err)
		return
	}

	// ResetNoReset when PrepareFlashConnection already got the chip into the ROM bootloader
	// itself - letting espflasher additionally attempt its own DTR/RTS sequence through the Port
	// interface would, on Windows, just hit the exact same broken code path a second time.
	resetMode := espflasher.ResetDefault
	if alreadyInBootloader {
		resetMode = espflasher.ResetNoReset
	}

	opts := espflasher.DefaultOptions()
	opts.ChipType = espflasher.ChipESP32
	opts.ResetMode = resetMode
	opts.BaudRate = 115200
	opts.FlashBaudRate = 115200 // see flasher.go's package doc comment for why
	opts.Logger = flasherLogAdapter{}
	opts.SerialOpener = func(_ string, _ *bugstserial.Mode) (bugstserial.Port, error) {
		return &nonClosingPort{fp}, nil
	}
	opts.ConnectStatus = func(phase espflasher.ConnectPhase, attempt, maxAttempts int, message string) {
		percent := 0
		if maxAttempts > 0 {
			percent = attempt * 100 / maxAttempts
		}
		setStatus(JobStatus{Phase: PhaseConnecting, Percent: percent, Message: message})
	}

	setStatus(JobStatus{Phase: PhaseConnecting, Message: "Connecting to bootloader..."})
	fl, err := espflasher.New(portName, opts)
	if err != nil {
		serial.ReleaseFlashSession(portName, nil)
		fail(fmt.Errorf("connect: %w", err))
		return
	}
	defer fl.Close() // no-op: nonClosingPort.Close() never closes the real transport

	if erase {
		setStatus(JobStatus{Phase: PhaseErasing, Message: "Erasing flash..."})
		err := fl.EraseFlash(func(cur, total int) {
			setStatus(JobStatus{Phase: PhaseErasing, Percent: progressPercent(cur, total), Message: "Erasing flash..."})
		})
		if err != nil {
			serial.ReleaseFlashSession(portName, nil)
			fail(fmt.Errorf("erase: %w", err))
			return
		}
	}

	setStatus(JobStatus{Phase: PhaseWriting, Message: "Writing firmware..."})
	if err := fl.FlashImages(images, func(cur, total int) {
		setStatus(JobStatus{Phase: PhaseWriting, Percent: progressPercent(cur, total), Message: "Writing firmware..."})
	}); err != nil {
		serial.ReleaseFlashSession(portName, nil)
		fail(fmt.Errorf("write: %w", err))
		return
	}

	// Extra safety the old browser-driven flow never had: verify each region's MD5 against what
	// was actually meant to be written, on top of whatever internal verification espflasher's own
	// stub-based write path already does.
	setStatus(JobStatus{Phase: PhaseVerifying, Message: "Verifying..."})
	for _, img := range images {
		sum, err := fl.GetFlashMD5(img.Offset, uint32(len(img.Data)), nil)
		if err != nil {
			serial.ReleaseFlashSession(portName, nil)
			fail(fmt.Errorf("verify at 0x%08X: %w", img.Offset, err))
			return
		}
		expected := md5.Sum(img.Data)
		if sum != hex.EncodeToString(expected[:]) {
			serial.ReleaseFlashSession(portName, nil)
			fail(fmt.Errorf("verify at 0x%08X: MD5 mismatch", img.Offset))
			return
		}
	}

	setStatus(JobStatus{Phase: PhaseResetting, Message: "Resetting device..."})
	fl.Reset()
	time.Sleep(500 * time.Millisecond) // let the reset pulse finish before the platform hook below

	// A no-op returning port unchanged on platforms where fl.Reset() above already reached real
	// hardware correctly (Linux, macOS); on Windows, performs the actual reset itself and returns
	// a freshly reopened connection - see HardResetToApp's doc comment for why it's needed there.
	resultPort, err := serial.HardResetToApp(port, portName)
	if err != nil {
		logger.Warn("Firmware flash: device may not have rebooted into the new firmware: %v", err)
		resultPort = nil // let the ordinary watchdog rediscover it rather than adopt a broken handle
	}

	serial.ReleaseFlashSession(portName, resultPort)
	setStatus(JobStatus{Phase: PhaseDone, Percent: 100, Message: "Flash complete."})
}

func progressPercent(current, total int) int {
	if total <= 0 {
		return 0
	}
	p := current * 100 / total
	if p > 100 {
		p = 100
	}
	return p
}

func fail(err error) {
	logger.Error("Firmware flash failed: %v", err)
	setStatus(JobStatus{Phase: PhaseError, Message: "Flash failed.", Error: err.Error()})
}
