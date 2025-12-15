package serial

import (
	"encoding/json"
	"fmt"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"time"
)

// SyncFirmwareConfig fetches the firmware configuration and updates the proxy's internal switch list
// to hide any heaters that are set to "Disabled" mode (Mode 5).
func SyncFirmwareConfig() {
	// Wait a moment for the connection to stabilize and the mutex to be released
	time.Sleep(1 * time.Second)

	logger.Info("Syncing switch configuration with firmware...")

	response, err := SendCommand(`{"get":"config"}`, false, 5*time.Second)
	if err != nil {
		logger.Error("Failed to sync firmware config: %v", err)
		return
	}

	var fwConfig struct {
		DH []struct {
			M int `json:"m"` // Mode
		} `json:"dh"`
		PS config.PowerStartupStates `json:"ps"` // Power Startup States
	}

	if err := json.Unmarshal([]byte(response), &fwConfig); err != nil {
		logger.Error("Failed to parse firmware config for sync: %v", err)
		return
	}

	// Rebuild maps contiguously
	newIDMap := make(map[int]string)
	newShortKeyByID := make(map[int]string)
	// ShortSwitchIDMap (string->string) doesn't need re-indexing, but we should remove disabled keys from it?
	// Actually ShortSwitchIDMap maps "dc1" -> "d1". If we don't expose "pwm1", we probably shouldn't effectively remove it from here?
	// It's used for reverse lookup. If the ID doesn't exist, it won't be used.
	// But let's keep it safe.

	// 0. Sensors (Always at fixed IDs 0, 1, 2)
	newIDMap[0] = config.SensorVoltageKey
	newIDMap[1] = config.SensorCurrentKey
	newIDMap[2] = config.SensorPowerKey
	newShortKeyByID[0] = config.SensorVoltageKey
	newShortKeyByID[1] = config.SensorCurrentKey
	newShortKeyByID[2] = config.SensorPowerKey

	// 1. Standard Switches (Starting at ID 3)
	// These are always present (unless we want to hide unused DC ports later, but for now they are static)
	standardSwitches := []string{"dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"}
	standardShortKeys := []string{"d1", "d2", "d3", "d4", "d5", "u12", "u34", "adj"}

	currentID := 3 // Start after sensors

	for i, name := range standardSwitches {
		shortKey := standardShortKeys[i]
		var state int
		switch shortKey {
		case "d1":
			state = fwConfig.PS.DC1
		case "d2":
			state = fwConfig.PS.DC2
		case "d3":
			state = fwConfig.PS.DC3
		case "d4":
			state = fwConfig.PS.DC4
		case "d5":
			state = fwConfig.PS.DC5
		case "u12":
			state = fwConfig.PS.USBC12
		case "u34":
			state = fwConfig.PS.USB345
		case "adj":
			state = fwConfig.PS.AdjConv
		}

		// If switch is Disabled (State 2), skip it
		if state == 2 {
			continue
		}

		newIDMap[currentID] = name
		newShortKeyByID[currentID] = shortKey
		currentID++
	}

	// 2. Dew Heaters
	for i := range fwConfig.DH {
		// If heater is Disabled (Mode 5), skip it to hide from ASCOM
		if fwConfig.DH[i].M == 5 {
			continue
		}

		internalName := fmt.Sprintf("pwm%d", i+1)
		shortKey := fmt.Sprintf("pwm%d", i+1)

		newIDMap[currentID] = internalName
		newShortKeyByID[currentID] = shortKey
		currentID++
	}

	// 3. Master Power (Always Last)
	if config.Get().EnableMasterPower {
		newIDMap[currentID] = "master_power"
		newShortKeyByID[currentID] = "all"
	}

	// Update Global Config with mutex protection
	// This ensures thread-safe access during concurrent web requests
	config.SwitchMapMutex.Lock()
	config.SwitchIDMap = newIDMap
	config.ShortSwitchKeyByID = newShortKeyByID
	config.SwitchMapMutex.Unlock()

	logger.Info("Switch configuration sync complete. Total Switches: %d", len(newIDMap))
}

func resetSwitchMaps() {
	// Not strictly needed if we fully rebuild, but good for fallback
	// We'll leave it empty or doing nothing as SyncFirmwareConfig rebuilds from scratch.
	// Actually, let's just make it do nothing or reset to full default if called manually.
}
