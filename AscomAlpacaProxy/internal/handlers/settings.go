package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
)

// SettingsResponse defines the structure for the GET /api/v1/settings response.
type SettingsResponse struct {
	ProxyConfig    *config.ProxyConfig `json:"proxy_config"`
	AvailableIPs   []string            `json:"available_ips"`
	ActiveSwitches map[int]string      `json:"active_switches"`
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
		ProxyConfig:    conf,
		AvailableIPs:   ips,
		ActiveSwitches: config.SwitchIDMap,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandlePostSettings updates the proxy configuration.
func HandlePostSettings(w http.ResponseWriter, r *http.Request) {
	var newConfig config.ProxyConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

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
	conf.SwitchNames = newConfig.SwitchNames
	conf.SwitchNames = newConfig.SwitchNames
	conf.HeaterAutoEnableLeader = newConfig.HeaterAutoEnableLeader
	conf.HistoryRetentionNights = newConfig.HistoryRetentionNights
	conf.TelemetryInterval = newConfig.TelemetryInterval
	conf.EnableAlpacaVoltageControl = newConfig.EnableAlpacaVoltageControl

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
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips, nil
}
