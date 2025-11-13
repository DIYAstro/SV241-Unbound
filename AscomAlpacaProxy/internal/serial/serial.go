package serial

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/events"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// Command defines a command to be sent to the serial device.
type Command struct {
	Command  string
	Response chan<- string
	Error    chan<- error
}

// StatusCache stores the latest power status from the device.
type StatusCache struct {
	Data map[string]interface{}
	*sync.RWMutex
}

// ConditionsCache stores the latest sensor readings from the device.
type ConditionsCache struct {
	Data map[string]interface{}
	*sync.RWMutex
}

var (
	highPriorityCommands = make(chan Command)
	lowPriorityCommands  = make(chan Command)
	sv241Port            serial.Port
	portMutex            = &sync.Mutex{}
	firmwareVersion      = "unknown"

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
		reconnect(conf.SerialPortName)
		portMutex.Unlock()
	} else {
		logger.Info("Initial Connection: Starting auto-detection...")
		foundPort, err := FindPort()
		if err != nil {
			logger.Warn("Initial Connection: Auto-detection failed: %v", err)
		} else {
			logger.Info("Auto-detection found device on port %s. Connecting...", foundPort)
			portMutex.Lock()
			reconnect(foundPort)
			portMutex.Unlock()
		}
	}

	portMutex.Lock()
	if sv241Port != nil {
		logger.Info("Initial connection attempt finished successfully.")
	} else {
		logger.Warn("Initial connection attempt failed. The application will continue to try connecting in the background.")
	}
	portMutex.Unlock()

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

		logger.Debug("Processing command: %s", cmd.Command)
		_, err := sv241Port.Write([]byte(cmd.Command + "\n"))
		if err != nil {
			logger.Error("Serial write failed: %v. Marking port as disconnected.", err)
			handleDisconnect()
			portMutex.Unlock()
			cmd.Error <- fmt.Errorf("failed to write to serial port: %w", err)
			continue
		}

		sv241Port.SetReadTimeout(2 * time.Second)
		reader := bufio.NewReader(sv241Port)
		response, err := reader.ReadString('\n')
		if err != nil {
			logger.Error("Serial read failed: %v. Marking port as disconnected.", err)
			handleDisconnect()
			portMutex.Unlock()
			cmd.Error <- fmt.Errorf("failed to read from serial port: %w", err)
			continue
		}
		portMutex.Unlock()

		trimmedResponse := strings.TrimSpace(response)
		logger.Debug("Received response from device: %s", trimmedResponse)
		cmd.Response <- trimmedResponse
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
		isConnected := (sv241Port != nil)
		if !isConnected {
			logger.Info("Connection Manager: Device is disconnected. Attempting to connect...")
			conf := config.Get()
			targetPort := conf.SerialPortName
			autoDetect := conf.AutoDetectPort

			// Wenn Auto-Detect AUS ist, versuchen wir NUR den konfigurierten Port.
			if !autoDetect && targetPort != "" {
				logger.Info("Connection Manager: Trying configured port '%s' for reconnection.", targetPort)
				reconnect(targetPort)
			} else {
				// Wenn Auto-Detect AN ist (oder kein Port konfiguriert ist), verhalten wir uns wie bisher.
				if targetPort != "" {
					logger.Info("Connection Manager: Trying configured port '%s' for reconnection.", targetPort)
					reconnect(targetPort)
					if sv241Port == nil {
						logger.Warn("Connection Manager: Configured port '%s' failed. Falling back to auto-detection.", targetPort)
						conf.SerialPortName = "" // Leeren, damit der nächste Versuch den Autoscan nutzt
						config.Save()
					}
				}

				// Wenn immer noch nicht verbunden, starte den Autoscan.
				if sv241Port == nil {
					logger.Info("Connection Manager: Starting auto-detection...")
					foundPort, err := FindPort()
					if err != nil {
						logger.Warn("Connection Manager: Auto-detection failed: %v", err)
					} else {
						logger.Info("Connection Manager: Auto-detection found device on port %s. Connecting...", foundPort)
						reconnect(foundPort)
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
func FindPort() (string, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		logger.Warn("FindPort: enumerator.GetDetailedPortsList returned an error: %v.", err)
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports found on the system")
	}

	logger.Info("Found %d serial ports. Probing for SV241 device...", len(ports))
	for _, port := range ports {
		if port.IsUSB {
			logger.Info("Probing port: %s", port.Name)
			mode := &serial.Mode{BaudRate: 115200}
			p, err := serial.Open(port.Name, mode)
			if err != nil {
				logger.Warn("Could not open port %s to probe: %v", port.Name, err)
				continue
			}

			_, err = p.Write([]byte("{\"get\":\"sensors\"}\n"))
			if err != nil {
				p.Close()
				continue
			}

			p.SetReadTimeout(2 * time.Second)
			reader := bufio.NewReader(p)
			line, err := reader.ReadString('\n')
			p.Close() // Close the port immediately after use
			if err != nil {
				continue
			}

			var js json.RawMessage
			if json.Unmarshal([]byte(line), &js) == nil {
				logger.Info("Successfully probed port: %s", port.Name)
				return port.Name, nil
			}
		}
	}
	return "", errors.New("could not find SV241 device on any USB serial port")
}

// Reconnect is a public wrapper for reconnecting, intended to be called from other packages.
func Reconnect(portName string) {
	portMutex.Lock()
	defer portMutex.Unlock()
	reconnect(portName)
}

// reconnect attempts to close the current port and open a new one.
// It MUST be called within a portMutex lock.
func reconnect(newPortName string) {
	handleDisconnect() // Close existing port if any

	if newPortName != "" {
		logger.Info("Attempting to open serial port: %s", newPortName)
		mode := &serial.Mode{BaudRate: 115200}
		p, err := serial.Open(newPortName, mode)
		if err != nil {
			logger.Error("reconnect: Failed to open port %s: %v", newPortName, err)
		} else {
			sv241Port = p
			conf := config.Get()
			conf.SerialPortName = newPortName // Update config with the valid port
			if err := config.Save(); err != nil {
				logger.Warn("Failed to save newly connected serial port to config: %v", err)
			}
			logger.Info("Successfully opened serial port: %s", newPortName)

			// Send a connected event if the status changed from disconnected.
			if lastSentStatus == events.Disconnected {
				// Use a non-blocking send. If the channel is full or no one is listening,
				// this will not block the serial manager. This is important at startup.
				select {
				case events.ComPortStatusChan <- events.Connected:
					lastSentStatus = events.Connected
				default: // Do nothing if the channel is not ready.
				}
			}
		}
	} else {
		logger.Info("reconnect called with empty port name. Connection remains closed.")
	}
}

// handleDisconnect closes the port and sets it to nil. MUST be called within a portMutex lock.
func handleDisconnect() {
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

// --- Cache Management ---

func periodicCacheUpdater(initDone chan struct{}) {
	logger.Info("Periodic cache update task started. Waiting for initial signal...")
	<-initDone
	logger.Info("Initial signal received. Starting cache updates.")

	for {
		performCacheUpdate()
		time.Sleep(3 * time.Second)
	}
}

func performCacheUpdate() {
	logger.Debug("Performing on-demand cache update.")
	statusJSON, err := SendCommand(`{"get":"status"}`, false, 0)
	if err == nil {
		var statusData map[string]map[string]interface{}
		if json.Unmarshal([]byte(statusJSON), &statusData) == nil {
			logger.Debug("Successfully unmarshaled status cache data.")
			Status.Lock()
			Status.Data = statusData["status"]
			Status.Unlock()
		} else {
			logger.Warn("Failed to unmarshal status JSON from device. Raw data: %s", statusJSON)
		}
	} else {
		logger.Warn("Failed to get status for cache update: %v", err)
	}

	conditionsJSON, err := SendCommand(`{"get":"sensors"}`, false, 0)
	if err == nil {
		var conditionsData map[string]interface{}
		if err := json.Unmarshal([]byte(conditionsJSON), &conditionsData); err == nil {
			Conditions.Lock()
			Conditions.Data = conditionsData
			logMemoryStatus(conditionsData)
			Conditions.Unlock()
			logger.Debug("Successfully unmarshaled conditions cache data.")
		} else {
			logger.Warn("Failed to unmarshal conditions JSON from device. Raw data: %s", conditionsJSON)
		}
	} else {
		logger.Warn("Failed to get conditions for cache update: %v", err)
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
	firmwareVersion = versionResponse.Version
	logger.Info("Firmware version: %s", firmwareVersion)
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
