package alpaca

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
	"sv241pro-alpaca-proxy/internal/weather"
	"sync/atomic"
	"time"
)

// --- Management Handlers ---

// AlpacaDescription defines the structure for the management/v1/description endpoint.
type AlpacaDescription struct {
	ServerName          string `json:"ServerName"`
	Manufacturer        string `json:"Manufacturer"`
	ManufacturerVersion string `json:"ManufacturerVersion"`
	Location            string `json:"Location"`
}

// AlpacaConfiguredDevice defines the structure for a single device in the management/v1/configureddevices endpoint.
type AlpacaConfiguredDevice struct {
	DeviceName   string `json:"DeviceName"`
	DeviceType   string `json:"DeviceType"`
	DeviceNumber int    `json:"DeviceNumber"`
	UniqueID     string `json:"UniqueID"`
}

// API holds all dependencies for the Alpaca API handlers.
type API struct {
	appVersion string
	// driverConnected is read and written from different request goroutines (PUT vs GET
	// .../connected, and every handler that gates on it) - atomic.Bool instead of a plain bool
	// avoids a data race across them. Zero value is already false, so NewAPI doesn't need to set
	// it explicitly.
	driverConnected atomic.Bool
}

// NewAPI creates a new API instance.
func NewAPI(appVersion string) *API {
	return &API{
		appVersion: appVersion,
	}
}

func (a *API) HandleManagementDescription(w http.ResponseWriter, r *http.Request) {
	description := AlpacaDescription{
		ServerName:          "SV241 Alpaca Proxy",
		Manufacturer:        "User-Made",
		ManufacturerVersion: a.appVersion,
		Location:            "My Observatory",
	}
	ManagementValueResponse(w, r, description)
}

// HandleManagementConfiguredDevices is static and doesn't need the API struct receiver.
func HandleManagementConfiguredDevices(w http.ResponseWriter, r *http.Request) {
	devices := []AlpacaConfiguredDevice{
		{
			DeviceName:   "SV241 Power Switch",
			DeviceType:   "Switch",
			DeviceNumber: 0,
			UniqueID:     "a7f5a59c-f5d3-47f5-a59c-f5d347f5a59c", // Static GUID
		},
		{
			DeviceName:   "SV241 Environment",
			DeviceType:   "ObservingConditions",
			DeviceNumber: 0,
			UniqueID:     "b8g6b69d-g6e4-58g6-b69d-g6e458g6b69d", // Static GUID
		},
	}
	ManagementValueResponse(w, r, devices)
}

// HandleManagementApiVersions is static and doesn't need the API struct receiver.
func HandleManagementApiVersions(w http.ResponseWriter, r *http.Request) {
	// This endpoint doesn't use the standard alpaca handler.
	response := struct {
		Value               []int  `json:"Value"`
		ClientTransactionID uint32 `json:"ClientTransactionID"`
		ServerTransactionID uint32 `json:"ServerTransactionID"`
		ErrorNumber         int    `json:"ErrorNumber"`
		ErrorMessage        string `json:"ErrorMessage"`
	}{
		Value: []int{1},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- Common Device Handlers ---

func (a *API) HandleDeviceDescription(w http.ResponseWriter, r *http.Request) {
	StringResponse(w, r, "SV241 Pro Proxy Driver")
}

func (a *API) HandleDriverInfo(w http.ResponseWriter, r *http.Request) {
	StringResponse(w, r, "A Go-based ASCOM Alpaca proxy driver for the SV241 Pro.")
}

func (a *API) HandleDriverVersion(w http.ResponseWriter, r *http.Request) {
	StringResponse(w, r, a.appVersion)
}

func (a *API) HandleInterfaceVersion(w http.ResponseWriter, r *http.Request) {
	IntResponse(w, r, 1) // Switch and ObsCond are both Interface Version 1
}

func (a *API) HandleConnected(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		connectedStr, ok := GetFormValueIgnoreCase(r, "Connected")
		if !ok {
			ErrorResponse(w, r, http.StatusOK, 0x400, "Missing Connected parameter for PUT request")
			return
		}
		connected, err := strconv.ParseBool(connectedStr)
		if err != nil {
			ErrorResponse(w, r, http.StatusOK, 0x400, fmt.Sprintf("Invalid value for Connected: '%s'", connectedStr))
			return
		}
		// Update our internal connection state
		a.driverConnected.Store(connected)
		// If connecting, we verify hardware is actually there
		if connected && !serial.IsConnected() {
			// But ASCOM says we should try to connect or error if we can't.
			// Reconnect is already handled in background, but if it's currently down, we error.
			ErrorResponse(w, r, http.StatusOK, 0x40B, "SV241 device not connected. Please check the USB connection.")
			return
		}
		EmptyResponse(w, r)
		return
	}
	// For GET, report the internal connection status.
	// Many clients depend on this being true only if hardware is also alive.
	BoolResponse(w, r, a.driverConnected.Load() && serial.IsConnected())
}

func (a *API) HandleDeviceName(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		StringResponse(w, r, name)
	}
}

func (a *API) HandleSupportedActions(w http.ResponseWriter, r *http.Request) {
	StringListResponse(w, r, []string{"getlenstemperature"})
}

func (a *API) HandleObsCondAction(w http.ResponseWriter, r *http.Request) {
	action, ok := GetFormValueIgnoreCase(r, "Action")
	if !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Missing Action parameter")
		return
	}

	if strings.ToLower(action) == "getlenstemperature" {
		serial.Conditions.RLock()
		defer serial.Conditions.RUnlock()
		if val, ok := serial.Conditions.Data["t_lens"]; ok && val != nil {
			StringResponse(w, r, fmt.Sprintf("%v", val))
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
		}
		return
	}

	ErrorResponse(w, r, http.StatusOK, 0x400, fmt.Sprintf("Action '%s' is not supported.", action))
}

// --- Switch Handlers ---

func (a *API) HandleSwitchMaxSwitch(w http.ResponseWriter, r *http.Request) {
	count := config.GetSwitchMapLength()
	IntResponse(w, r, count)
}

func (a *API) HandleSwitchGetSwitchName(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		internalName, _ := config.GetSwitchIDMapEntry(id)

		// Sensor switches have fixed human-readable names
		switch internalName {
		case config.SensorVoltageKey:
			StringResponse(w, r, "Input Voltage")
			return
		case config.SensorCurrentKey:
			StringResponse(w, r, "Total Current")
			return
		case config.SensorPowerKey:
			StringResponse(w, r, "Total Power")
			return
		case config.SensorLensTempKey:
			if name := config.Get().LensTempName; name != "" {
				StringResponse(w, r, name)
			} else {
				StringResponse(w, r, "Lens Temperature")
			}
			return
		case config.SensorPWM1Key:
			if name := config.GetSwitchName("pwm1"); name != "" {
				StringResponse(w, r, name)
			} else {
				StringResponse(w, r, "Dew Heater 1")
			}
			return
		case config.SensorPWM2Key:
			if name := config.GetSwitchName("pwm2"); name != "" {
				StringResponse(w, r, name)
			} else {
				StringResponse(w, r, "Dew Heater 2")
			}
			return
		}

		customName := config.GetSwitchName(internalName)
		if customName != "" {
			StringResponse(w, r, customName)
		} else {
			StringResponse(w, r, internalName)
		}
	}
}

func (a *API) HandleSwitchGetSwitchDescription(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		internalName, _ := config.GetSwitchIDMapEntry(id)

		// Sensor switches have descriptive text with units
		switch internalName {
		case config.SensorVoltageKey:
			StringResponse(w, r, "Input voltage in Volts (V)")
			return
		case config.SensorCurrentKey:
			StringResponse(w, r, "Total current draw in Amperes (A)")
			return
		case config.SensorPowerKey:
			StringResponse(w, r, "Total power consumption in Watts (W)")
			return
		case config.SensorLensTempKey:
			StringResponse(w, r, "Temperature in °C")
			return
		case config.SensorPWM1Key:
			StringResponse(w, r, "PWM 1 power output in %")
			return
		case config.SensorPWM2Key:
			StringResponse(w, r, "PWM 2 power output in %")
			return
		}

		StringResponse(w, r, internalName)
	}
}

func (a *API) HandleSwitchGetSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}

	key, _ := config.GetSwitchIDMapEntry(id)

	// Sensors always return true (they are "on" when device is connected)
	if config.IsSensorSwitch(key) {
		BoolResponse(w, r, true)
		return
	}

	shortKey, _ := config.GetShortSwitchKeyByIDEntry(id)
	serial.Status.RLock()
	defer serial.Status.RUnlock()

	if shortKey == "all" {
		allOn := true
		// Loop through all defined switches (except the master itself and sensors) - a snapshot
		// copy, not the live package map, so this doesn't race SyncFirmwareConfig reassigning it
		// concurrently.
		for _, key := range config.GetShortSwitchKeyByIDSnapshot() {
			if key == "all" {
				continue
			}
			// Skip sensor keys - they are not in Status.Data
			if config.IsSensorSwitch(key) {
				continue
			}
			if val, ok := serial.Status.Data[key]; ok {
				// Handle both float64 (active value) and bool (false=off)
				isOn := false
				if boolVal, isBool := val.(bool); isBool {
					if boolVal {
						isOn = true
					}
				} else if floatVal, isFloat := val.(float64); isFloat {
					if floatVal >= 1.0 {
						isOn = true
					}
				}

				if !isOn {
					allOn = false
					break
				}
			} else {
				// If a switch status is missing, we can't be sure, but let's assume OFF for safety.
				allOn = false
				break
			}
		}
		BoolResponse(w, r, allOn)
		return
	}

	if val, ok := serial.Status.Data[shortKey]; ok {
		// Safe type assertion - handle both float64 and bool
		isOn := false
		if floatVal, isFloat := val.(float64); isFloat {
			isOn = floatVal >= 1.0
		} else if boolVal, isBool := val.(bool); isBool {
			isOn = boolVal
		}
		BoolResponse(w, r, isOn)
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Could not read switch status from cache")
	}
}

func (a *API) HandleSwitchGetSwitchValue(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}

	key, _ := config.GetSwitchIDMapEntry(id)

	// Handle sensor switches
	if config.IsSensorSwitch(key) {
		// All sensors (Voltage, Current, Power, LensTemp, PWM) live in Conditions cache (Telemetry)
		// PWM in Status (e.g. "pwm1": false) is just the enabled state, not the duty cycle.
		serial.Conditions.RLock()
		defer serial.Conditions.RUnlock()

		var dataKey string
		switch key {
		case config.SensorVoltageKey:
			dataKey = "v"
		case config.SensorCurrentKey:
			dataKey = "i"
		case config.SensorPowerKey:
			dataKey = "p"
		case config.SensorLensTempKey:
			dataKey = "t_lens"
		case config.SensorPWM1Key:
			dataKey = "pwm1"
		case config.SensorPWM2Key:
			dataKey = "pwm2"
		}

		// Handle Lens Temp specifically to inject fallback check
		if key == config.SensorLensTempKey {
			if val, found := serial.Conditions.Data["t_lens"]; found && val != nil {
				if floatVal, isFloat := val.(float64); isFloat {
					FloatResponse(w, r, floatVal)
					return
				}
			}
			// Sensor Missing/Error
			FloatResponse(w, r, -273.15)
			return
		}

		if val, found := serial.Conditions.Data[dataKey]; found && val != nil {
			if floatVal, isFloat := val.(float64); isFloat {
				// Current is in mA, convert to A
				if key == config.SensorCurrentKey {
					floatVal = floatVal / 1000.0
				}
				// Round to 2 decimal places for consistency with WebUI
				floatVal = math.Round(floatVal*100) / 100
				FloatResponse(w, r, floatVal)
				return
			}
		}
		FloatResponse(w, r, 0.0)
		return
	}

	shortKey, _ := config.GetShortSwitchKeyByIDEntry(id)
	serial.Status.RLock()
	defer serial.Status.RUnlock()

	if shortKey == "all" {
		allOn := true
		for _, key := range config.GetShortSwitchKeyByIDSnapshot() {
			if key == "all" {
				continue
			}
			// Skip sensor keys - they are not in Status.Data
			if config.IsSensorSwitch(key) {
				continue
			}
			if val, ok := serial.Status.Data[key]; ok {
				// Handle both float64 (active value) and bool (false=off)
				isOn := false
				if boolVal, isBool := val.(bool); isBool {
					if boolVal {
						isOn = true
					}
				} else if floatVal, isFloat := val.(float64); isFloat {
					if floatVal >= 1.0 {
						isOn = true
					}
				}

				if !isOn {
					allOn = false
					break
				}
			} else {
				allOn = false
				break
			}
		}
		var switchValue float64
		if allOn {
			switchValue = 1.0
		}
		FloatResponse(w, r, switchValue)
		return
	}

	if val, ok := serial.Status.Data[shortKey]; ok {
		var switchValue float64
		// Special handling for Adjustable Voltage if enabled
		if shortKey == "adj" && config.Get().EnableAlpacaVoltageControl {
			// Check if the device reports the output is actually OFF (boolean false)
			// Firmware reports boolean 'false' for OFF, and float voltage for ON.
			if boolVal, isBool := val.(bool); isBool && !boolVal {
				switchValue = 0.0 // Device is OFF
			} else {
				// Device is ON. Return cached target to reflect intended voltage.
				serial.VoltageMutex.RLock()
				target := serial.ActiveVoltageTarget
				serial.VoltageMutex.RUnlock()

				if target >= 0 {
					switchValue = target
				} else {
					// Fallback: trust the reported status value if target is unknown
					if v, ok := val.(float64); ok {
						switchValue = v
					} else {
						switchValue = 0.0
					}
				}
			}
		} else {
			// Standard Logic (or Voltage Control Disabled)
			// Check for PWM Manual Mode to allow > 1.0
			isManualPWM := false
			if shortKey == "pwm1" || shortKey == "pwm2" {
				heaterIdx := 0
				if shortKey == "pwm2" {
					heaterIdx = 1
				}

				// Note: We're already inside serial.Status.RLock() from line 239,
				// so we can access Data directly without another lock
				dmVal, found := serial.Status.Data["dm"]

				if found {
					if dmArray, ok := dmVal.([]interface{}); ok && heaterIdx < len(dmArray) {
						modeFloat, isFloat := dmArray[heaterIdx].(float64)
						if isFloat && int(modeFloat) == 0 {
							isManualPWM = true
						}
					}
				}
			}

			// Handle potential Boolean or Float values
			if v, isFloat := val.(float64); isFloat {
				if isManualPWM {
					switchValue = v // Return full value (e.g. 75.0)
				} else {
					if v >= 1.0 {
						switchValue = 1.0 // Clamp to binary for Auto/Standard
					}
				}
			} else if b, isBool := val.(bool); isBool && b {
				switchValue = 1.0
			}
		}
		FloatResponse(w, r, switchValue)
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Could not read switch value from cache")
	}
}

func (a *API) HandleSwitchSetSwitchValue(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}

	// Sensors are read-only - cannot be set
	key, _ := config.GetSwitchIDMapEntry(id)
	if config.IsSensorSwitch(key) {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Sensor switches are read-only and cannot be set")
		return
	}

	var state bool
	var err error
	if valueStr, ok := GetFormValueIgnoreCase(r, "Value"); ok {
		// Normalize: allows usage of "12,5" instead of "12.5"
		valueStr = strings.Replace(valueStr, ",", ".", -1)
		value, err := strconv.ParseFloat(valueStr, 64)
		// strconv.ParseFloat accepts "NaN"/"Inf" as valid floats (err == nil) - reject them
		// explicitly rather than letting a non-finite value flow into a command meant to drive a
		// physical output.
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			ErrorResponse(w, r, http.StatusOK, 400, "Invalid Value parameter")
			return
		}
		state = (value >= 1.0)
	} else if stateStr, ok := GetFormValueIgnoreCase(r, "State"); ok {
		state, err = strconv.ParseBool(stateStr)
		if err != nil {
			ErrorResponse(w, r, http.StatusOK, 400, "Invalid State parameter")
			return
		}
	} else {
		ErrorResponse(w, r, http.StatusOK, 400, "Missing Value or State parameter")
		return
	}

	longKey, _ := config.GetSwitchIDMapEntry(id)
	shortKey := config.ShortSwitchIDMap[longKey] // static map, never reassigned - no race

	// Special handling for Adjustable Voltage (ID 7) if enabled
	var command string
	var newVoltageTarget float64 = -1.0

	// Special handling for PWM if in Manual Mode (Lightweight check)
	heaterIdx := -1
	if longKey == "pwm1" {
		heaterIdx = 0
	} else if longKey == "pwm2" {
		heaterIdx = 1
	}

	// Track if we should send a manual PWM command (replaces goto pattern)
	sendManualPWMCommand := false

	if heaterIdx >= 0 {

		// Check Mode from Status Cache
		isAuto := false
		serial.Status.RLock()
		dmVal, found := serial.Status.Data["dm"]
		serial.Status.RUnlock()

		if found {
			if dmArray, ok := dmVal.([]interface{}); ok && heaterIdx < len(dmArray) {
				modeFloat, isFloat := dmArray[heaterIdx].(float64)
				if isFloat {
					if int(modeFloat) != 0 {
						isAuto = true
					}
				}
			}
		}

		// Use Manual PWM Command Logic if:
		// 1. Explicit Value provided (User wants to set a specific power).
		//    BUT: Value=0 should NOT be treated as explicit - it means "turn off"!
		// 2. State Toggle AND we are NOT in Auto Mode.
		// note: Turning OFF (!state) in Auto Mode should fall through to standard "false" command.
		forceManualValue, _ := GetFormValueIgnoreCase(r, "Value")
		hasExplicitValue := false
		if forceManualValue != "" {
			// Parse the value to check if it's > 0 (actual power setpoint)
			valFloat, err := strconv.ParseFloat(strings.Replace(forceManualValue, ",", ".", -1), 64)
			if err == nil && valFloat > 0 {
				hasExplicitValue = true
			}
		}
		useManualLogic := (heaterIdx >= 0) && (hasExplicitValue || !isAuto)

		if useManualLogic {
			if valueStr, ok := GetFormValueIgnoreCase(r, "Value"); ok {
				valueStr = strings.Replace(valueStr, ",", ".", -1)
				value, _ := strconv.ParseFloat(valueStr, 64)
				command = fmt.Sprintf(`{"set":{"%s":%.0f}}`, shortKey, value)
			} else {
				// Restore-on-Toggle Logic for Manual Mode:
				if state {
					// Turning ON (state=true) in Manual Mode
					// Use Smart Restore to recover last saved power level
					command = restorePowerState(shortKey, heaterIdx, state)
				} else {
					// Turning OFF in Manual Mode
					// Send "false" to disable.
					command = fmt.Sprintf(`{"set":{"%s":false}}`, shortKey)
				}
			}
			sendManualPWMCommand = true
		}
	}

	// Build command if not already set by manual PWM handler
	if !sendManualPWMCommand {
		// Special handling for Adjustable Voltage
		if longKey == "adj_conv" && config.Get().EnableAlpacaVoltageControl {
			if valueStr, ok := GetFormValueIgnoreCase(r, "Value"); ok {
				// If Value is provided, set specific voltage
				originalStr := valueStr
				valueStr = strings.Replace(valueStr, ",", ".", -1)
				logger.Debug("SetSwitchValue (AdjConv) - Received: '%s', Normalized: '%s'", originalStr, valueStr)
				value, parseErr := strconv.ParseFloat(valueStr, 64)
				// See the identical check above - ParseFloat accepts "NaN"/"Inf" without error,
				// which would otherwise reach the firmware and bypass its voltage clamps (NaN
				// compares false against every bound check).
				if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					ErrorResponse(w, r, http.StatusOK, 400, "Invalid Value parameter")
					return
				}
				command = fmt.Sprintf(`{"set":{"%s":%.2f}}`, shortKey, value)
				newVoltageTarget = value
			} else {
				// Use "true"/"false" for bool to avoid ambiguity with "1"=1V in firmware
				command = fmt.Sprintf(`{"set":{"%s":%t}}`, shortKey, state)
			}
		} else if longKey == "master_power" {
			// Master Power Handling:
			// If turning ON, we must intelligently restore PWM values to > 0 to prevent NINA timeouts.
			if state {
				logger.Info("Master Power ON: Triggering Smart Restore for PWM heaters...")

				// Global Enable first (synchronous)
				serial.SendCommand(`{"set":{"all":1}}`, true, 0)

				// Now force-restore values for PWM heaters using smart restore logic. Fetch the
				// firmware config once and reuse it for both heaters, instead of restorePowerState
				// fetching it twice in a row - halves the number of blocking serial round trips
				// this request makes (see fetchFirmwareConfig's doc comment).
				// We don't need to check errors here, we just fire and forget
				fullConfig, err := fetchFirmwareConfig()
				if err != nil {
					logger.Warn("Master Power ON: Could not get/parse firmware config for smart restore: %v", err)
					fullConfig = map[string]interface{}{}
				}
				cmd1 := restorePowerStateFromConfig("pwm1", fullConfig, 0)
				serial.SendCommand(cmd1, true, 0)

				cmd2 := restorePowerStateFromConfig("pwm2", fullConfig, 1)
				serial.SendCommand(cmd2, true, 0)

				// Respond success immediately (the commands are queued)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"status":"Master Power ON sequence initiated"}`)
				return
			} else {
				// Turning OFF -> Standard all:0
				command = `{"set":{"all":0}}`
			}
		} else {
			// Standard Logic (Auto Modes or Generic Switches)
			// Use "true"/"false" for bool to avoid ambiguity with "1"=1V in firmware
			command = fmt.Sprintf(`{"set":{"%s":%t}}`, shortKey, state)
		}
	}

	_, err = serial.SendCommand(command, true, 0)
	if err != nil {
		ErrorResponse(w, r, http.StatusInternalServerError, http.StatusInternalServerError, fmt.Sprintf("Failed to send command: %v", err))
		return
	}

	// Update the Voltage Target Cache if this was a voltage change command
	if newVoltageTarget >= 0 {
		serial.VoltageMutex.Lock()
		serial.ActiveVoltageTarget = newVoltageTarget
		serial.VoltageMutex.Unlock()
	}

	// Parse response which can contain mixed types ("status" object and "dm" array)
	// This ensures we capture any immediate status updates from the command,
	// particularly for the "Turbo" update mechanism in serial.go which might run before this.
	// But we must NOT prevent standard polling from working.

	// We don't send the raw firmware response to the client.
	// Alpaca expects a standard envelope.
	EmptyResponse(w, r)

	// Handle auto-enable/disable logic in a goroutine
	go handleHeaterInteractions(id, state)

}

// restorePowerState determines the best command to enable a heater using the firmware configuration (mp).
func restorePowerState(shortKey string, heaterIdx int, state bool) string {
	// Simple Logic: Always use the firmware's configured "Manual Power" (mp) setting.
	// This matches the simplified user requirement: On = Set to Configured Value.
	savedVal := getSavedManualPower(heaterIdx)

	logger.Info("Smart Restore (%s): Restoring power to %.0f%% (Firmware Config).", shortKey, savedVal)
	return fmt.Sprintf(`{"set":{"%s":%.0f}}`, shortKey, savedVal)
}

// restorePowerStateFromConfig is restorePowerState, but takes an already-fetched firmware config
// instead of fetching its own - see fetchFirmwareConfig's doc comment for why this matters when
// restoring more than one heater in the same request (Master Power ON).
func restorePowerStateFromConfig(shortKey string, fullConfig map[string]interface{}, heaterIdx int) string {
	savedVal := getSavedManualPowerFromConfig(fullConfig, heaterIdx)
	logger.Info("Smart Restore (%s): Restoring power to %.0f%% (Firmware Config).", shortKey, savedVal)
	return fmt.Sprintf(`{"set":{"%s":%.0f}}`, shortKey, savedVal)
}

// fetchFirmwareConfig fetches and parses the full firmware config in one blocking round trip.
// Callers that need the "mp" value for more than one heater (e.g. Master Power ON, which restores
// both pwm1 and pwm2) should call this once and pass the result to getSavedManualPowerFromConfig
// for each heater, rather than each going through getSavedManualPower separately - that would
// fetch and parse the identical config twice in a row for no reason, adding a second blocking
// serial round trip (up to the full command timeout) to an already-slow request.
func fetchFirmwareConfig() (map[string]interface{}, error) {
	configJSON, err := serial.SendCommand(`{"get":"config"}`, false, 0)
	if err != nil {
		return nil, err
	}
	var fullConfig map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &fullConfig); err != nil {
		return nil, err
	}
	return fullConfig, nil
}

// getSavedManualPowerFromConfig extracts a heater's configured "Manual Power" (mp) value from an
// already-fetched firmware config - see fetchFirmwareConfig.
func getSavedManualPowerFromConfig(fullConfig map[string]interface{}, heaterIdx int) float64 {
	if dhRaw, ok := fullConfig["dh"]; ok {
		if dhArray, ok := dhRaw.([]interface{}); ok && heaterIdx < len(dhArray) {
			if heaterMap, ok := dhArray[heaterIdx].(map[string]interface{}); ok {
				if mpVal, found := heaterMap["mp"]; found {
					if mpFloat, isFloat := mpVal.(float64); isFloat {
						return mpFloat
					}
				}
			}
		}
	}
	return 0
}

func getSavedManualPower(heaterIdx int) float64 {
	// Attempt to read the full config to find the 'mp' value for this heater.
	// This is a blocking call, but necessary to ensure we restore the correct value.
	fullConfig, err := fetchFirmwareConfig()
	if err != nil {
		logger.Warn("RestoreToggle: Could not get/parse firmware config: %v", err)
		return 0
	}
	return getSavedManualPowerFromConfig(fullConfig, heaterIdx)
}

func (a *API) HandleSwitchSetSwitchName(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}

	internalName, _ := config.GetSwitchIDMapEntry(id)

	// Sensors have fixed names and cannot be renamed
	if config.IsSensorSwitch(internalName) {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Sensor switches have fixed names and cannot be renamed")
		return
	}

	newName, ok := GetFormValueIgnoreCase(r, "Name")
	if !ok {
		ErrorResponse(w, r, http.StatusBadRequest, http.StatusBadRequest, "Missing Name parameter")
		return
	}
	config.SetSwitchName(internalName, newName)
	logger.Info("Set custom name for switch %d ('%s') to '%s'", id, internalName, newName)

	if err := config.Save(); err != nil {
		logger.Error("Failed to save proxy config after setting switch name: %v", err)
		ErrorResponse(w, r, http.StatusInternalServerError, http.StatusInternalServerError, "Failed to save configuration")
		return
	}
	EmptyResponse(w, r)
}

func (a *API) HandleSwitchCanWrite(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		key, _ := config.GetSwitchIDMapEntry(id)
		// Sensors are read-only
		if config.IsSensorSwitch(key) {
			BoolResponse(w, r, false)
			return
		}
		BoolResponse(w, r, true)
	}
}

func (a *API) HandleSwitchMaxSwitchValue(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		key, _ := config.GetSwitchIDMapEntry(id)
		// Debug logging for troubleshooting slider issue
		logger.Debug("MaxSwitchValue: ID=%d Key=%s", id, key)

		// Sensor max values
		switch key {
		case config.SensorVoltageKey:
			FloatResponse(w, r, 15.0) // Max voltage
			return
		case config.SensorCurrentKey:
			FloatResponse(w, r, 10.0) // Max current in A
			return
		case config.SensorPowerKey:
			FloatResponse(w, r, 150.0) // Max power in W
			return
		case config.SensorLensTempKey:
			FloatResponse(w, r, 100.0) // Max temp
			return
		case config.SensorPWM1Key, config.SensorPWM2Key:
			FloatResponse(w, r, 100.0) // Max PWM %
			return
		}

		if key == "adj_conv" && config.Get().EnableAlpacaVoltageControl {
			FloatResponse(w, r, 15.0)
			return
		}

		// Lightweight PWM limit based on Dew Mode
		// Status contains "dm": [mode1, mode2]
		heaterIdx := -1
		if key == "pwm1" {
			heaterIdx = 0
		} else if key == "pwm2" {
			heaterIdx = 1
		}

		if heaterIdx >= 0 {
			serial.Status.RLock()
			dmVal, found := serial.Status.Data["dm"]
			serial.Status.RUnlock()

			if found {
				if dmArray, ok := dmVal.([]interface{}); ok && heaterIdx < len(dmArray) {
					// JSON numbers come as float64 usually
					modeFloat, isFloat := dmArray[heaterIdx].(float64)
					if isFloat && int(modeFloat) == 0 { // 0 = Manual
						FloatResponse(w, r, 100.0)
						return
					}
				}
			}
		}

		FloatResponse(w, r, 1.0)
	}
}

func (a *API) HandleSwitchMinSwitchValue(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		key, _ := config.GetSwitchIDMapEntry(id)
		if key == config.SensorLensTempKey {
			FloatResponse(w, r, -273.15) // Absolute zero as min/error
			return
		}
		FloatResponse(w, r, 0.0)
	}
}

func (a *API) HandleSwitchSwitchStep(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		key, _ := config.GetSwitchIDMapEntry(id)

		// Sensors have 0.1 step for precision
		if config.IsSensorSwitch(key) {
			FloatResponse(w, r, 0.1)
			return
		}

		if key == "adj_conv" && config.Get().EnableAlpacaVoltageControl {
			FloatResponse(w, r, 0.1)
			return
		}
		FloatResponse(w, r, 1.0)
	}
}

func (a *API) HandleSwitchSupportedActions(w http.ResponseWriter, r *http.Request) {
	actions := []string{"MasterSwitchOn", "MasterSwitchOff"}
	StringListResponse(w, r, actions)
}

func (a *API) HandleSwitchAction(w http.ResponseWriter, r *http.Request) {
	action, ok := GetFormValueIgnoreCase(r, "Action")
	if !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Missing Action parameter")
		return
	}

	switch strings.ToLower(action) {
	case "masterswitchon", "masterswitchoff":
		state := strings.ToLower(action) == "masterswitchon"
		logger.Info("Executing ASCOM Action: %s", action)
		StringResponse(w, r, "") // Respond immediately with empty string value per ASCOM spec
		go func() {
			stateInt := 0
			if state {
				stateInt = 1
			}
			command := fmt.Sprintf(`{"set":{"all":%d}}`, stateInt)
			serial.SendCommand(command, true, 0)
		}()
		return
	default:
		ErrorResponse(w, r, http.StatusOK, 0x400, fmt.Sprintf("Action '%s' is not supported.", action))
		return
	}
}

// --- ObservingConditions Handlers ---

// HandleObsCondValue returns a handler for the 9 ObservingConditions properties that are a plain
// float value backed by getWeatherValue (temperature, humidity, dewpoint, pressure, windspeed,
// winddirection, windgust, cloudcover, rainrate) - these used to be 9 separate functions with an
// identical body differing only in the metric/hwKey pair passed to getWeatherValue. hwKey is
// resolved once from metricHardwareKeys and captured in the closure, so callers only ever pass the
// metric name (the same string already used as the route's map key in server.go).
func (a *API) HandleObsCondValue(metric string) http.HandlerFunc {
	hwKey := metricHardwareKeys[metric] // "" for the 6 internet-only metrics (Go zero-value)
	return func(w http.ResponseWriter, r *http.Request) {
		if val, impl, err := a.getWeatherValue(metric, hwKey); impl {
			if err == nil {
				FloatResponse(w, r, val)
			} else {
				ErrorResponse(w, r, http.StatusOK, 0x40B, err.Error())
			}
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented")
		}
	}
}

func (a *API) HandleObsCondNotImplemented(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented by this driver.")
}

func (a *API) HandleObsCondAveragePeriod(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		avgPeriodStr, ok := GetFormValueIgnoreCase(r, "AveragePeriod")
		if !ok {
			ErrorResponse(w, r, http.StatusOK, 0x400, "Missing required parameter 'AveragePeriod'.")
			return
		}
		avg, err := strconv.ParseFloat(avgPeriodStr, 64)
		if err != nil {
			ErrorResponse(w, r, http.StatusOK, 0x401, fmt.Sprintf("Invalid value '%s' for AveragePeriod.", avgPeriodStr))
			return
		}
		if avg < -1.0 {
			// ASCOM requires error 0x401 for values out of range
			ErrorResponse(w, r, http.StatusOK, 0x401, "AveragePeriod must be >= -1.0")
			return
		}
		EmptyResponse(w, r)
		return
	}
	FloatResponse(w, r, 0)
}

func (a *API) getGlobalLatestUpdate() time.Time {
	var latest time.Time

	// Check Hardware update time
	serial.Conditions.RLock()
	if !serial.Conditions.LastUpdate.IsZero() {
		latest = serial.Conditions.LastUpdate
	}
	serial.Conditions.RUnlock()

	// Check Weather Service update time
	if data := weather.GetService().GetData(); data != nil {
		if data.Timestamp.After(latest) {
			latest = data.Timestamp
		}
	}

	return latest
}

func (a *API) HandleObsCondLatestUpdateTime(w http.ResponseWriter, r *http.Request) {
	latest := a.getGlobalLatestUpdate()

	if latest.IsZero() {
		// If no data ever arrived, use current time but return early if never connected
		if !a.driverConnected.Load() {
			ErrorResponse(w, r, http.StatusOK, 0x40B, "Driver not connected")
			return
		}
		latest = time.Now()
	}

	// Convert to ASCOM DATE (serial date) for ASCOM.Double
	// Days since 30.12.1899 00:00:00
	// Unix epoch (1970) is 25569 days after ASCOM epoch.
	// Use time.Sub for maximum sub-second precision.
	baseDate := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	daysSinceEpoch := float64(latest.Sub(baseDate).Nanoseconds()) / float64(24*time.Hour)

	RawFloatResponse(w, r, daysSinceEpoch)
}

func (a *API) HandleObsCondSensorDescription(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		ErrorResponse(w, r, http.StatusMethodNotAllowed, 0x405, "Method PUT not allowed for sensordescription.")
		return
	}
	sensorName, ok := GetFormValueIgnoreCase(r, "SensorName")
	if !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Missing required parameter 'SensorName'.")
		return
	}

	metric := strings.ToLower(sensorName)
	hwKey := metricHardwareKeys[metric]

	if a.isMetricImplemented(metric, hwKey) {
		priority := config.GetWeatherSourcePriority(metric)
		if priority == "" {
			priority = "hybrid"
		}
		StringResponse(w, r, fmt.Sprintf("Source: %s", priority))
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented by this driver.")
	}
}

func (a *API) HandleObsCondTimeSinceLastUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		ErrorResponse(w, r, http.StatusMethodNotAllowed, 0x405, "Method PUT not allowed for timesincelastupdate.")
		return
	}

	// ASCOM spec: If SensorName is empty string, return time since most recent update of ANY sensor.
	sensorName, ok := GetFormValueIgnoreCase(r, "SensorName")
	if !ok {
		// If parameter is completely missing, Alpaca clients often interpret this as empty string
		// for properties that support it.
		sensorName = ""
	}

	if sensorName == "" {
		latest := a.getGlobalLatestUpdate()
		if latest.IsZero() {
			FloatResponse(w, r, 0)
			return
		}
		FloatResponse(w, r, time.Since(latest).Seconds())
		return
	}

	metric := strings.ToLower(sensorName)
	hwKey := metricHardwareKeys[metric]

	if a.isMetricImplemented(metric, hwKey) {
		// Treat as real-time for now (hot cache)
		FloatResponse(w, r, 0)
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x40C, fmt.Sprintf("Sensor '%s' is not implemented.", sensorName))
	}
}

func (a *API) HandleObsCondRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		ErrorResponse(w, r, http.StatusMethodNotAllowed, 0x405, "Method "+r.Method+" not allowed for refresh.")
		return
	}
	EmptyResponse(w, r)
}

// --- Helper Logic ---

// metricHardwareKeys maps a metric name to its firmware hardware key. Metrics not present here
// resolve to "" (Go's zero value for a missing map key), meaning "no hardware source" - intentional
// for the 6 internet-only metrics (pressure, windspeed, winddirection, windgust, cloudcover,
// rainrate), not an omission. Single source of truth for this mapping, used by HandleObsCondValue,
// HandleObsCondSensorDescription, and HandleObsCondTimeSinceLastUpdate - previously duplicated
// (implicitly or via an inline switch) in all three places.
var metricHardwareKeys = map[string]string{
	"temperature": "t_amb",
	"humidity":    "h_amb",
	"dewpoint":    "d",
}

// internetSupportedMetrics defines which metrics can be sourced from the internet (Open-Meteo).
var internetSupportedMetrics = map[string]bool{
	"temperature":   true,
	"humidity":      true,
	"dewpoint":      true,
	"pressure":      true,
	"windspeed":     true,
	"winddirection": true,
	"windgust":      true,
	"cloudcover":    true,
	"rainrate":      true,
}

func (a *API) isMetricImplemented(metric string, hwKey string) bool {
	conf := config.Get()
	priority := config.GetWeatherSourcePriority(metric)
	if priority == "" {
		priority = "hybrid"
	}

	// 1. Hardware implementation
	if (priority == "hybrid" || priority == "hardware") && hwKey != "" {
		return true
	}

	// 2. Internet implementation (mapped to supported Open-Meteo metrics)
	if (priority == "hybrid" || priority == "internet") && conf.EnableWeatherService {
		if internetSupportedMetrics[metric] {
			return true
		}
	}
	return false
}

func (a *API) getWeatherValue(metric string, hwKey string) (float64, bool, error) {
	if !a.isMetricImplemented(metric, hwKey) {
		return 0, false, fmt.Errorf("property not implemented")
	}

	conf := config.Get()
	priority := config.GetWeatherSourcePriority(metric)
	if priority == "" {
		priority = "hybrid"
	}

	// 1. Try Hardware if priority is 'hybrid' or 'hardware'
	if priority == "hybrid" || priority == "hardware" {
		if hwKey != "" {
			serial.Conditions.RLock()
			val, ok := serial.Conditions.Data[hwKey]
			serial.Conditions.RUnlock()
			if ok && val != nil {
				if floatVal, isFloat := val.(float64); isFloat {
					return floatVal, true, nil
				}
			}
		}
		// If hardware-only
		if priority == "hardware" {
			return 0, true, fmt.Errorf("hardware sensor missing/initialising")
		}
	}

	// 2. Try Internet (Open-Meteo) if priority is 'hybrid' or 'internet'
	if priority == "hybrid" || priority == "internet" {
		if !conf.EnableWeatherService {
			return 0, true, fmt.Errorf("weather service disabled")
		}

		data := weather.GetService().GetData()
		// Cache validity check: Double the interval as grace period
		if data != nil && time.Since(data.Timestamp) < time.Duration(conf.WeatherInterval*2+1)*time.Minute {
			switch metric {
			case "temperature":
				return data.Temperature, true, nil
			case "humidity":
				return data.Humidity, true, nil
			case "dewpoint":
				return data.DewPoint, true, nil
			case "pressure":
				return data.Pressure, true, nil
			case "windspeed":
				return data.WindSpeed, true, nil
			case "winddirection":
				return data.WindDir, true, nil
			case "windgust":
				return data.WindGust, true, nil
			case "cloudcover":
				return data.CloudCover, true, nil
			case "rainrate":
				return data.Precipitation, true, nil
			}
		}
	}

	return 0, true, fmt.Errorf("data not available (initialising)")
}

func handleHeaterInteractions(id int, state bool) {
	// Runs in its own goroutine (see the `go handleHeaterInteractions(...)` call site) with no
	// caller to recover a panic for it - an unrecovered panic in any goroutine takes down the
	// whole process, not just this one heater toggle. Belt-and-suspenders against exactly that,
	// on top of the explicit length check below.
	defer func() {
		if r := recover(); r != nil {
			logger.Error("handleHeaterInteractions panicked (recovered): %v", r)
		}
	}()

	// This logic checks for heater inter-dependencies (PID leader/follower).
	key, _ := config.GetSwitchIDMapEntry(id)
	if key != "pwm1" && key != "pwm2" {
		return // Not a heater
	}

	configJSON, err := serial.SendCommand(`{"get":"config"}`, false, 0)
	if err != nil {
		logger.Warn("HeaterInteraction: Could not get firmware config: %v", err)
		return
	}
	var fwConfig struct {
		DH []struct {
			M int `json:"m"` // Mode
		} `json:"dh"`
	}
	if err := json.Unmarshal([]byte(configJSON), &fwConfig); err != nil {
		logger.Warn("HeaterInteraction: Could not parse firmware config: %v", err)
		return
	}
	// fwConfig.DH is indexed by heater index (0/1) below in both branches - a response that's
	// valid JSON but doesn't carry a full 2-element "dh" array (e.g. an error object from the
	// firmware, or a version mismatch) would otherwise panic here. Mirrors the same guard
	// SyncFirmwareConfig (internal/serial/serial_sync.go) already has for the identical shape.
	if len(fwConfig.DH) < 2 {
		logger.Warn("HeaterInteraction: firmware config response had %d heater(s), expected 2 - skipping.", len(fwConfig.DH))
		return
	}

	if state { // Logic for turning a heater ON
		followerHeaterIndex := 0
		if key == "pwm2" {
			followerHeaterIndex = 1
		}

		followerKey := key
		if !config.GetHeaterAutoEnableLeader(followerKey) {
			logger.Debug("Auto-enable leader is disabled for %s. Skipping.", followerKey)
			return
		}

		leaderHeaterIndex := 1 - followerHeaterIndex
		isFollower := fwConfig.DH[followerHeaterIndex].M == 3 // 3 = PID-Sync (Follower)
		leaderMode := fwConfig.DH[leaderHeaterIndex].M
		isLeaderValid := leaderMode == 1 || leaderMode == 4 // 1 = PID, 4 = MinTemp
		if isFollower && isLeaderValid {
			// Determine Leader Key
			leaderLongKey := "pwm1"
			if leaderHeaterIndex == 1 {
				leaderLongKey = "pwm2"
			}

			logger.Info("Activating Leader (%s) for Follower (%s).", leaderLongKey, followerKey)
			leaderShortKey := config.ShortSwitchIDMap[leaderLongKey]
			leaderCommand := fmt.Sprintf(`{"set":{"%s":true}}`, leaderShortKey)
			responseJSON, err := serial.SendCommand(leaderCommand, true, 0)
			if err != nil {
				logger.Error("HeaterInteraction: Failed to send enable command to Leader (%s): %v", leaderLongKey, err)
			} else {
				// Update Cache
				var rootData map[string]interface{}
				if json.Unmarshal([]byte(responseJSON), &rootData) == nil {
					if statusMap, ok := rootData["status"].(map[string]interface{}); ok {
						serial.Status.Lock()
						if dmVal, found := rootData["dm"]; found {
							statusMap["dm"] = dmVal
						} else {
							if existingDM, exists := serial.Status.Data["dm"]; exists {
								statusMap["dm"] = existingDM
							}
						}
						serial.Status.Data = statusMap
						serial.Status.Unlock()
						logger.Info("HeaterInteraction: Successfully activated Leader (%s).", leaderLongKey)
					}
				}
			}
		}
	} else { // Logic for turning a heater OFF
		// If a PID Leader is turned OFF, disable its Follower if needed

		leaderHeaterIndex := 0
		if key == "pwm2" {
			leaderHeaterIndex = 1
		}

		leaderLongKey := key // The heater being turned off is potentially a leader

		followerHeaterIndex := 1 - leaderHeaterIndex
		leaderMode := fwConfig.DH[leaderHeaterIndex].M
		isLeaderValid := leaderMode == 1 || leaderMode == 4 // 1 = PID, 4 = MinTemp
		isFollower := fwConfig.DH[followerHeaterIndex].M == 3

		if isLeaderValid && isFollower {
			followerLongKey := "pwm1"
			if followerHeaterIndex == 1 {
				followerLongKey = "pwm2"
			}

			logger.Info("Deactivating PID Follower (%s) because Leader (%s) was turned off.", followerLongKey, leaderLongKey)
			followerShortKey := config.ShortSwitchIDMap[followerLongKey]
			followerCommand := fmt.Sprintf(`{"set":{"%s":false}}`, followerShortKey)
			responseJSON, err := serial.SendCommand(followerCommand, true, 0)
			if err != nil {
				logger.Error("HeaterInteraction: Failed to send disable command to Follower (%s): %v", followerLongKey, err)
			} else {
				// Update Cache with response to ensure UI reflects the change immediately
				var rootData map[string]interface{}
				if json.Unmarshal([]byte(responseJSON), &rootData) == nil {
					if statusMap, ok := rootData["status"].(map[string]interface{}); ok {
						serial.Status.Lock()
						if dmVal, found := rootData["dm"]; found {
							statusMap["dm"] = dmVal
						} else {
							// Preserve existing DM
							if existingDM, exists := serial.Status.Data["dm"]; exists {
								statusMap["dm"] = existingDM
							}
						}
						serial.Status.Data = statusMap
						serial.Status.Unlock()
						logger.Info("HeaterInteraction: Successfully deactivated Follower (%s).", followerLongKey)
					}
				}
			}
		}
	}
}
