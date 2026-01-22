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
		// When client tries to connect, verify hardware is available
		if connected && !serial.IsConnected() {
			ErrorResponse(w, r, http.StatusOK, 0x400, "SV241 device not connected. Please check the USB connection.")
			return
		}
		// The connection is managed automatically, so we just acknowledge.
		EmptyResponse(w, r)
		return
	}
	// For GET, report the actual connection status.
	BoolResponse(w, r, serial.IsConnected())
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
	count := len(config.SwitchIDMap)
	IntResponse(w, r, count)
}

func (a *API) HandleSwitchGetSwitchName(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		internalName := config.SwitchIDMap[id]

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
			StringResponse(w, r, "Lens Temperature")
			return
		case config.SensorPWM1Key:
			if name := config.Get().SwitchNames["pwm1"]; name != "" {
				StringResponse(w, r, name)
			} else {
				StringResponse(w, r, "Dew Heater 1")
			}
			return
		case config.SensorPWM2Key:
			if name := config.Get().SwitchNames["pwm2"]; name != "" {
				StringResponse(w, r, name)
			} else {
				StringResponse(w, r, "Dew Heater 2")
			}
			return
		}

		customName := config.Get().SwitchNames[internalName]
		if customName != "" {
			StringResponse(w, r, customName)
		} else {
			StringResponse(w, r, internalName)
		}
	}
}

func (a *API) HandleSwitchGetSwitchDescription(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		internalName := config.SwitchIDMap[id]

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
			StringResponse(w, r, "Lens/Objective temperature in °C")
			return
		case config.SensorPWM1Key:
			StringResponse(w, r, "Dew Heater 1 power output in %")
			return
		case config.SensorPWM2Key:
			StringResponse(w, r, "Dew Heater 2 power output in %")
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

	key := config.SwitchIDMap[id]

	// Sensors always return true (they are "on" when device is connected)
	if config.IsSensorSwitch(key) {
		BoolResponse(w, r, true)
		return
	}

	shortKey := config.ShortSwitchKeyByID[id]
	serial.Status.RLock()
	defer serial.Status.RUnlock()

	if shortKey == "all" {
		allOn := true
		// Loop through all defined switches (except the master itself and sensors)
		for _, key := range config.ShortSwitchKeyByID {
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

	key := config.SwitchIDMap[id]

	// Handle sensor switches
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

	shortKey := config.ShortSwitchKeyByID[id]
	serial.Status.RLock()
	defer serial.Status.RUnlock()

	if shortKey == "all" {
		allOn := true
		for _, key := range config.ShortSwitchKeyByID {
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
	key := config.SwitchIDMap[id]
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
		if err != nil {
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

	longKey := config.SwitchIDMap[id]
	shortKey := config.ShortSwitchIDMap[longKey]

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
		isManual := false
		serial.Status.RLock()
		dmVal, found := serial.Status.Data["dm"]
		serial.Status.RUnlock()

		if found {
			if dmArray, ok := dmVal.([]interface{}); ok && heaterIdx < len(dmArray) {
				modeFloat, isFloat := dmArray[heaterIdx].(float64)
				if isFloat && int(modeFloat) == 0 {
					isManual = true
				}
			}
		}

		if isManual {
			if valueStr, ok := GetFormValueIgnoreCase(r, "Value"); ok {
				valueStr = strings.Replace(valueStr, ",", ".", -1)
				value, _ := strconv.ParseFloat(valueStr, 64)
				command = fmt.Sprintf(`{"set":{"%s":%.0f}}`, shortKey, value)
			} else {
				// Use "true"/"false" so firmware applies default manual power or ON state
				command = fmt.Sprintf(`{"set":{"%s":%t}}`, shortKey, state)
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
				value, _ := strconv.ParseFloat(valueStr, 64)
				command = fmt.Sprintf(`{"set":{"%s":%.2f}}`, shortKey, value)
				newVoltageTarget = value
			} else {
				// Use "true"/"false" for bool to avoid ambiguity with "1"=1V in firmware
				command = fmt.Sprintf(`{"set":{"%s":%t}}`, shortKey, state)
			}
		} else if longKey == "master_power" {
			// Master Power "all" command usually expects 0/1 in some firmware versions, matching Action handler
			stateInt := 0
			if state {
				stateInt = 1
			}
			command = fmt.Sprintf(`{"set":{"%s":%d}}`, shortKey, stateInt)
		} else {
			// Use "true"/"false" for bool to avoid ambiguity with "1"=1V in firmware
			command = fmt.Sprintf(`{"set":{"%s":%t}}`, shortKey, state)
		}
	}

	responseJSON, err := serial.SendCommand(command, true, 0)
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
	var rootData map[string]interface{}
	if json.Unmarshal([]byte(responseJSON), &rootData) == nil {
		if statusMap, ok := rootData["status"].(map[string]interface{}); ok {
			serial.Status.Lock()

			// Inject "dm" (Dew Mode) array into the status map so handlers can find it easily
			if dmVal, found := rootData["dm"]; found {
				statusMap["dm"] = dmVal
			} else {
				// If not found in response (e.g. from SET command), preserve existing DM from cache
				// We must do this before overwriting serial.Status.Data
				// ALREADY LOCKED via serial.Status.Lock() above, so we can access directly.
				if existingDM, exists := serial.Status.Data["dm"]; exists {
					statusMap["dm"] = existingDM
				}
			}

			serial.Status.Data = statusMap
			serial.Status.Unlock()
		} else {
			logger.Warn("Status JSON missing 'status' object after set command.")
		}
	} else {
		logger.Warn("Failed to unmarshal status JSON from device after set command. Raw data: %s", responseJSON)
	}

	// Handle auto-enable/disable logic in a goroutine
	go handleHeaterInteractions(id, state)

	EmptyResponse(w, r)
}

func (a *API) HandleSwitchSetSwitchName(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}

	internalName := config.SwitchIDMap[id]

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
	conf := config.Get()
	conf.SwitchNames[internalName] = newName
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
		key := config.SwitchIDMap[id]
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
		key := config.SwitchIDMap[id]
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
		key := config.SwitchIDMap[id]
		if key == config.SensorLensTempKey {
			FloatResponse(w, r, -273.15) // Absolute zero as min/error
			return
		}
		FloatResponse(w, r, 0.0)
	}
}

func (a *API) HandleSwitchSwitchStep(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		key := config.SwitchIDMap[id]

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

func (a *API) HandleObsCondTemperature(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["t_amb"]; ok && val != nil {
		if floatVal, isFloat := val.(float64); isFloat {
			FloatResponse(w, r, floatVal)
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Invalid data type for temperature in cache.")
		}
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
	}
}

func (a *API) HandleObsCondHumidity(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["h_amb"]; ok && val != nil {
		if floatVal, isFloat := val.(float64); isFloat {
			FloatResponse(w, r, floatVal)
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Invalid data type for humidity in cache.")
		}
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
	}
}

func (a *API) HandleObsCondDewPoint(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["d"]; ok && val != nil {
		if floatVal, isFloat := val.(float64); isFloat {
			FloatResponse(w, r, floatVal)
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Invalid data type for dew point in cache.")
		}
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
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
		if _, err := strconv.ParseFloat(avgPeriodStr, 64); err != nil {
			ErrorResponse(w, r, http.StatusOK, 0x401, fmt.Sprintf("Invalid value '%s' for AveragePeriod.", avgPeriodStr))
			return
		}
	}
	ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented by this driver.")
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
	switch strings.ToLower(sensorName) {
	case "temperature", "humidity", "dewpoint":
		ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented by this driver.")
	default:
		ErrorResponse(w, r, http.StatusOK, 0x401, fmt.Sprintf("Invalid SensorName: '%s'", sensorName))
	}
}

func (a *API) HandleObsCondTimeSinceLastUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		ErrorResponse(w, r, http.StatusMethodNotAllowed, 0x405, "Method PUT not allowed for timesincelastupdate.")
		return
	}
	sensorName, ok := GetFormValueIgnoreCase(r, "SensorName")
	if !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Missing required parameter 'SensorName'.")
		return
	}
	switch strings.ToLower(sensorName) {
	case "temperature", "humidity", "dewpoint":
		ErrorResponse(w, r, http.StatusOK, 0x40C, "Property not implemented by this driver.")
	default:
		ErrorResponse(w, r, http.StatusOK, 0x401, fmt.Sprintf("Invalid SensorName: '%s'", sensorName))
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

func handleHeaterInteractions(id int, state bool) {
	// This logic checks for heater inter-dependencies (PID leader/follower).
	key := config.SwitchIDMap[id]
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

	if state { // Logic for turning a heater ON
		followerHeaterIndex := 0
		if key == "pwm2" {
			followerHeaterIndex = 1
		}

		followerKey := key
		if !config.Get().HeaterAutoEnableLeader[followerKey] {
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
