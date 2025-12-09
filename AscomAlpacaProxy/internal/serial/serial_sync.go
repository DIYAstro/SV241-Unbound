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

	// 1. Standard Switches (Indices 0-7)
	// These are always present (unless we want to hide unused DC ports later, but for now they are static)
	standardSwitches := []string{"dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"}
	standardShortKeys := []string{"d1", "d2", "d3", "d4", "d5", "u12", "u34", "adj"}

	currentID := 0

	for i, name := range standardSwitches {
		newIDMap[currentID] = name
		newShortKeyByID[currentID] = standardShortKeys[i]
		currentID++
	}

	// 2. Dew Heaters (Dynamic)
	for i, heater := range fwConfig.DH {
		if heater.M != 5 { // Not Disabled
			internalName := fmt.Sprintf("pwm%d", i+1)
			shortKey := fmt.Sprintf("pwm%d", i+1)

			newIDMap[currentID] = internalName
			newShortKeyByID[currentID] = shortKey
			currentID++
		} else {
			logger.Info("Heater PWM%d is DISABLED in firmware. Hiding it from ASCOM Switch list.", i+1)
		}
	}

	// 3. Master Power (Always Last)
	newIDMap[currentID] = "master_power"
	newShortKeyByID[currentID] = "all"
	// currentID++ // No need to increment further

	// Update Global Config
	// Warning: This is not thread-safe if heavily accessed, but we assume this runs at connection time.
	config.SwitchIDMap = newIDMap
	config.ShortSwitchKeyByID = newShortKeyByID

	logger.Info("Switch configuration sync complete. Total Switches: %d", len(config.SwitchIDMap))
}

func resetSwitchMaps() {
	// Not strictly needed if we fully rebuild, but good for fallback
	// We'll leave it empty or doing nothing as SyncFirmwareConfig rebuilds from scratch.
	// Actually, let's just make it do nothing or reset to full default if called manually.
}
