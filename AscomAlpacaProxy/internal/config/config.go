package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync"
)

// defaultListenAddress returns the platform-specific default listen address.
// On Linux (typically headless servers/Astro-Pi), we default to 0.0.0.0 for network access.
// On Windows, we default to 127.0.0.1 (localhost only) for security.
func defaultListenAddress() string {
	if runtime.GOOS == "linux" {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// DeviceProfile holds settings that belong to a specific physical SV241 box (identified by its
// factory MAC address, see SetActiveDeviceSerial) rather than to this proxy installation. Keeping
// these keyed by device rather than flat means swapping which box is plugged into this computer -
// or moving a box to a different computer's proxy install via Backup/Restore - carries the right
// names with it instead of leaving them behind.
type DeviceProfile struct {
	RigName                string            `json:"rigName"` // User-facing label shown instead of the raw MAC
	SwitchNames            map[string]string `json:"switchNames"`
	LensTempName           string            `json:"lensTempName"`
	HeaterAutoEnableLeader map[string]bool   `json:"heaterAutoEnableLeader"`
	WeatherSourcePriority  map[string]string `json:"weatherSourcePriority"`
}

// ProxyConfig stores configuration specific to the Go proxy itself.
type ProxyConfig struct {
	SerialPortName string `json:"serialPortName"`
	AutoDetectPort bool   `json:"autoDetectPort"`
	NetworkPort    int    `json:"networkPort"`
	ListenAddress  string `json:"listenAddress"`
	LogLevel       string `json:"logLevel"`

	// DeviceProfiles holds the per-box settings below, keyed by device MAC address (see
	// SetActiveDeviceSerial). SwitchNames/LensTempName/HeaterAutoEnableLeader/WeatherSourcePriority
	// are kept as top-level fields too, but only as a live mirror of whichever device is currently
	// active (see applyActiveProfileLocked/syncActiveProfileFromFlatLocked) - every read/write
	// call site elsewhere in the codebase keeps using them exactly as before, unaware that a
	// specific box is involved at all. Access DeviceProfiles itself only through the Get*/Set*
	// accessors below, never directly - same concurrency hazard as the map fields it contains.
	DeviceProfiles         map[string]DeviceProfile `json:"deviceProfiles"`
	SwitchNames            map[string]string        `json:"switchNames"`
	HeaterAutoEnableLeader map[string]bool          `json:"heaterAutoEnableLeader"`

	HistoryRetentionNights     int    `json:"historyRetentionNights"`
	TelemetryInterval          int    `json:"telemetryInterval"`          // Seconds
	EnableAlpacaVoltageControl bool   `json:"enableAlpacaVoltageControl"` // Allow voltage control via Alpaca
	EnableAlpacaDiscovery      bool   `json:"enableAlpacaDiscovery"`      // Respond to Alpaca UDP discovery packets
	EnableMasterPower          bool   `json:"enableMasterPower"`          // Show Master Power switch
	EnableNotifications        bool   `json:"enableNotifications"`        // Show Windows toast notifications
	AlwaysShowLensTemp         bool   `json:"alwaysShowLensTemp"`         // Always expose Lens Temp switch regardless of PID mode
	LensTempName               string `json:"lensTempName"`               // Custom name for Lens Temp sensor check
	FirstRunComplete           bool   `json:"firstRunComplete"`           // Onboarding wizard completed

	// Weather Service (Open-Meteo)
	EnableWeatherService  bool              `json:"enableWeatherService"`
	WeatherLatitude       float64           `json:"weatherLatitude"`
	WeatherLongitude      float64           `json:"weatherLongitude"`
	WeatherModel          string            `json:"weatherModel"`          // best_match, icon_seamless, etc.
	WeatherInterval       int               `json:"weatherInterval"`       // Minutes
	WeatherSourcePriority map[string]string `json:"weatherSourcePriority"` // metric -> hardware|internet|hybrid
}

// CombinedConfig defines the structure for a full backup file.
type CombinedConfig struct {
	ProxyConfig    *ProxyConfig    `json:"proxyConfig"`
	FirmwareConfig json.RawMessage `json:"firmwareConfig"`
	// FirmwareConfigSerial is the MAC of whichever device FirmwareConfig was actually captured
	// from, so a restore can warn before applying one box's on-device settings (calibration
	// offsets, heater config, power startup states, etc.) to a *different* physical box. "" for
	// backups made before this field existed - treated as "unknown, can't verify" at restore
	// time, same as a genuine mismatch.
	FirmwareConfigSerial string `json:"firmwareConfigSerial,omitempty"`
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

// ProxyConfigMutex protects concurrent access to the map-valued fields of the ProxyConfig
// singleton (SwitchNames, HeaterAutoEnableLeader, WeatherSourcePriority) - config.Get() returns a
// bare pointer with no synchronization of its own, and HandlePostSettings (re)assigns these maps
// wholesale from an HTTP handler goroutine while other goroutines read them concurrently. Use the
// Get*/Set* accessors below rather than accessing these map fields directly - a concurrent map
// read racing a write is not just a data race but can trigger Go's fatal "concurrent map read and
// write" runtime error, crashing the process.
var ProxyConfigMutex sync.RWMutex

// GetSwitchName returns the custom display name for a switch's internal key, thread-safely.
func GetSwitchName(internalName string) string {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	return Get().SwitchNames[internalName]
}

// SetSwitchName sets the custom display name for a switch's internal key, thread-safely.
func SetSwitchName(internalName, displayName string) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	conf := Get()
	conf.SwitchNames[internalName] = displayName
	syncActiveProfileFromFlatLocked(conf)
}

// SetLensTempName sets the custom display name for the Lens Temp sensor check, thread-safely. Use
// this instead of assigning conf.LensTempName directly - a direct assignment would skip syncing
// the change into the active device's profile (see syncActiveProfileFromFlatLocked).
func SetLensTempName(name string) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	conf := Get()
	conf.LensTempName = name
	syncActiveProfileFromFlatLocked(conf)
}

// GetHeaterAutoEnableLeader returns whether auto-enabling the leader is on for a follower heater
// key, thread-safely.
func GetHeaterAutoEnableLeader(followerKey string) bool {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	return Get().HeaterAutoEnableLeader[followerKey]
}

// GetWeatherSourcePriority returns the configured source priority for a weather metric,
// thread-safely.
func GetWeatherSourcePriority(metric string) string {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	return Get().WeatherSourcePriority[metric]
}

// SetProxyMaps atomically replaces SwitchNames, HeaterAutoEnableLeader, and WeatherSourcePriority
// on the ProxyConfig singleton. Use this from HandlePostSettings/backup-restore instead of
// assigning conf.SwitchNames = ... etc. directly, which would race concurrent readers.
func SetProxyMaps(switchNames map[string]string, heaterAutoEnableLeader map[string]bool, weatherSourcePriority map[string]string) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	conf := Get()
	if switchNames != nil {
		conf.SwitchNames = switchNames
	}
	if heaterAutoEnableLeader != nil {
		conf.HeaterAutoEnableLeader = heaterAutoEnableLeader
	}
	if weatherSourcePriority != nil {
		conf.WeatherSourcePriority = weatherSourcePriority
	}
	syncActiveProfileFromFlatLocked(conf)
}

// SetDeviceProfiles atomically replaces every known device's profile, e.g. when restoring a full
// backup - this is what makes a backup taken on one computer carry every box's names correctly to
// another. Re-applies whichever device is currently active from the newly-restored data
// afterwards, since the restored map may not even contain an entry for it yet.
func SetDeviceProfiles(profiles map[string]DeviceProfile) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	if profiles == nil {
		return
	}
	conf := Get()
	conf.DeviceProfiles = profiles
	if activeDeviceSerial != "" {
		if profile, exists := conf.DeviceProfiles[activeDeviceSerial]; exists {
			applyActiveProfileLocked(conf, profile)
		}
	}
}

// GetActiveDeviceSerial returns the MAC address of whichever SV241 box is currently connected, or
// "" if none has connected yet this run. Thread-safe.
func GetActiveDeviceSerial() string {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	return activeDeviceSerial
}

// GetActiveRigName returns the user-facing label for the currently active device's profile, or ""
// if no device is active yet. Thread-safe.
func GetActiveRigName() string {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	if activeDeviceSerial == "" {
		return ""
	}
	return Get().DeviceProfiles[activeDeviceSerial].RigName
}

// GetRigNameForSerial returns the rig name for an arbitrary (not necessarily currently active)
// device serial, or "" if that serial has no known profile or no rig name set. Used to label
// historical telemetry by whichever device actually recorded it, rather than whichever device
// happens to be connected right now - see GetActiveRigName for that. Thread-safe.
func GetRigNameForSerial(serial string) string {
	ProxyConfigMutex.RLock()
	defer ProxyConfigMutex.RUnlock()
	if serial == "" {
		return ""
	}
	return Get().DeviceProfiles[serial].RigName
}

// SetActiveRigName sets the user-facing label for the currently active device's profile. No-op if
// no device is active yet. Thread-safe.
func SetActiveRigName(name string) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	if activeDeviceSerial == "" {
		return
	}
	conf := Get()
	profile := conf.DeviceProfiles[activeDeviceSerial]
	profile.RigName = name
	conf.DeviceProfiles[activeDeviceSerial] = profile
}

// SetActiveDeviceSerial records which physical box is currently connected (keyed by the MAC
// address the firmware reports in {"get":"version"}) and switches the flat mirror fields
// (SwitchNames, LensTempName, HeaterAutoEnableLeader, WeatherSourcePriority) over to that device's
// own profile - every other accessor in this file keeps reading/writing those same flat fields
// unaware a specific box is involved at all. Called once per successful connection by the serial
// package, right after the device's MAC is read back.
//
// The very first device this install ever sees inherits whatever the flat fields already held
// (DeviceProfiles is empty at that point) - this is the upgrade path for existing single-box
// users, who should see their existing names completely unchanged. Any device after that starts
// from the same clean defaults Load() gives a brand new install, rather than inheriting an
// unrelated box's custom names.
func SetActiveDeviceSerial(serial string) {
	ProxyConfigMutex.Lock()
	defer ProxyConfigMutex.Unlock()
	if serial == "" || serial == activeDeviceSerial {
		return
	}
	activeDeviceSerial = serial
	conf := Get()
	if conf.DeviceProfiles == nil {
		conf.DeviceProfiles = make(map[string]DeviceProfile)
	}
	profile, exists := conf.DeviceProfiles[serial]
	if !exists {
		if len(conf.DeviceProfiles) == 0 {
			profile = DeviceProfile{
				SwitchNames:            conf.SwitchNames,
				LensTempName:           conf.LensTempName,
				HeaterAutoEnableLeader: conf.HeaterAutoEnableLeader,
				WeatherSourcePriority:  conf.WeatherSourcePriority,
			}
		} else {
			profile = newDefaultDeviceProfile()
		}
		conf.DeviceProfiles[serial] = profile
	}
	applyActiveProfileLocked(conf, profile)
	logger.Info("Active device profile: serial=%s, rigName=%q", serial, profile.RigName)
	go func() {
		if err := Save(); err != nil {
			logger.Error("Failed to save proxy config after switching active device profile: %v", err)
		}
	}()
}

// applyActiveProfileLocked copies a device profile's fields onto the flat mirror fields. Caller
// must hold ProxyConfigMutex.
func applyActiveProfileLocked(conf *ProxyConfig, profile DeviceProfile) {
	conf.SwitchNames = profile.SwitchNames
	conf.LensTempName = profile.LensTempName
	conf.HeaterAutoEnableLeader = profile.HeaterAutoEnableLeader
	conf.WeatherSourcePriority = profile.WeatherSourcePriority
}

// syncActiveProfileFromFlatLocked writes the flat mirror fields back into the active device's
// entry in DeviceProfiles, so a change made through any of the Get*/Set* accessors above actually
// persists per-box instead of only updating the transient mirror. No-op if no device has connected
// yet (activeDeviceSerial == "") - the flat fields still work as a plain fallback in that case,
// exactly as they did before this feature existed, and get adopted as the first profile the moment
// a device does connect (see SetActiveDeviceSerial). Caller must hold ProxyConfigMutex.
func syncActiveProfileFromFlatLocked(conf *ProxyConfig) {
	if activeDeviceSerial == "" {
		return
	}
	if conf.DeviceProfiles == nil {
		conf.DeviceProfiles = make(map[string]DeviceProfile)
	}
	conf.DeviceProfiles[activeDeviceSerial] = DeviceProfile{
		RigName:                conf.DeviceProfiles[activeDeviceSerial].RigName,
		SwitchNames:            conf.SwitchNames,
		LensTempName:           conf.LensTempName,
		HeaterAutoEnableLeader: conf.HeaterAutoEnableLeader,
		WeatherSourcePriority:  conf.WeatherSourcePriority,
	}
}

// newDefaultDeviceProfile builds a fresh profile using the same defaults Load() gives a brand new
// install - used for every device profile after the very first one (see SetActiveDeviceSerial).
func newDefaultDeviceProfile() DeviceProfile {
	SwitchMapMutex.RLock()
	switchNames := make(map[string]string, len(SwitchIDMap))
	for _, internalName := range SwitchIDMap {
		switchNames[internalName] = internalName
	}
	SwitchMapMutex.RUnlock()
	return DeviceProfile{
		SwitchNames: switchNames,
		HeaterAutoEnableLeader: map[string]bool{
			"pwm1": true,
			"pwm2": true,
		},
		WeatherSourcePriority: make(map[string]string),
	}
}

// Sensor switch keys - these are read-only sensors at fixed IDs 0, 1, 2
// Sensor switch keys - these are read-only sensors at fixed IDs 0, 1, 2
const (
	SensorVoltageKey  = "sensor_voltage"
	SensorCurrentKey  = "sensor_current"
	SensorPowerKey    = "sensor_power"
	SensorLensTempKey = "sensor_lens_temp"
	SensorPWM1Key     = "sensor_pwm1"
	SensorPWM2Key     = "sensor_pwm2"
)

// IsSensorSwitch returns true if the switch key is a read-only sensor
func IsSensorSwitch(key string) bool {
	return key == SensorVoltageKey || key == SensorCurrentKey || key == SensorPowerKey ||
		key == SensorLensTempKey || key == SensorPWM1Key || key == SensorPWM2Key
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

	// activeDeviceSerial is the MAC address of whichever SV241 box is currently connected, or ""
	// if none has connected yet this run. Guarded by ProxyConfigMutex - see SetActiveDeviceSerial.
	activeDeviceSerial string
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

// GetShortSwitchKeyByIDSnapshot returns a copy of ShortSwitchKeyByID, safe to range over without
// racing SyncFirmwareConfig's wholesale reassignment of the live map. The single-entry getters
// above don't cover iteration - callers that need to loop over every switch (e.g. to compute an
// "all on" aggregate) should copy once via this function rather than ranging over the package
// variable directly.
func GetShortSwitchKeyByIDSnapshot() map[int]string {
	SwitchMapMutex.RLock()
	defer SwitchMapMutex.RUnlock()
	snapshot := make(map[int]string, len(ShortSwitchKeyByID))
	for k, v := range ShortSwitchKeyByID {
		snapshot[k] = v
	}
	return snapshot
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
				ListenAddress:  defaultListenAddress(),
				LogLevel:       "INFO",
				SwitchNames:    make(map[string]string),
				HeaterAutoEnableLeader: map[string]bool{
					"pwm1": true,
					"pwm2": true,
				},
				HistoryRetentionNights: 10,   // Default to 10 nights
				TelemetryInterval:      10,   // Default to 10 seconds
				EnableAlpacaDiscovery:  true, // Default to discovery enabled
				EnableNotifications:    true, // Default to notifications enabled
				WeatherInterval:        5,    // Default to 5 minutes
				WeatherModel:           "best_match",
				WeatherSourcePriority:  make(map[string]string),
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

	// Unmarshal into a map to check for missing boolean keys (which default to false)
	var rawMap map[string]interface{}
	if err := json.Unmarshal(file, &rawMap); err == nil {
		if _, exists := rawMap["enableAlpacaDiscovery"]; !exists {
			logger.Info("Configuration key 'enableAlpacaDiscovery' not found, defaulting to true.")
			tempConfig.EnableAlpacaDiscovery = true
		}
	}

	proxyConfig = &tempConfig

	// --- Validate and set defaults for missing fields ---
	if proxyConfig.NetworkPort == 0 {
		proxyConfig.NetworkPort = 32241
	}
	if proxyConfig.ListenAddress == "" {
		defAddr := defaultListenAddress()
		logger.Warn("Configuration key 'ListenAddress' not found, using default '%s'.", defAddr)
		proxyConfig.ListenAddress = defAddr
	}
	if proxyConfig.LogLevel == "" {
		logger.Warn("Configuration key 'LogLevel' not found, using default 'INFO'.")
		proxyConfig.LogLevel = "INFO"
	}
	if proxyConfig.DeviceProfiles == nil {
		proxyConfig.DeviceProfiles = make(map[string]DeviceProfile)
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

	// Weather Defaults
	if proxyConfig.WeatherInterval < 1 {
		proxyConfig.WeatherInterval = 5
	}
	if proxyConfig.WeatherModel == "" {
		proxyConfig.WeatherModel = "best_match"
	}
	if proxyConfig.WeatherSourcePriority == nil {
		proxyConfig.WeatherSourcePriority = make(map[string]string)
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
