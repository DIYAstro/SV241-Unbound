//go:build windows

package serial

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// PrepareFlashConnection is the Windows implementation of the cross-platform hook
// internal/flasher calls right before connecting via espflasher. It exists because of a real,
// confirmed-on-hardware problem: go.bug.st/serial's SetDTR/SetRTS on Windows configure the port
// via SetCommState's DTR_CONTROL_ENABLE/RTS_CONTROL_ENABLE DCB flags (chosen specifically to
// work around a *different*, older bug in the raw EscapeCommFunction approach - see that
// package's own SetRTS doc comment) - both calls report success, but for the SV241's CH340-based
// USB-to-serial chip specifically, the WCH VCP driver never actually issues the USB control
// transfer that would change the physical DTR/RTS lines. Confirmed against real hardware: after a
// full classic-reset sequence issued purely through go.bug.st/serial's Port interface, the device
// never resets (no boot log observed on the wire at all) - but the exact same sequence issued via
// raw EscapeCommFunction (rawEscapeCommReset below) reliably lands the chip in the ROM bootloader
// ("rst:0x1 (POWERON_RESET),boot:0x3 (DOWNLOAD_BOOT(UART0/UART1/SDIO_REI_REO_V2))"). This is a
// Windows-driver-specific problem, not a Linux one: ch340_linux.go's own PrepareFlashConnection is
// a no-op, since its EnableRawControl-gated SetDTR/SetRTS already reach real hardware correctly
// there (they use the same libusb control-transfer path this file uses raw Win32 calls for).
//
// port is what serial.AcquirePortForFlashing returned; portName is its resolved COM name (e.g.
// "COM5"). port is closed here (its OS handle must be free for the raw reset below to claim
// exclusively) regardless of outcome. On success, returns a freshly reopened Port (via openPort,
// which leaves DTR/RTS held low - see its own doc comment) and alreadyInBootloader=true, telling
// the caller to configure espflasher with ResetNoReset rather than letting it attempt its own
// DTR/RTS sequence through the Port interface, which would hit this exact same broken code path.
func PrepareFlashConnection(port Port, portName string) (Port, bool, error) {
	if port != nil {
		port.Close()
	}

	if err := rawEscapeCommReset(portName); err != nil {
		return nil, false, fmt.Errorf("raw DTR/RTS reset on %s: %w", portName, err)
	}

	// Give the ROM bootloader a moment to start responding before espflasher's own sync loop
	// begins - mirrors the settle delay openPort() already uses after its own reset paths.
	time.Sleep(200 * time.Millisecond)

	newPort, err := openPort(portName)
	if err != nil {
		return nil, false, fmt.Errorf("reopen %s after reset: %w", portName, err)
	}
	return newPort, true, nil
}

// HardResetToApp is called by internal/flasher right after a successful flash (and after
// Flasher.Reset()), to reboot the chip into its newly-written application firmware. It exists
// for exactly the same reason PrepareFlashConnection does (see that function's doc comment):
// espflasher's own Flasher.Reset() tries to do this same job through the Port interface
// (ultimately calling go.bug.st/serial's SetRTS/SetDTR), which silently doesn't reach this
// hardware's actual DTR/RTS lines on Windows - so the chip would otherwise stay parked in the ROM
// bootloader after a successful flash instead of booting the firmware that was just written.
//
// port is closed here (its OS handle must be free for the raw reset to claim exclusively,
// exactly like PrepareFlashConnection) regardless of outcome, and a freshly reopened Port is
// returned for the caller to hand to serial.ReleaseFlashSession.
func HardResetToApp(port Port, portName string) (Port, error) {
	if port != nil {
		port.Close()
	}

	path, err := windows.UTF16PtrFromString(`\\.\` + portName)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", portName, err)
	}

	set := func(fn uint32) error { return windows.EscapeCommFunction(handle, fn) }
	resetErr := func() error {
		// DTR stays released (GPIO0 high -> normal boot, never bootloader) throughout - only EN
		// (RTS) pulses, mirroring espflasher's own non-USB hardReset() sequence in reset.go.
		if err := set(windows.CLRDTR); err != nil {
			return fmt.Errorf("CLRDTR: %w", err)
		}
		if err := set(windows.SETRTS); err != nil {
			return fmt.Errorf("SETRTS: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
		if err := set(windows.CLRDTR); err != nil {
			return fmt.Errorf("CLRDTR: %w", err)
		}
		return set(windows.CLRRTS)
	}()
	windows.CloseHandle(handle)
	if resetErr != nil {
		return nil, resetErr
	}

	time.Sleep(200 * time.Millisecond)
	return openPort(portName)
}

// rawEscapeCommReset performs the classic ESP32 auto-reset sequence (DTR->GPIO0, RTS->EN - see
// tinygo.org/x/espflasher's reset.go for the general algorithm this mirrors) via direct
// EscapeCommFunction calls against a freshly opened raw handle - see PrepareFlashConnection's doc
// comment for why go.bug.st/serial's SetDTR/SetRTS can't be used for this on Windows.
func rawEscapeCommReset(portName string) error {
	path, err := windows.UTF16PtrFromString(`\\.\` + portName)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", portName, err)
	}
	defer windows.CloseHandle(handle)

	set := func(fn uint32) error { return windows.EscapeCommFunction(handle, fn) }

	// Hold in reset (EN=LOW via RTS), GPIO0=HIGH (via DTR) first - normal-boot pin state while
	// held, so releasing EN alone (next step) is what actually matters for entering bootloader.
	if err := set(windows.CLRDTR); err != nil {
		return fmt.Errorf("CLRDTR: %w", err)
	}
	if err := set(windows.SETRTS); err != nil {
		return fmt.Errorf("SETRTS: %w", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Release EN (chip boots) while GPIO0 is simultaneously pulled low -> ROM bootloader.
	if err := set(windows.SETDTR); err != nil {
		return fmt.Errorf("SETDTR: %w", err)
	}
	if err := set(windows.CLRRTS); err != nil {
		return fmt.Errorf("CLRRTS: %w", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Release GPIO0.
	if err := set(windows.CLRDTR); err != nil {
		return fmt.Errorf("CLRDTR (final): %w", err)
	}
	return nil
}
