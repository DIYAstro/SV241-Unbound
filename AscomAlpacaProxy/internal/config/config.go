package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sv241pro-alpaca-proxy/internal/logger"
)

// ProxyConfig stores configuration specific to the Go proxy itself.
type ProxyConfig struct {
	SerialPortName         string            `json:"serialPortName"`
	AutoDetectPort         bool              `json:"autoDetectPort"`
	NetworkPort            int               `json:"networkPort"`
	LogLevel               string            `json:"logLevel"`
	SwitchNames            map[string]string `json:"switchNames"`
	HeaterAutoEnableLeader map[string]bool   `json:"heaterAutoEnableLeader"`
}

// CombinedConfig defines the structure for a full backup file.
type CombinedConfig struct {
	ProxyConfig    *ProxyConfig    `json:"proxyConfig"`
	FirmwareConfig json.RawMessage `json:"firmwareConfig"`
}

var (
	// Maps are public so other packages (like alpaca) can use them.
	SwitchIDMap = map[int]string{
		0: "dc1", 1: "dc2", 2: "dc3", 3: "dc4", 4: "dc5",
		5: "usbc12", 6: "usb345", 7: "adj_conv", 8: "pwm1", 9: "pwm2",
	}
	ShortSwitchIDMap = map[string]string{
		"dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
		"usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
	}
	ShortSwitchKeyByID = map[int]string{
		0: "d1", 1: "d2", 2: "d3", 3: "d4", 4: "d5",
		5: "u12", 6: "u34", 7: "adj", 8: "pwm1", 9: "pwm2",
	}

	proxyConfig     *ProxyConfig // Singleton instance
	proxyConfigFile string       // Full path to the config file
)

// init sets up the path to the configuration file.
func init() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// This is a critical failure at startup. We can't proceed without a config path.
		// Using log.Fatalf here is acceptable as it's a pre-flight check.
		logger.Fatal("FATAL: Could not get user config directory: %v", err)
	}
	appConfigDir := filepath.Join(configDir, "SV241AlpacaProxy")
	// The logger setup will create this dir, but it's safe to do it here too.
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		logger.Fatal("FATAL: Could not create application config directory '%s': %v", appConfigDir, err)
	}
	proxyConfigFile = filepath.Join(appConfigDir, "proxy_config.json")
}

// Load reads the configuration from the JSON file into the singleton instance.
// If the file doesn't exist, it initializes a default configuration and saves it.
func Load() error {
	file, err := os.ReadFile(proxyConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("Proxy config file '%s' not found. Using default settings.", proxyConfigFile)
			// Initialize with default values
			proxyConfig = &ProxyConfig{
				AutoDetectPort: true, // Standardmäßig ist der Autoscan an
				NetworkPort:    8080,
				LogLevel:       "INFO",
				SwitchNames:    make(map[string]string),
				HeaterAutoEnableLeader: map[string]bool{
					"pwm1": true,
					"pwm2": true,
				},
			}
			for _, internalName := range SwitchIDMap {
				proxyConfig.SwitchNames[internalName] = internalName
			}
			// Attempt to save the initial default config
			return Save() // File not found is not an error, just means defaults apply
		}
		return fmt.Errorf("failed to read proxy config file: %w", err)
	}

	// First, unmarshal into a temporary instance.
	var tempConfig ProxyConfig
	if err := json.Unmarshal(file, &tempConfig); err != nil {
		// Don't overwrite the global config if unmarshalling fails.
		return fmt.Errorf("failed to unmarshal proxy config: %w", err)
	}
	proxyConfig = &tempConfig

	// --- Validate and set defaults for missing fields ---
	if proxyConfig.NetworkPort == 0 {
		proxyConfig.NetworkPort = 8080
	}
	if proxyConfig.LogLevel == "" {
		logger.Warn("Configuration key 'LogLevel' not found, using default 'INFO'.")
		proxyConfig.LogLevel = "INFO"
	}
	if proxyConfig.SwitchNames == nil {
		proxyConfig.SwitchNames = make(map[string]string)
	}
	for _, internalName := range SwitchIDMap {
		if _, exists := proxyConfig.SwitchNames[internalName]; !exists {
			logger.Warn("Missing custom name for '%s', adding with default value.", internalName)
			proxyConfig.SwitchNames[internalName] = internalName
		}
	}
	if proxyConfig.HeaterAutoEnableLeader == nil {
		proxyConfig.HeaterAutoEnableLeader = make(map[string]bool)
	}
	if _, exists := proxyConfig.HeaterAutoEnableLeader["pwm1"]; !exists {
		logger.Warn("Missing auto-enable setting for 'pwm1', adding with default 'true'.")
		proxyConfig.HeaterAutoEnableLeader["pwm1"] = true
	}
	if _, exists := proxyConfig.HeaterAutoEnableLeader["pwm2"]; !exists {
		logger.Warn("Missing auto-enable setting for 'pwm2', adding with default 'true'.")
		proxyConfig.HeaterAutoEnableLeader["pwm2"] = true
	}

	// Wenn das Feld in einer alten Konfigurationsdatei fehlt, setzen wir es auf true,
	// um das bisherige Verhalten beizubehalten.
	if !proxyConfig.AutoDetectPort && proxyConfig.SerialPortName == "" {
		proxyConfig.AutoDetectPort = true
	}

	// Apply the loaded log level immediately.
	logger.SetLevelFromString(proxyConfig.LogLevel)
	logger.Info("Loaded proxy config from '%s'", proxyConfigFile)
	return nil
}

// Save writes the current configuration to the JSON file.
func Save() error {
	if proxyConfig == nil {
		return fmt.Errorf("cannot save nil config")
	}
	logger.Debug("Attempting to save proxy config to file: %s", proxyConfigFile)
	data, err := json.MarshalIndent(proxyConfig, "", "  ")
	if err != nil {
		logger.Error("saveProxyConfig: failed to marshal proxy config: %v", err)
		return fmt.Errorf("failed to marshal proxy config: %w", err)
	}

	if err := os.WriteFile(proxyConfigFile, data, 0644); err != nil {
		logger.Error("saveProxyConfig: failed to write proxy config file '%s': %v", proxyConfigFile, err)
		return fmt.Errorf("failed to write proxy config file: %w", err)
	}
	logger.Info("Successfully saved proxy config to file '%s'", proxyConfigFile)
	return nil
}

// Get returns a pointer to the singleton ProxyConfig instance.
func Get() *ProxyConfig {
	if proxyConfig == nil {
		// This should not happen in the normal flow, as Load() is called on startup.
		// But as a safeguard, we initialize a default config.
		if err := Load(); err != nil {
			logger.Fatal("Failed to load configuration on demand: %v", err)
		}
	}
	return proxyConfig
}

// GetNetworkPortForDiscovery reads the network port directly from the file.
// This is a special case for the single-instance check, which runs before the main
// configuration and logging are initialized.
func GetNetworkPortForDiscovery() int {
	const defaultPort = 8080

	file, err := os.ReadFile(proxyConfigFile)
	if err != nil {
		return defaultPort // File not found or other error, use default.
	}

	var config struct {
		NetworkPort int `json:"networkPort"`
	}
	if err := json.Unmarshal(file, &config); err != nil {
		return defaultPort // JSON is corrupt, use default.
	}

	if config.NetworkPort == 0 {
		return defaultPort
	}

	return config.NetworkPort
}
