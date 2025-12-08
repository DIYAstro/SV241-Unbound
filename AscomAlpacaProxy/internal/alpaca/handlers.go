package alpaca

import (
	"encoding/json"
	"fmt"
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
		if _, err := strconv.ParseBool(connectedStr); err != nil {
			ErrorResponse(w, r, http.StatusOK, 0x400, fmt.Sprintf("Invalid value for Connected: '%s'", connectedStr))
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
	IntResponse(w, r, len(config.SwitchIDMap))
}

func (a *API) HandleSwitchGetSwitchName(w http.ResponseWriter, r *http.Request) {
	if id, ok := ParseSwitchID(w, r); ok {
		internalName := config.SwitchIDMap[id]
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
		StringResponse(w, r, internalName)
	}
}

func (a *API) HandleSwitchGetSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
		return
	}
	shortKey := config.ShortSwitchKeyByID[id]
	serial.Status.RLock()
	defer serial.Status.RUnlock()

	if shortKey == "all" {
		allOn := true
		// Loop through all defined switches (except the master itself)
		for _, key := range config.ShortSwitchKeyByID {
			if key == "all" {
				continue
			}
			if val, ok := serial.Status.Data[key]; ok {
				if val.(float64) < 1.0 {
					allOn = false
					break
				}
			} else {
				// If a switch status is missing, we can't be sure, but let's assume OFF for safety,
				// or just ignore it. Assuming OFF implies Master is OFF.
				allOn = false
				break
			}
		}
		BoolResponse(w, r, allOn)
		return
	}

	if val, ok := serial.Status.Data[shortKey]; ok {
		BoolResponse(w, r, val.(float64) >= 1.0)
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Could not read switch status from cache")
	}
}

func (a *API) HandleSwitchGetSwitchValue(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseSwitchID(w, r)
	if !ok {
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
			if val, ok := serial.Status.Data[key]; ok {
				if val.(float64) < 1.0 {
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
		if val.(float64) >= 1.0 {
			switchValue = 1.0
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

	var state bool
	var err error
	if valueStr, ok := GetFormValueIgnoreCase(r, "Value"); ok {
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
	stateInt := 0
	if state {
		stateInt = 1
	}
	command := fmt.Sprintf(`{"set":{"%s":%d}}`, shortKey, stateInt)
	responseJSON, err := serial.SendCommand(command, true, 0)
	if err != nil {
		ErrorResponse(w, r, http.StatusInternalServerError, http.StatusInternalServerError, fmt.Sprintf("Failed to send command: %v", err))
		return
	}

	var statusData map[string]map[string]interface{}
	if json.Unmarshal([]byte(responseJSON), &statusData) == nil {
		serial.Status.Lock()
		serial.Status.Data = statusData["status"]
		serial.Status.Unlock()
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
	newName, ok := GetFormValueIgnoreCase(r, "Name")
	if !ok {
		ErrorResponse(w, r, http.StatusBadRequest, http.StatusBadRequest, "Missing Name parameter")
		return
	}

	internalName := config.SwitchIDMap[id]
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
	if _, ok := ParseSwitchID(w, r); ok {
		BoolResponse(w, r, true)
	}
}

func (a *API) HandleSwitchMaxSwitchValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := ParseSwitchID(w, r); ok {
		FloatResponse(w, r, 1.0)
	}
}

func (a *API) HandleSwitchMinSwitchValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := ParseSwitchID(w, r); ok {
		FloatResponse(w, r, 0.0)
	}
}

func (a *API) HandleSwitchSwitchStep(w http.ResponseWriter, r *http.Request) {
	if _, ok := ParseSwitchID(w, r); ok {
		FloatResponse(w, r, 1.0)
	}
}

func (a *API) HandleSwitchSupportedActions(w http.ResponseWriter, r *http.Request) {
	actions := []string{"getvoltage", "getcurrent", "getpower", "MasterSwitchOn", "MasterSwitchOff"}
	StringListResponse(w, r, actions)
}

func (a *API) HandleSwitchAction(w http.ResponseWriter, r *http.Request) {
	action, ok := GetFormValueIgnoreCase(r, "Action")
	if !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Missing Action parameter")
		return
	}

	var valueStr string
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
	case "getvoltage":
		serial.Conditions.RLock()
		defer serial.Conditions.RUnlock()
		if value, found := serial.Conditions.Data["v"]; found && value != nil {
			valueStr = fmt.Sprintf("%v", value)
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
			return
		}
	case "getpower":
		serial.Conditions.RLock()
		defer serial.Conditions.RUnlock()
		if value, found := serial.Conditions.Data["p"]; found && value != nil {
			valueStr = fmt.Sprintf("%v", value)
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
			return
		}
	case "getcurrent":
		serial.Conditions.RLock()
		defer serial.Conditions.RUnlock()
		if value, found := serial.Conditions.Data["i"]; found && value != nil {
			if currentMA, ok := value.(float64); ok {
				valueStr = fmt.Sprintf("%.3f", currentMA/1000.0)
			} else {
				ErrorResponse(w, r, http.StatusOK, 0x401, "Invalid data type for current in cache.")
				return
			}
		} else {
			ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
			return
		}
	default:
		ErrorResponse(w, r, http.StatusOK, 0x400, fmt.Sprintf("Action '%s' is not supported.", action))
		return
	}
	StringResponse(w, r, valueStr)
}

// --- ObservingConditions Handlers ---

func (a *API) HandleObsCondTemperature(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["t_amb"]; ok && val != nil {
		FloatResponse(w, r, val.(float64))
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
	}
}

func (a *API) HandleObsCondHumidity(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["h_amb"]; ok && val != nil {
		FloatResponse(w, r, val.(float64))
	} else {
		ErrorResponse(w, r, http.StatusOK, 0x401, "Sensor not available or failed to read.")
	}
}

func (a *API) HandleObsCondDewPoint(w http.ResponseWriter, r *http.Request) {
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if val, ok := serial.Conditions.Data["d"]; ok && val != nil {
		FloatResponse(w, r, val.(float64))
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
	if id != 8 && id != 9 {
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
		followerHeaterIndex := id - 8
		followerKey := fmt.Sprintf("pwm%d", followerHeaterIndex+1)
		if !config.Get().HeaterAutoEnableLeader[followerKey] {
			logger.Debug("Auto-enable leader is disabled for %s. Skipping.", followerKey)
			return
		}

		leaderHeaterIndex := 1 - followerHeaterIndex
		isFollower := fwConfig.DH[followerHeaterIndex].M == 3 // 3 = PID-Sync (Follower)
		isLeaderPID := fwConfig.DH[leaderHeaterIndex].M == 1  // 1 = PID
		if isFollower && isLeaderPID {
			leaderAscomId := leaderHeaterIndex + 8
			logger.Info("Activating PID Leader (ID %d) for Follower (ID %d).", leaderAscomId, id)
			leaderShortKey := config.ShortSwitchKeyByID[leaderAscomId]
			leaderCommand := fmt.Sprintf(`{"set":{"%s":1}}`, leaderShortKey)
			serial.SendCommand(leaderCommand, true, 0)
		}
	} else { // Logic for turning a heater OFF
		leaderHeaterIndex := id - 8
		followerHeaterIndex := 1 - leaderHeaterIndex
		isLeaderPID := fwConfig.DH[leaderHeaterIndex].M == 1
		isFollower := fwConfig.DH[followerHeaterIndex].M == 3
		if isLeaderPID && isFollower {
			followerAscomId := followerHeaterIndex + 8
			logger.Info("Deactivating PID Follower (ID %d) because Leader (ID %d) was turned off.", followerAscomId, id)
			followerShortKey := config.ShortSwitchKeyByID[followerAscomId]
			followerCommand := fmt.Sprintf(`{"set":{"%s":0}}`, followerShortKey)
			serial.SendCommand(followerCommand, true, 0)
		}
	}
}
