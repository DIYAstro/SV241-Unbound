package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync"
)

// ProxyConfig stores configuration specific to the Go proxy itself.
type ProxyConfig struct {
	SerialPortName             string            `json:"serialPortName"`
	AutoDetectPort             bool              `json:"autoDetectPort"`
	NetworkPort                int               `json:"networkPort"`
	ListenAddress              string            `json:"listenAddress"`
	LogLevel                   string            `json:"logLevel"`
	SwitchNames                map[string]string `json:"switchNames"`
	HeaterAutoEnableLeader     map[string]bool   `json:"heaterAutoEnableLeader"`
	HistoryRetentionNights     int               `json:"historyRetentionNights"`
	TelemetryInterval          int               `json:"telemetryInterval"`          // Seconds
	EnableAlpacaVoltageControl bool              `json:"enableAlpacaVoltageControl"` // Allow voltage control via Alpaca
	EnableMasterPower          bool              `json:"enableMasterPower"`          // Show Master Power switch
	FirstRunComplete           bool              `json:"firstRunComplete"`           // Onboarding wizard completed
}

// CombinedConfig defines the structure for a full backup file.
type CombinedConfig struct {
	ProxyConfig    *ProxyConfig    `json:"proxyConfig"`
	FirmwareConfig json.RawMessage `json:"firmwareConfig"`
}

// PowerStartupStates defines the startup state of standard switches.
// 0: Off, 1: On, 2: Disabled
type PowerStartupStates struct {
	DC1     int `json:"d1"`
	DC2     int `json:"d2"`
	DC3     int `json:"d3"`
	DC4     int `json:"d4"`
	DC5     int `json:"d5"`
	USBC12  int `json:"u12"`
	USB345  int `json:"u34"`
	AdjConv int `json:"adj"`
}

// SwitchMapMutex protects concurrent access to SwitchIDMap and ShortSwitchKeyByID.
var SwitchMapMutex sync.RWMutex

// Sensor switch keys - these are read-only sensors at fixed IDs 0, 1, 2
const (
	SensorVoltageKey = "sensor_voltage"
	SensorCurrentKey = "sensor_current"
	SensorPowerKey   = "sensor_power"
)

// IsSensorSwitch returns true if the switch key is a read-only sensor
func IsSensorSwitch(key string) bool {
	return key == SensorVoltageKey || key == SensorCurrentKey || key == SensorPowerKey
}

var (
	// Maps are public so other packages (like alpaca) can use them.
	// IMPORTANT: Access these via GetSwitchIDMap() and GetShortSwitchKeyByID() for thread safety.
	// Sensors are always at IDs 0, 1, 2. Power switches start at ID 3.
	SwitchIDMap = map[int]string{
		0: SensorVoltageKey, 1: SensorCurrentKey, 2: SensorPowerKey,
		3: "dc1", 4: "dc2", 5: "dc3", 6: "dc4", 7: "dc5",
		8: "usbc12", 9: "usb345", 10: "adj_conv", 11: "pwm1", 12: "pwm2",
		13: "master_power",
	}
	ShortSwitchIDMap = map[string]string{
		"dc1": "d1", "dc2": "d2", "dc3": "d3", "dc4": "d4", "dc5": "d5",
		"usbc12": "u12", "usb345": "u34", "adj_conv": "adj", "pwm1": "pwm1", "pwm2": "pwm2",
		"master_power": "all",
		// Sensors don't need short keys as they read from serial.Conditions
	}
	ShortSwitchKeyByID = map[int]string{
		// Sensors at 0, 1, 2 - these use different data source
		0: SensorVoltageKey, 1: SensorCurrentKey, 2: SensorPowerKey,
		3: "d1", 4: "d2", 5: "d3", 6: "d4", 7: "d5",
		8: "u12", 9: "u34", 10: "adj", 11: "pwm1", 12: "pwm2",
		13: "all",
	}

	proxyConfig     *ProxyConfig // Singleton instance
	proxyConfigFile string       // Full path to the config file
)

// GetSwitchMapLength returns the number of switches in a thread-safe manner.
func GetSwitchMapLength() int {
	SwitchMapMutex.RLock()
	defer SwitchMapMutex.RUnlock()
	return len(SwitchIDMap)
}

// GetSwitchIDMapEntry returns the switch name for a given ID in a thread-safe manner.
func GetSwitchIDMapEntry(id int) (string, bool) {
	SwitchMapMutex.RLock()
	defer SwitchMapMutex.RUnlock()
	val, ok := SwitchIDMap[id]
	return val, ok
}

// GetShortSwitchKeyByIDEntry returns the short key for a given ID in a thread-safe manner.
func GetShortSwitchKeyByIDEntry(id int) (string, bool) {
	SwitchMapMutex.RLock()
	defer SwitchMapMutex.RUnlock()
	val, ok := ShortSwitchKeyByID[id]
	return val, ok
}

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
				NetworkPort:    32241,
				ListenAddress:  "127.0.0.1", // Default to localhost only
				LogLevel:       "INFO",
				SwitchNames:    make(map[string]string),
				HeaterAutoEnableLeader: map[string]bool{
					"pwm1": true,
					"pwm2": true,
				},
				HistoryRetentionNights: 10, // Default to 10 nights
				TelemetryInterval:      10, // Default to 10 seconds
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
		proxyConfig.NetworkPort = 32241
	}
	if proxyConfig.ListenAddress == "" {
		logger.Warn("Configuration key 'ListenAddress' not found, using default '127.0.0.1'.")
		proxyConfig.ListenAddress = "127.0.0.1"
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
	// Defaults for new fields
	if proxyConfig.HistoryRetentionNights == 0 {
		proxyConfig.HistoryRetentionNights = 10
	}
	// Note: TelemetryInterval=0 is valid (means disabled), so no auto-default here

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

// GetSetupURL builds the full URL for the web setup page based on the current config.
func GetSetupURL() string {
	conf := Get()
	host := conf.ListenAddress
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d/setup", host, conf.NetworkPort)
}

// GetSetupURLFromFile reads the configuration file directly to build the setup URL.
// This is a special case for the single-instance check, which runs before the main
// configuration and logging are initialized. It ensures that a second instance
// opens the correct URL based on the saved listenAddress.
func GetSetupURLFromFile() string {
	const defaultHost = "127.0.0.1"
	const defaultPort = 32241

	file, err := os.ReadFile(proxyConfigFile)
	if err != nil {
		// File not found or other error, use failsafe defaults.
		return fmt.Sprintf("http://%s:%d/setup", defaultHost, defaultPort)
	}

	var config struct {
		NetworkPort   int    `json:"networkPort"`
		ListenAddress string `json:"listenAddress"`
	}
	if err := json.Unmarshal(file, &config); err != nil {
		// JSON is corrupt, use failsafe defaults.
		return fmt.Sprintf("http://%s:%d/setup", defaultHost, defaultPort)
	}

	host := config.ListenAddress
	port := config.NetworkPort

	if host == "0.0.0.0" || host == "" {
		host = defaultHost
	}
	if port == 0 {
		port = defaultPort
	}

	return fmt.Sprintf("http://%s:%d/setup", host, port)
}
