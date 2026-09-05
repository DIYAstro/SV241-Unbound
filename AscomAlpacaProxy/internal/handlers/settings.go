package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
)

// SettingsResponse defines the structure for the GET /api/v1/settings response.
type SettingsResponse struct {
	ProxyConfig         *config.ProxyConfig `json:"proxy_config"`
	AvailableIPs        []string            `json:"available_ips"`
	ActiveSwitches      map[int]string      `json:"active_switches"`
	SerialPortConnected bool                `json:"serial_port_connected"`
	ReconnectPaused     bool                `json:"reconnect_paused"`
	// ActiveDeviceSerial/ActiveRigName identify whichever SV241 box is currently connected - both
	// are "" until a device has connected at least once this run. See config.DeviceProfile.
	ActiveDeviceSerial string `json:"active_device_serial"`
	ActiveRigName      string `json:"active_rig_name"`
}

// HandleGetSettings provides the current proxy configuration and available IP addresses.
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	conf := config.Get()
	ips, err := getAvailableIPs()
	if err != nil {
		logger.Error("Failed to get available IP addresses: %v", err)
		http.Error(w, "Failed to get IP addresses", http.StatusInternalServerError)
		return
	}

	response := SettingsResponse{
		ProxyConfig:         conf,
		AvailableIPs:        ips,
		ActiveSwitches:      config.SwitchIDMap,
		SerialPortConnected: serial.IsConnected(),
		ReconnectPaused:     serial.IsReconnectPaused(),
		ActiveDeviceSerial:  config.GetActiveDeviceSerial(),
		ActiveRigName:       config.GetActiveRigName(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// postSettingsPayload is the POST /api/v1/settings request body. It embeds config.ProxyConfig for
// every plain proxy-wide setting, plus ActiveRigName - which isn't a ProxyConfig field itself (it
// lives on whichever DeviceProfile is currently active, see config.SetActiveRigName) but is edited
// alongside everything else on the same settings page.
//
// ActiveRigName is a *string, not string, deliberately: a caller that posts its proxy_config
// as-is (e.g. SwitchConfig.vue's save flow, which spreads store.proxyConfig - a shape that never
// had this field, since it isn't part of ProxyConfig) omits this key entirely, which must leave
// the rig name untouched rather than being decoded as "" and wiping out whatever was set before.
// Same reasoning as SwitchNames/HeaterAutoEnableLeader/WeatherSourcePriority - nil means "not
// provided", not "clear it" - see SetProxyMaps.
type postSettingsPayload struct {
	config.ProxyConfig
	ActiveRigName *string `json:"active_rig_name"`
}

// HandlePostSettings updates the proxy configuration.
func HandlePostSettings(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var payload postSettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}
	newConfig := payload.ProxyConfig

	// Basic validation
	if newConfig.NetworkPort <= 0 || newConfig.NetworkPort > 65535 {
		http.Error(w, "Invalid Network Port", http.StatusBadRequest)
		return
	}
	if net.ParseIP(newConfig.ListenAddress) == nil && newConfig.ListenAddress != "0.0.0.0" {
		http.Error(w, "Invalid Listen Address", http.StatusBadRequest)
		return
	}

	conf := config.Get()
	// Check if serial port settings have changed to trigger a reconnect
	portChanged := conf.SerialPortName != newConfig.SerialPortName || conf.AutoDetectPort != newConfig.AutoDetectPort

	// Update all relevant fields from the new config
	conf.ListenAddress = newConfig.ListenAddress
	conf.NetworkPort = newConfig.NetworkPort
	conf.SerialPortName = newConfig.SerialPortName
	conf.AutoDetectPort = newConfig.AutoDetectPort
	conf.LogLevel = newConfig.LogLevel
	// SwitchNames/HeaterAutoEnableLeader/WeatherSourcePriority are maps read concurrently by other
	// goroutines - assigning them directly here would race those readers (see ProxyConfigMutex's
	// doc comment). WeatherSourcePriority is set the same way further down, once its value is
	// validated.
	config.SetProxyMaps(newConfig.SwitchNames, newConfig.HeaterAutoEnableLeader, nil)
	conf.HistoryRetentionNights = newConfig.HistoryRetentionNights
	conf.TelemetryInterval = newConfig.TelemetryInterval
	conf.EnableAlpacaVoltageControl = newConfig.EnableAlpacaVoltageControl
	conf.EnableAlpacaDiscovery = newConfig.EnableAlpacaDiscovery
	conf.EnableMasterPower = newConfig.EnableMasterPower
	conf.EnableNotifications = newConfig.EnableNotifications
	conf.AlwaysShowLensTemp = newConfig.AlwaysShowLensTemp
	// LensTempName is tied to the active device's profile (see config.SetLensTempName) - a direct
	// assignment here would skip syncing the change into it.
	config.SetLensTempName(newConfig.LensTempName)
	if payload.ActiveRigName != nil {
		config.SetActiveRigName(*payload.ActiveRigName)
	}
	conf.FirstRunComplete = newConfig.FirstRunComplete

	// Update Weather Service Settings
	conf.EnableWeatherService = newConfig.EnableWeatherService
	conf.WeatherLatitude = newConfig.WeatherLatitude
	conf.WeatherLongitude = newConfig.WeatherLongitude
	conf.WeatherModel = newConfig.WeatherModel
	conf.WeatherInterval = newConfig.WeatherInterval
	if conf.WeatherInterval < 1 {
		conf.WeatherInterval = 5
	}
	config.SetProxyMaps(nil, nil, newConfig.WeatherSourcePriority)

	// Apply log level immediately
	logger.SetLevelFromString(conf.LogLevel)

	if err := config.Save(); err != nil {
		logger.Error("Failed to save proxy config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	// Trigger reconnect in a goroutine if needed
	if portChanged {
		logger.Info("Serial port configuration changed. Triggering reconnect.")
		go serial.Reconnect(conf.SerialPortName)
	} else {
		// If port didn't change, we still might need to update the switch map (e.g. Master Power)
		// Reconnect triggers it internally, so we only need it here if NOT reconnecting.
		// Also, SyncFirmwareConfig relies on a stable connection.
		go serial.SyncFirmwareConfig()
	}

	logger.Info("Proxy settings updated via API.")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(conf)
}

// getAvailableIPs returns a list of local IPv4 addresses.
func getAvailableIPs() ([]string, error) {
	ips := []string{"127.0.0.1", "0.0.0.0"}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ipStr := ipnet.IP.String()
				// Filter out APIPA addresses (169.254.x.x)
				if !strings.HasPrefix(ipStr, "169.254.") {
					ips = append(ips, ipStr)
				}
			}
		}
	}
	return ips, nil
}
