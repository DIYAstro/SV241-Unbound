package serial

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/events"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync"
	"time"
)

// Command defines a command to be sent to the serial device.
type Command struct {
	Command  string
	Response chan<- string
	Error    chan<- error
	Timeout  time.Duration
}

// StatusCache stores the latest power status from the device.
type StatusCache struct {
	Data map[string]interface{}
	*sync.RWMutex
}

// ConditionsCache stores the latest sensor readings from the device.
type ConditionsCache struct {
	Data       map[string]interface{}
	LastUpdate time.Time
	*sync.RWMutex
}

var (
	highPriorityCommands = make(chan Command)
	lowPriorityCommands  = make(chan Command)
	sv241Port            Port
	portMutex            = &sync.Mutex{}
	firmwareVersion      = "unknown"
	firmwareVersionMu    sync.RWMutex

	// Caches are managed within the serial package
	Status     = &StatusCache{RWMutex: &sync.RWMutex{}}
	Conditions = &ConditionsCache{RWMutex: &sync.RWMutex{}}

	// Memory logging state
	lastLoggedHeapFree     float64
	lastLoggedHeapMinFree  float64
	lastLoggedHeapMaxAlloc float64
	lastLoggedHeapSize     float64
	lastMemoryLogTime      time.Time

	// lastSentStatus tracks the last connection status event sent to avoid duplicate notifications.
	lastSentStatus events.ComPortStatus = events.Disconnected

	// ActiveVoltageTarget tracks the last set voltage for the "adj" output (RAM target).
	// Initialized to -1.0 to indicate "unknown/unset" (use config default).
	ActiveVoltageTarget = -1.0
	VoltageMutex        sync.RWMutex

	// reconnectPaused prevents the connection manager from auto-reconnecting.
	// Used when the flasher releases the port for external access.
	reconnectPaused = false

	// consecutiveReadFailures counts commands that exhausted their retries without a response,
	// in a row. Reset on the first successful command. Only once it reaches
	// maxConsecutiveReadFailures do we actually tear the connection down - see that constant's
	// definition (per-platform) for why closing the port is expensive on Linux.
	// Guarded by portMutex.
	consecutiveReadFailures = 0
)

// StartManager initializes all background tasks for serial communication.
func StartManager() {
	initDone := make(chan struct{})

	go ProcessCommands()
	go ManageConnection(initDone)
	go periodicCacheUpdater(initDone)

	// Perform an initial, synchronous connection attempt.
	logger.Info("Performing initial device connection attempt...")
	conf := config.Get()
	if conf.SerialPortName != "" {
		logger.Info("Initial Connection: Trying configured port '%s'.", conf.SerialPortName)
		portMutex.Lock()
		reconnect(conf.SerialPortName, nil)
		portMutex.Unlock()
	} else {
		logger.Info("Initial Connection: Starting auto-detection...")
		foundPort, foundHandle, err := FindPort()
		if err != nil {
			logger.Warn("Initial Connection: Auto-detection failed: %v", err)
		} else {
			logger.Info("Auto-detection found device on port %s. Connecting...", foundPort)
			portMutex.Lock()
			reconnect(foundPort, foundHandle)
			portMutex.Unlock()
		}
	}

	portMutex.Lock()
	connected := sv241Port != nil
	portMutex.Unlock()
	if connected {
		logger.Info("Initial connection attempt finished successfully.")
		// Populate Status.Data (including "dm", the heater mode array) synchronously before
		// anything else can race ahead - periodicCacheUpdater below does the same fetch, but only
		// after close(initDone) unblocks it, and server.Start() (called by our own caller right
		// after StartManager returns) could already be accepting requests by the time that
		// goroutine actually runs. Without this, a GetSwitchValue for a manual-mode heater that
		// arrives in that window reads Status.Data["dm"] as missing and returns a clamped,
		// wrong value. Deliberately NOT done inside reconnect() itself - that always runs under
		// portMutex, which SendCommand's own processor (ProcessCommands) needs too, so calling it
		// there would deadlock. Only needed here, once: a reconnect later in the process's life
		// leaves the previous (stale but present) "dm" entry in Status.Data until this same
		// periodic update naturally refreshes it, so there's no equivalent gap to close there.
		performCacheUpdate()
	} else {
		logger.Warn("Initial connection attempt failed. The application will continue to try connecting in the background.")
	}

	// Signal background tasks to start their main loops.
	logger.Info("Signaling background tasks to start main loops.")
	close(initDone)

}

// IsConnected returns the current connection status of the serial port.
func IsConnected() bool {
	portMutex.Lock()
	defer portMutex.Unlock()
	return sv241Port != nil
}

// GetFirmwareVersion returns the cached firmware version.
func GetFirmwareVersion() string {
	firmwareVersionMu.RLock()
	defer firmwareVersionMu.RUnlock()
	return firmwareVersion
}

// SendCommand queues a command to be sent to the device.
func SendCommand(command string, isHighPriority bool, timeout time.Duration) (string, error) {
	if timeout == 0 {
		timeout = 3 * time.Second // Default timeout
	}

	responseChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	cmd := Command{
		Command:  command,
		Response: responseChan,
		Error:    errorChan,
		Timeout:  timeout,
	}

	if isHighPriority {
		logger.Debug("Queueing high-priority command: %s", command)
		highPriorityCommands <- cmd
	} else {
		logger.Debug("Queueing low-priority command: %s", command)
		lowPriorityCommands <- cmd
	}

	select {
	case response := <-responseChan:
		return response, nil
	case err := <-errorChan:
		return "", err
	case <-time.After(timeout):
		return "", errors.New("command timed out waiting for response from processor")
	}
}

// ProcessCommands is the heart of the command prioritization system.
func ProcessCommands() {
	logger.Info("Serial command processor started.")
	for {
		var cmd Command
		select {
		case cmd = <-highPriorityCommands:
		default:
			select {
			case cmd = <-highPriorityCommands:
			case cmd = <-lowPriorityCommands:
			}
		}

		portMutex.Lock()
		if sv241Port == nil {
			portMutex.Unlock()
			cmd.Error <- errors.New("serial port is not open")
			continue
		}

		// Drain input buffer to remove unsolicited data (e.g. boot logs) before sending new command
		// This ensures the next line we read is likely the response to our command.
		// We read with a very short timeout until no more data is available.
		drainInputBuffer(sv241Port)

		logger.Debug("Processing command: %s", cmd.Command)
		_, err := sv241Port.Write([]byte(cmd.Command + "\n"))
		if err != nil {
			logger.Error("Serial write failed: %v. Marking port as disconnected.", err)
			handleDisconnect()
			portMutex.Unlock()
			cmd.Error <- fmt.Errorf("failed to write to serial port: %w", err)
			continue
		}

		// Read response with retry logic to tolerate transient timeouts.
		// Some Windows USB stacks (especially CH340) can occasionally miss a response.
		var response string
		var readErr error
		maxRetries := 2
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				logger.Warn("Retry %d/%d for command: %s", attempt, maxRetries, cmd.Command)
				drainInputBuffer(sv241Port)
				_, err = sv241Port.Write([]byte(cmd.Command + "\n"))
				if err != nil {
					logger.Error("Serial write failed on retry: %v. Marking port as disconnected.", err)
					handleDisconnect()
					portMutex.Unlock()
					cmd.Error <- fmt.Errorf("failed to write to serial port: %w", err)
					break
				}
			}
			response, readErr = readLine(sv241Port, cmd.Timeout)
			if readErr == nil {
				break
			}
			logger.Warn("Serial read attempt %d/%d failed: %v", attempt+1, maxRetries+1, readErr)
		}
		if readErr != nil {
			consecutiveReadFailures++
			if consecutiveReadFailures >= maxConsecutiveReadFailures {
				logger.Error("Serial read failed after %d attempts, %d commands in a row. Marking port as disconnected.",
					maxRetries+1, consecutiveReadFailures)
				handleDisconnect()
			} else {
				// Keep the port open and just fail this one command. Reconnecting costs a device
				// reset (and with it the user's live output states) on Linux, so a single missed
				// response is not worth tearing the link down for.
				logger.Warn("Serial read failed after %d attempts (%d/%d in a row). Keeping port open.",
					maxRetries+1, consecutiveReadFailures, maxConsecutiveReadFailures)
			}
			portMutex.Unlock()
			cmd.Error <- fmt.Errorf("failed to read from serial port: %w", readErr)
			continue
		}
		consecutiveReadFailures = 0
		portMutex.Unlock()

		trimmedResponse := strings.TrimSpace(response)
		logger.Debug("Received response from device: %s", trimmedResponse)

		// The firmware rejects some "set" commands (e.g. a disabled output) by sending a single
		// {"error":"..."} line instead of the usual status JSON - surface that as a real Go error
		// rather than a "successful" response callers would otherwise silently ignore. This is
		// the device's only use of a top-level "error" key, so no legitimate response is
		// misclassified by this check.
		if strings.Contains(trimmedResponse, `"error":`) {
			var errResp struct {
				Error string `json:"error"`
			}
			if json.Unmarshal([]byte(trimmedResponse), &errResp) == nil && errResp.Error != "" {
				cmd.Error <- fmt.Errorf("device rejected command: %s", errResp.Error)
				continue
			}
		}

		// Instant Cache Update (Turbo): Sniff the response for status or sensor data.
		// If found, update the global cache immediately so NINA sees the change without waiting for the poller.
		if strings.Contains(trimmedResponse, `"status":`) {
			updateStatusCacheFromJSON(trimmedResponse)
		} else if strings.Contains(trimmedResponse, `"sht_temperature":`) {
			updateConditionsCacheFromJSON(trimmedResponse)
		}

		cmd.Response <- trimmedResponse
	}
}

// drainInputBuffer reads from the port until no more data is available.
// This removes stale data (e.g., boot logs, unsolicited output) before sending a new command.
func drainInputBuffer(port Port) {
	port.SetReadTimeout(50 * time.Millisecond)
	buf := make([]byte, 1024)
	for {
		n, err := port.Read(buf)
		if err != nil || n == 0 {
			break
		}
		// Keep draining until the buffer is empty
	}
}

// readLine reads from the port until a newline is encountered or timeout.
// Uses chunk-based reading (256 bytes) instead of byte-by-byte to minimize syscall overhead.
func readLine(port Port, timeout time.Duration) (string, error) {
	port.SetReadTimeout(timeout)
	var result []byte
	buf := make([]byte, 256)
	start := time.Now()

	for {
		if time.Since(start) > timeout {
			return "", errors.New("read timeout")
		}

		n, err := port.Read(buf)
		if err != nil {
			return "", err
		}
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					return string(result), nil
				}
				result = append(result, buf[i])
			}
		}
	}
}

// ManageConnection is a background task that ensures the device stays connected.
func ManageConnection(initDone chan struct{}) {
	logger.Info("Connection manager task started. Waiting for initial signal...")
	<-initDone
	logger.Info("Initial signal received. Starting connection management.")

	for {
		time.Sleep(5 * time.Second)
		logger.Debug("Connection Manager: Checking connection status...")

		portMutex.Lock()
		// Skip reconnection if paused (e.g., during flashing)
		if reconnectPaused {
			logger.Debug("Connection Manager: Reconnect is paused. Skipping.")
			portMutex.Unlock()
			continue
		}

		isConnected := (sv241Port != nil)
		if !isConnected {
			logger.Info("Connection Manager: Device is disconnected. Attempting to connect...")
			conf := config.Get()
			targetPort := conf.SerialPortName
			autoDetect := conf.AutoDetectPort

			// Wenn Auto-Detect AUS ist, versuchen wir NUR den konfigurierten Port.
			if !autoDetect && targetPort != "" {
				logger.Info("Connection Manager: Trying configured port '%s' for reconnection.", targetPort)
				reconnect(targetPort, nil)
			} else {
				// Wenn Auto-Detect AN ist (oder kein Port konfiguriert ist), verhalten wir uns wie bisher.
				if targetPort != "" {
					logger.Info("Connection Manager: Trying configured port '%s' for reconnection.", targetPort)
					reconnect(targetPort, nil)
					if sv241Port == nil {
						logger.Warn("Connection Manager: Configured port '%s' failed. Falling back to auto-detection.", targetPort)
						conf.SerialPortName = "" // Leeren, damit der nächste Versuch den Autoscan nutzt
						config.Save()
					}
				}

				// Wenn immer noch nicht verbunden, starte den Autoscan.
				if sv241Port == nil {
					logger.Info("Connection Manager: Starting auto-detection...")
					foundPort, foundHandle, err := FindPort()
					if err != nil {
						logger.Warn("Connection Manager: Auto-detection failed: %v", err)
					} else {
						logger.Info("Connection Manager: Auto-detection found device on port %s. Connecting...", foundPort)
						reconnect(foundPort, foundHandle)
					}
				}
			}
		} else {
			logger.Debug("Connection Manager: Device is connected.")
		}
		portMutex.Unlock()
	}
}

// FindPort iterates through available serial ports to find the SV241 device.
// probePortWithTimeout probes a port with a hard timeout that guarantees cleanup.
// Uses a goroutine for the actual probe, but closes the port if timeout occurs.
func probePortWithTimeout(portName string, timeout time.Duration) (Port, bool) {
	type probeResult struct {
		success bool
		port    Port
	}
	resultChan := make(chan probeResult, 1)

	// Shared variable for port handle - allows cleanup on timeout
	var probePort Port
	var probeMutex sync.Mutex

	go func() {
		// openPort does whatever platform-specific reset/settle handling is needed and returns
		// a port that's already ready for commands - see serial_platform_other.go (Windows) and
		// ch340_linux.go (Linux) for what that actually involves.
		p, err := openPort(portName)
		if err != nil {
			logger.Warn("Could not open port %s to probe: %v", portName, err)
			resultChan <- probeResult{false, nil}
			return
		}

		// Store port handle for potential cleanup
		probeMutex.Lock()
		probePort = p
		probeMutex.Unlock()

		p.SetReadTimeout(2 * time.Second)

		var success bool
		_, err = p.Write([]byte("{\"get\":\"sensors\"}\n"))
		if err != nil {
			logger.Debug("Port %s: Write failed: %v", portName, err)
		} else {
			for i := 0; i < 5; i++ {
				line, readErr := readLine(p, 2 * time.Second)
				if readErr != nil {
					logger.Debug("Port %s: Read failed or timed out: %v", portName, readErr)
					break
				}
	
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
	
				var js json.RawMessage
				if json.Unmarshal([]byte(trimmed), &js) == nil {
					logger.Info("Successfully probed port: %s", portName)
					success = true
					break
				}
				logger.Debug("Port %s: Line was not valid JSON: %s", portName, trimmed)
			}
		}

		// Clear the shared handle since we are done with probing phase
		probeMutex.Lock()
		probePort = nil
		probeMutex.Unlock()

		if success {
			resultChan <- probeResult{true, p}
		} else {
			p.Close()
			resultChan <- probeResult{false, nil}
		}
	}()

	// Wait for result with hard timeout
	select {
	case res := <-resultChan:
		return res.port, res.success
	case <-time.After(timeout):
		logger.Warn("Port %s: Probe timed out after %v. Forcing cleanup.", portName, timeout)

		// Force close the port if goroutine is still holding it
		probeMutex.Lock()
		if probePort != nil {
			probePort.Close()
			probePort = nil
		}
		probeMutex.Unlock()

		return nil, false
	}
}

// Reconnect is a public wrapper for reconnecting, intended to be called from other packages.
func Reconnect(portName string) {
	ReconnectWithHandle(portName, nil)
}

// ReconnectWithHandle is like Reconnect, but lets the caller pass an already-open, already
// boot-settled port handle (e.g. one returned by FindPort()) to be adopted instead of forcing a
// second open+boot-settle cycle - avoiding a redundant reset pulse to the device. Passing nil
// behaves exactly like Reconnect.
func ReconnectWithHandle(portName string, preOpenedPort Port) {
	portMutex.Lock()
	defer portMutex.Unlock()
	reconnect(portName, preOpenedPort)
}

// FindAndConnect probes for the SV241 device and, if found, connects to it - all while holding
// portMutex for the whole operation. This is the same find-then-adopt pattern ManageConnection's
// own auto-detect branch already uses internally; exposing it as its own function lets other
// callers (e.g. handleRestoreBackup's "reconnect right now" flow) use it too, instead of calling
// the unlocked FindPort() on their own. FindPort() itself takes no lock, so calling it outside of
// portMutex let it race with the watchdog's own auto-detect probe for the same USB port - not
// harmful (the loser just gets a clean "port busy" and backs off), but noisy and wasteful.
// Returns the connected port name, or an error if no device could be found.
func FindAndConnect() (string, error) {
	portMutex.Lock()
	defer portMutex.Unlock()

	// Someone else (typically the watchdog) may have already connected while we were waiting for
	// the lock - re-probing the port now would only collide with that already-open handle and
	// fail with a spurious "port busy". Nothing to do in that case, we're already connected.
	if sv241Port != nil {
		return config.Get().SerialPortName, nil
	}

	foundPort, foundHandle, err := FindPort()
	if err != nil {
		return "", err
	}
	reconnect(foundPort, foundHandle)
	return foundPort, nil
}

// reconnect attempts to close the current port and open a new one.
// It MUST be called within a portMutex lock.
func reconnect(newPortName string, preOpenedPort Port) {
	handleDisconnect() // Close existing port if any

	if newPortName != "" {
		var p Port
		var err error

		if preOpenedPort != nil {
			logger.Info("Using auto-detected, pre-opened serial port: %s", newPortName)
			p = preOpenedPort
		} else {
			logger.Info("Attempting to open serial port: %s", newPortName)
			// openPort does whatever platform-specific reset/settle handling is needed and
			// returns a port that's already ready for commands - see serial_platform_other.go
			// (Windows) and ch340_linux.go (Linux) for what that actually involves.
			p, err = openPort(newPortName)
			if err != nil {
				logger.Error("reconnect: Failed to open port %s: %v", newPortName, err)
			}
		}

		if p != nil {
			sv241Port = p

			// Persist what the port actually resolved to, not necessarily newPortName itself -
			// they differ whenever p implements resolvableName (currently just ch340Port on
			// Linux) and newPortName was a stale/non-pinning value (no prior pin, the pre-pinning
			// ch340PortLabel constant, or a pin that no longer matched anything). Without this,
			// a value that never round-trips through resolvableName (like ch340PortLabel) would
			// keep re-saving itself here forever, since this line runs on *every* successful
			// (re)connect, including ones that went through the preOpenedPort branch above where
			// openPort() (and its own internal resolution) is never even called.
			resolvedName := newPortName
			if rn, ok := p.(resolvableName); ok {
				if resolved := rn.ResolvedName(); resolved != "" {
					resolvedName = resolved
				}
			}

			conf := config.Get()
			conf.SerialPortName = resolvedName // Update config with the valid port
			if err := config.Save(); err != nil {
				logger.Warn("Failed to save newly connected serial port to config: %v", err)
			}
			logger.Info("Successfully opened serial port: %s", resolvedName)

			// Send a connected event if the status changed from disconnected.
			if lastSentStatus == events.Disconnected {
				// Use a non-blocking send. If the channel is full or no one is listening,
				// this will not block the serial manager. This is important at startup.
				select {
				case events.ComPortStatusChan <- events.Connected:
					lastSentStatus = events.Connected
				default: // Do nothing if the channel is not ready.
				}

				// TRIGGER CONFIG SYNC
				// Run sequentially in a single goroutine to avoid command storms
				// on systems with slower USB stacks.
				go func() {
					time.Sleep(2 * time.Second)
					FetchFirmwareVersion()
					time.Sleep(1 * time.Second)
					SyncFirmwareConfig()
				}()
			}
		}
	} else {
		logger.Info("reconnect called with empty port name. Connection remains closed.")
	}
}

// handleDisconnect closes the port and sets it to nil. MUST be called within a portMutex lock.
func handleDisconnect() {
	consecutiveReadFailures = 0
	if sv241Port != nil {
		// Send a disconnected event if the status changed from connected.
		if lastSentStatus == events.Connected {
			// Use a non-blocking send.
			select {
			case events.ComPortStatusChan <- events.Disconnected:
				lastSentStatus = events.Disconnected
			default: // Do nothing if the channel is not ready.
			}
		}
		sv241Port.Close()
		sv241Port = nil
	} else {
		lastSentStatus = events.Disconnected
	}
}

// ReleasePort closes the serial port to allow external tools (e.g., web flasher) to access it.
// It also pauses auto-reconnect until ResumeReconnect is called.
func ReleasePort() error {
	portMutex.Lock()
	defer portMutex.Unlock()

	reconnectPaused = true
	logger.Info("ReleasePort: Auto-reconnect paused.")

	if sv241Port == nil {
		logger.Info("ReleasePort: Port is already closed.")
		return nil
	}

	logger.Info("ReleasePort: Closing serial port for external access...")
	handleDisconnect()
	logger.Info("ReleasePort: Serial port closed successfully.")
	return nil
}

// ResumeReconnect allows the connection manager to auto-reconnect again.
func ResumeReconnect() {
	portMutex.Lock()
	defer portMutex.Unlock()
	reconnectPaused = false
	logger.Info("ResumeReconnect: Auto-reconnect resumed.")
}

// IsReconnectPaused returns true if auto-reconnect is paused (e.g., for firmware flashing).
func IsReconnectPaused() bool {
	portMutex.Lock()
	defer portMutex.Unlock()
	return reconnectPaused
}

// --- Cache Management ---

func periodicCacheUpdater(initDone chan struct{}) {
	logger.Info("Periodic cache update task started. Waiting for initial signal...")
	<-initDone
	logger.Info("Initial signal received. Starting cache updates.")

	for {
		performCacheUpdate()
		time.Sleep(5 * time.Second)
	}
}

func performCacheUpdate() {
	logger.Debug("Performing on-demand cache update.")
	statusJSON, err := SendCommand(`{"get":"status"}`, false, 0)
	if err == nil {
		updateStatusCacheFromJSON(statusJSON)
	} else {
		logger.Warn("Failed to get status for cache update: %v", err)
	}

	conditionsJSON, err := SendCommand(`{"get":"sensors"}`, false, 0)
	if err == nil {
		updateConditionsCacheFromJSON(conditionsJSON)
	} else {
		logger.Warn("Failed to get conditions for cache update: %v", err)
	}
}

func updateStatusCacheFromJSON(statusJSON string) {
	var rootData map[string]interface{}
	// Unmarshal into generic map because we have mixed types ("status" object, "dm" array)
	if json.Unmarshal([]byte(statusJSON), &rootData) == nil {
		// Extract "status" block
		if statusMap, ok := rootData["status"].(map[string]interface{}); ok {
			Status.Lock()
			defer Status.Unlock()

			// Inject "dm" (Dew Mode) array into the status map so handlers can find it easily
			if dmVal, found := rootData["dm"]; found {
				statusMap["dm"] = dmVal
			} else {
				// Important: 'set' command responses don't include 'dm', but we need it for the UI.
				// Preserve the existing 'dm' from the cache if available.
				if Status.Data != nil {
					if existingDM, ok := Status.Data["dm"]; ok {
						statusMap["dm"] = existingDM
					}
				}
			}

			Status.Data = statusMap
			logger.Debug("Successfully updated status cache.")

			// Sync ActiveVoltageTarget from firmware report if available
			if adjVal, ok := Status.Data["adj"]; ok {
				if adjFloat, ok := adjVal.(float64); ok && adjFloat > 0 {
					VoltageMutex.Lock()
					ActiveVoltageTarget = adjFloat
					VoltageMutex.Unlock()
				}
			}
		} else {
			logger.Warn("Status JSON missing 'status' object")
		}
	} else {
		logger.Warn("Failed to unmarshal status JSON from device. Raw data: %s", statusJSON)
	}
}

func updateConditionsCacheFromJSON(conditionsJSON string) {
	var conditionsData map[string]interface{}
	if err := json.Unmarshal([]byte(conditionsJSON), &conditionsData); err == nil {
		Conditions.Lock()
		defer Conditions.Unlock()
		Conditions.Data = conditionsData
		Conditions.LastUpdate = time.Now()
		logMemoryStatus(conditionsData)
		logger.Debug("Successfully updated conditions cache.")
	} else {
		logger.Warn("Failed to unmarshal conditions JSON from device. Raw data: %s", conditionsJSON)
	}
}

func FetchFirmwareVersion() {
	// This function is now called as a goroutine after the main loops have started.
	// We wait a moment to ensure the connection is stable and other tasks are running.
	time.Sleep(3 * time.Second)

	logger.Info("Requesting firmware version from device...")
	resp, err := SendCommand(`{"get":"version"}`, false, 0)
	if err != nil {
		logger.Warn("Could not get firmware version: %v", err)
		return
	}

	var versionResponse struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(resp), &versionResponse); err != nil {
		logger.Warn("Could not parse firmware version response: %v", err)
		return
	}
	firmwareVersionMu.Lock()
	firmwareVersion = versionResponse.Version
	firmwareVersionMu.Unlock()
	logger.Info("Firmware version: %s", versionResponse.Version)
}

func logMemoryStatus(data map[string]interface{}) {
	getFloat := func(key string) float64 {
		if val, ok := data[key]; ok {
			if fVal, ok := val.(float64); ok {
				return fVal
			}
		}
		return 0
	}

	currentHeapFree := getFloat("hf")
	currentHeapMinFree := getFloat("hmf")
	currentHeapMaxAlloc := getFloat("hma")
	currentHeapSize := getFloat("hs")

	valuesChanged := currentHeapFree != lastLoggedHeapFree ||
		currentHeapMinFree != lastLoggedHeapMinFree ||
		currentHeapMaxAlloc != lastLoggedHeapMaxAlloc ||
		currentHeapSize != lastLoggedHeapSize

	timeForcedLog := time.Since(lastMemoryLogTime) > 2*time.Minute

	if valuesChanged || timeForcedLog {
		logger.Debug("ESP32 Heap Status: Size=%.0f, Free=%.0f, MinFree=%.0f, MaxAlloc=%.0f",
			currentHeapSize, currentHeapFree, currentHeapMinFree, currentHeapMaxAlloc)

		lastLoggedHeapFree = currentHeapFree
		lastLoggedHeapMinFree = currentHeapMinFree
		lastLoggedHeapMaxAlloc = currentHeapMaxAlloc
		lastLoggedHeapSize = currentHeapSize
		lastMemoryLogTime = time.Now()
	}
}
