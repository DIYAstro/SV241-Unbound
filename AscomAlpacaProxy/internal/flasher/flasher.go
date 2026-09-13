// Package flasher performs native, in-process firmware flashing for the SV241 Pro's in-app
// flasher, replacing what used to be delegated entirely to the browser (Web Serial API +
// esp-web-tools) at /flasher. See flash.go for the actual flash sequence.
//
// Why native, and why this design:
//
//   - The old browser-driven flow needed the proxy to fully release the OS-level serial port
//     (see serial.ReleasePort/ResumeReconnect, still used by the "Pro Level" VS Code/esptool.py
//     workflow documented in docs/TROUBLESHOOTING.md) so the browser's Web Serial API could open
//     it itself. On Linux that meant handing the port back from this project's own libusb-based
//     CH340 driver (ch340_linux.go) to the kernel's ch341 tty driver and waiting for it to
//     reattach - a real, timing-sensitive round trip. Flashing from inside this same process,
//     over the same already-open (or freshly, flashing-specifically discovered) connection,
//     removes that hand-off entirely: internal/serial's own libusb path stays in control the
//     whole time.
//   - It also removes the "browser must run physically on the machine the box is plugged into"
//     constraint Web Serial imposes (see docs/PINS_INSTALL.md's old note about needing a VNC
//     session on headless Raspberry Pi/PINS setups) - since flashing now happens over this
//     project's own HTTP+WebSocket API, any browser on the LAN can drive it, the same way the
//     rest of the setup UI already works.
//   - Flashing itself is done via tinygo.org/x/espflasher/pkg/espflasher, a pure-Go
//     reimplementation of esptool's ROM-bootloader protocol. Its transport requirement is
//     go.bug.st/serial.Port (not a generic io.ReadWriteCloser) - see internal/serial's
//     FlashablePort alias (port.go) and the platform-specific glue each Port implementation
//     needs to satisfy it (serial_platform_other.go needs none; ch340_linux.go's EnableRawControl
//     gates real DTR/RTS control for the duration of a flash session only).
//
// Flash speed: FlashBaudRate is deliberately kept equal to BaudRate (115200), so espflasher's own
// changeBaud() step never runs. The bundled firmware is small (bootloader+partitions+app well
// under 500 KB total), so 115200 baud flashes in well under a minute - not worth the extra
// complexity of a runtime baud-rate switch, which on Linux would also need generalizing
// ch340_linux.go's currently 115200-only baud divisor and its own hardware verification. This is
// a deliberate scope decision, not an oversight - see the project's plan notes for this feature if
// revisiting it.
package flasher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
)

// ConfigFormatChangeVersion is the version threshold below which firmware always resets settings
// to defaults on update - 0.9.15 switched the on-device config storage format from a raw binary
// struct dump to JSON, so firmware older than this can't find its config file after an update
// regardless of whether the flash itself erased anything. The single source of truth for this
// decision, moved server-side from the old in-app flasher UI's hardcoded JS constant of the same
// name (frontend-vue/public/flasher/index.html, before this feature).
const ConfigFormatChangeVersion = "0.9.15"

// flasherFS is the "flasher" subtree of the embedded frontend filesystem, set once by Init - the
// same bytes server.go's own flasherFS (built the same way) serves over HTTP at
// /flasher/firmware/*.bin for the standalone web flasher's manifest. Reading them directly here
// avoids needing any new download/upload mechanism for "flash the version bundled with this
// proxy build."
var flasherFS fs.FS

// Init wires this package to the embedded frontend filesystem. Call once at startup, before any
// GetInfo/StartFlash call, with the same fs.FS server.Start already receives.
func Init(frontendFS fs.FS) {
	sub, err := fs.Sub(frontendFS, "flasher")
	if err != nil {
		logger.Error("flasher.Init: failed to create flasher sub-filesystem: %v", err)
		return
	}
	flasherFS = sub
}

// Info answers the in-app flasher UI's initial "what would happen if I flash now" question -
// installed vs. bundled version, and whether erasing should be recommended, forced, or left off.
type Info struct {
	InstalledVersion string `json:"installedVersion"` // "unknown" if not currently connected
	BundledVersion   string `json:"bundledVersion"`
	UpToDate         bool   `json:"upToDate"`

	// RecommendEraseDefault pre-checks the UI's erase checkbox without disabling it - a plain
	// version mismatch where the user might still want to erase, but preserving settings is the
	// safe default.
	RecommendEraseDefault bool `json:"recommendEraseDefault"`

	// ForceErase means the installed firmware predates ConfigFormatChangeVersion: erasing isn't
	// optional here (updating always resets settings regardless), so the UI should disable the
	// checkbox in the checked state rather than merely default it - see ConfigFormatChangeVersion.
	ForceErase bool `json:"forceErase"`

	// DefaultPort is the last-known/pinned serial port name, for the UI to preselect if it shows
	// a port picker (see AmbiguousPortError). Empty if never connected.
	DefaultPort string `json:"defaultPort,omitempty"`
}

// GetInfo reports the installed vs. bundled firmware version and the resulting erase
// recommendation. Never fails on "not currently connected" - InstalledVersion is just "unknown"
// in that case, matching serial.GetFirmwareVersion()'s own convention.
func GetInfo() (Info, error) {
	bundled, err := bundledVersion()
	if err != nil {
		return Info{}, err
	}

	info := Info{
		InstalledVersion: serial.GetFirmwareVersion(),
		BundledVersion:   bundled,
		DefaultPort:      config.Get().SerialPortName,
	}

	if strings.EqualFold(info.InstalledVersion, "unknown") {
		return info, nil
	}

	info.UpToDate = info.InstalledVersion == bundled
	if !info.UpToDate && isVersionOlder(info.InstalledVersion, ConfigFormatChangeVersion) {
		info.ForceErase = true
	}
	return info, nil
}

// bundledVersion reads the version bundled with this proxy build, from the same version.json the
// standalone web flasher already serves at /flasher/firmware/version.json.
func bundledVersion() (string, error) {
	if flasherFS == nil {
		return "", errors.New("flasher not initialized")
	}
	data, err := fs.ReadFile(flasherFS, "firmware/version.json")
	if err != nil {
		return "", fmt.Errorf("read bundled version.json: %w", err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse bundled version.json: %w", err)
	}
	return v.Version, nil
}

// isVersionOlder reports whether version a is older than b, comparing only the numeric "x.y.z"
// core - a straight port of the old flasher/index.html's JS function of the same name (see that
// file's git history for the reasoning about stripping pre-release suffixes like "-daily.<sha>"
// before comparing, which a naive Atoi on the raw segment would otherwise silently break).
func isVersionOlder(a, b string) bool {
	base := func(v string) string {
		if i := strings.IndexByte(v, '-'); i >= 0 {
			return v[:i]
		}
		return v
	}
	pa := strings.Split(base(a), ".")
	pb := strings.Split(base(b), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	part := func(parts []string, i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(parts[i]) // non-numeric (e.g. a stray suffix) reads as 0, same as JS's Number()||0
		return n
	}
	for i := 0; i < n; i++ {
		na, nb := part(pa, i), part(pb, i)
		if na != nb {
			return na < nb
		}
	}
	return false
}

// JobPhase identifies a stage of an in-progress or finished flash job.
type JobPhase string

const (
	PhaseIdle       JobPhase = "idle"
	PhaseConnecting JobPhase = "connecting"
	PhaseErasing    JobPhase = "erasing"
	PhaseWriting    JobPhase = "writing"
	PhaseVerifying  JobPhase = "verifying"
	PhaseResetting  JobPhase = "resetting"
	PhaseDone       JobPhase = "done"
	PhaseError      JobPhase = "error"
)

// JobStatus is broadcast over /ws/flash (and returned by GET /api/v1/flash/status as a polling
// fallback) on every phase/progress change of the current or most recently finished flash job.
type JobStatus struct {
	Phase   JobPhase `json:"phase"`
	Percent int      `json:"percent"`
	Message string   `json:"message"`
	Error   string   `json:"error,omitempty"`
}

var (
	statusMu sync.Mutex
	status   = JobStatus{Phase: PhaseIdle}
	running  bool
)

// CurrentStatus returns the most recent JobStatus - the current job's progress, or the outcome of
// the last one if none is running.
func CurrentStatus() JobStatus {
	statusMu.Lock()
	defer statusMu.Unlock()
	return status
}

// setStatus updates the shared status and broadcasts it to every connected /ws/flash client (see
// ws.go). Safe to call from the flash goroutine at any point.
func setStatus(s JobStatus) {
	statusMu.Lock()
	status = s
	statusMu.Unlock()
	broadcast(s)
}

// ErrAlreadyRunning is returned by StartFlash if a flash job is already in progress.
var ErrAlreadyRunning = errors.New("a flash job is already running")

// StartFlash acquires the port and begins a flash job in the background, returning as soon as
// the device to flash is resolved - the flash itself then proceeds asynchronously, reported via
// CurrentStatus/the /ws/flash broadcast, not this call's return value. This split matters for one
// reason: port resolution (adopting the currently open connection, or a quick USB enumeration if
// not connected) can fail with a *serial.AmbiguousPortError when more than one candidate device
// is present - the caller (the HTTP handler) needs that synchronously, as an immediate error
// response with the candidate list for a picker, not discovered later via a job status that
// already claims to be "running".
//
// erase forces a full flash erase before writing (see Info.ForceErase/RecommendEraseDefault);
// portOverride pins the device to flash - typically the user's explicit choice after a prior
// *serial.AmbiguousPortError, or their last-known port - "" defers to
// serial.AcquirePortForFlashing's own default resolution.
func StartFlash(erase bool, portOverride string) error {
	statusMu.Lock()
	if running {
		statusMu.Unlock()
		return ErrAlreadyRunning
	}
	running = true
	statusMu.Unlock()

	port, portName, err := serial.AcquirePortForFlashing(portOverride)
	if err != nil {
		statusMu.Lock()
		running = false
		statusMu.Unlock()
		return err
	}

	setStatus(JobStatus{Phase: PhaseConnecting, Message: "Connecting to bootloader..."})
	go func() {
		defer func() {
			statusMu.Lock()
			running = false
			statusMu.Unlock()
		}()
		runFlash(port, portName, erase)
	}()
	return nil
}
