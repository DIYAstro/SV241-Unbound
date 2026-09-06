package server

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sv241pro-alpaca-proxy/internal/alpaca"
	"sv241pro-alpaca-proxy/internal/backup"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/handlers"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/logstream"
	"sv241pro-alpaca-proxy/internal/serial"
	"sv241pro-alpaca-proxy/internal/telemetry"
)

// Start initializes and starts the HTTP server, serving the frontend from the provided filesystem.
func Start(frontendFS fs.FS, appVersion string) {
	setupRoutes(frontendFS, appVersion)

	conf := config.Get()
	addr := fmt.Sprintf("%s:%d", conf.ListenAddress, conf.NetworkPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Fatal("Could not bind to address '%s' (reason: %v). Please check your configuration.", addr, err)
		return // Unreachable, but good practice
	}

	logger.Info("Starting Alpaca API server on %s...", addr)

	// Initialize CSV Telemetry Logger
	telemetry.Init()

	// Global HTTP handler with request logging. Deliberately does NOT lowercase r.URL.Path before
	// dispatch (a prior version did, "for case-insensitive routing") - that made a capitalized
	// device-type segment (e.g. /api/v1/SWITCH/0/description) match the lowercase-registered route
	// and return 200, which ASCOM Conform Universal 4.5.0 flags as a spec violation (non-lowercase
	// device-type URLs must get a 4xx, not succeed). The only case-insensitivity Alpaca actually
	// needs - the method-name segment (e.g. Description vs description) - is already handled
	// independently by deviceMux's own strings.ToLower(path[lastSlash+1:]) below, so removing this
	// doesn't lose any real leniency: a capitalized device type now falls through to the catch-all
	// "/" handler's static-file-not-found 404, which Conform accepts.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("Global HTTP: %s %s (from %s)", r.Method, r.URL.Path, r.RemoteAddr)
		http.DefaultServeMux.ServeHTTP(w, r)
	})

	if err := http.Serve(listener, handler); err != nil {
		logger.Fatal("HTTP server failed: %v", err)
	}
}

func setupRoutes(frontendFS fs.FS, appVersion string) {
	api := alpaca.NewAPI(appVersion)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/setup" {
			// Serve the SPA entry point
			http.ServeFileFS(w, r, frontendFS, "index.html")
		} else {
			// Serve static assets
			http.FileServer(http.FS(frontendFS)).ServeHTTP(w, r)
		}
	})

	http.HandleFunc("/flasher", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, frontendFS, "flasher/index.html")
	})
	// Create a sub-filesystem for the flasher directory so that /flasher/firmware/x.bin works correctly
	flasherFS, err := fs.Sub(frontendFS, "flasher")
	if err != nil {
		logger.Error("Failed to create flasher sub-filesystem: %v", err)
	} else {
		http.Handle("/flasher/", http.StripPrefix("/flasher/", http.FileServer(http.FS(flasherFS))))
	}

	// --- Management API ---
	http.HandleFunc("/management/v1/description", api.HandleManagementDescription)
	http.HandleFunc("/management/v1/configureddevices", alpaca.HandleManagementConfiguredDevices)
	http.HandleFunc("/management/apiversions", alpaca.HandleManagementApiVersions)

	// --- Setup Page API ---
	http.HandleFunc("/api/v1/config", handleGetFirmwareConfig)
	http.HandleFunc("/api/v1/config/set", handleSetFirmwareConfig)
	http.HandleFunc("/api/v1/power/status", handleGetPowerStatus)
	http.HandleFunc("/api/v1/status", handleGetLiveStatus)
	http.HandleFunc("/api/v1/power/all", handleSetAllPower)
	http.HandleFunc("/api/v1/command", handleDeviceCommand)
	http.HandleFunc("/api/v1/firmware/version", handleGetFirmwareVersion)
	http.HandleFunc("/api/v1/proxy/version", handleGetProxyVersion(appVersion))
	http.HandleFunc("/api/v1/backup/create", handleCreateBackup)
	http.HandleFunc("/api/v1/backup/restore", handleRestoreBackup)
	http.HandleFunc("/api/v1/backup/list", handleListAutoBackups)
	http.HandleFunc("/api/v1/backup/restore-auto", handleRestoreAutoBackup)
	http.HandleFunc("/api/v1/telemetry/dates", telemetry.HandleGetLogDates)
	http.HandleFunc("/api/v1/telemetry/history", telemetry.HandleGetHistory)
	http.HandleFunc("/api/v1/telemetry/download", telemetry.HandleDownloadCSV)
	http.HandleFunc("/api/v1/log/download", handleDownloadLog)
	http.HandleFunc("/api/serial/release", handleSerialRelease)
	http.HandleFunc("/api/serial/resume", handleSerialResume)

	// New settings endpoint combines getting and setting proxy config
	http.HandleFunc("/api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// This handler now returns the proxy config AND available IPs
			handlers.HandleGetSettings(w, r)
		} else if r.Method == http.MethodPost {
			// This handler now saves the entire proxy config
			handlers.HandlePostSettings(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// --- WebSocket ---
	http.HandleFunc("/ws/logs", logstream.ServeWs)

	// --- Alpaca Device API ---
	setupAlpacaDeviceRoutes(api)
}

func setupAlpacaDeviceRoutes(api *alpaca.API) {
	// Redirects for ASCOM client setup requests
	http.HandleFunc("/setup/v1/switch/0/setup", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/setup", http.StatusFound) })
	http.HandleFunc("/setup/v1/observingconditions/0/setup", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/setup", http.StatusFound) })

	// Common handlers
	commonHandlers := map[string]http.HandlerFunc{
		"description":      api.HandleDeviceDescription,
		"driverinfo":       api.HandleDriverInfo,
		"driverversion":    api.HandleDriverVersion,
		"connected":        api.HandleConnected,
		"interfaceversion": api.HandleInterfaceVersion,
	}

	// Switch device
	switchHandlers := map[string]http.HandlerFunc{
		"maxswitch":            api.HandleSwitchMaxSwitch,
		"getswitchname":        api.HandleSwitchGetSwitchName,
		"setswitchname":        api.HandleSwitchSetSwitchName,
		"canwrite":             api.HandleSwitchCanWrite,
		"getswitch":            api.HandleSwitchGetSwitch,
		"getswitchvalue":       api.HandleSwitchGetSwitchValue,
		"setswitchvalue":       api.HandleSwitchSetSwitchValue,
		"setswitch":            api.HandleSwitchSetSwitchValue, // Alias
		"getswitchdescription": api.HandleSwitchGetSwitchDescription,
		"maxswitchvalue":       api.HandleSwitchMaxSwitchValue,
		"minswitchvalue":       api.HandleSwitchMinSwitchValue,
		"switchstep":           api.HandleSwitchSwitchStep,
		"name":                 api.HandleDeviceName("SV241 Power Switch"),
		"supportedactions":     api.HandleSwitchSupportedActions,
		"action":               api.HandleSwitchAction,
	}
	for k, v := range commonHandlers {
		switchHandlers[k] = v
	}
	http.HandleFunc("/api/v1/switch/0/", alpaca.Handler(deviceMux(switchHandlers, api)))

	// ObservingConditions device
	obsCondHandlers := map[string]http.HandlerFunc{
		"temperature":         api.HandleObsCondValue("temperature"),
		"humidity":            api.HandleObsCondValue("humidity"),
		"dewpoint":            api.HandleObsCondValue("dewpoint"),
		"name":                api.HandleDeviceName("SV241 Environment"),
		"supportedactions":    api.HandleSupportedActions,
		"action":              api.HandleObsCondAction,
		"averageperiod":       api.HandleObsCondAveragePeriod,
		"sensordescription":   api.HandleObsCondSensorDescription,
		"timesincelastupdate": api.HandleObsCondTimeSinceLastUpdate,
		"refresh":             api.HandleObsCondRefresh,
		"cloudcover":          api.HandleObsCondValue("cloudcover"),
		"pressure":            api.HandleObsCondValue("pressure"),
		"rainrate":            api.HandleObsCondValue("rainrate"),
		"latestupdatetime":    api.HandleObsCondLatestUpdateTime,
		"latestupdate":        api.HandleObsCondLatestUpdateTime, // Alias
		"skybrightness":       api.HandleObsCondNotImplemented,
		"skyquality":          api.HandleObsCondNotImplemented,
		"skytemperature":      api.HandleObsCondNotImplemented,
		"starfwhm":            api.HandleObsCondNotImplemented,
		"winddirection":       api.HandleObsCondValue("winddirection"),
		"windgust":            api.HandleObsCondValue("windgust"),
		"windspeed":           api.HandleObsCondValue("windspeed"),
	}
	for k, v := range commonHandlers {
		obsCondHandlers[k] = v
	}
	http.HandleFunc("/api/v1/observingconditions/0/", alpaca.Handler(deviceMux(obsCondHandlers, api)))
}

// deviceMux creates a handler that routes to sub-handlers based on the final URL path segment.
func deviceMux(handlers map[string]http.HandlerFunc, api *alpaca.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash == -1 {
			alpaca.ErrorResponse(w, r, http.StatusNotFound, 0x404, "Invalid URL path.")
			return
		}
		method := strings.ToLower(path[lastSlash+1:])
		logger.Debug("Alpaca DeviceMux: Routing method '%s' (Path: %s)", method, r.URL.Path)

		if handler, ok := handlers[method]; ok {
			handler(w, r)
		} else {
			logger.Warn("Alpaca DeviceMux: Method '%s' not found on this device (Path: %s)", method, r.URL.Path)
			alpaca.ErrorResponse(w, r, http.StatusNotFound, 0x40C, fmt.Sprintf("Method '%s' not found on this device.", method))
		}
	}
}

// --- API Handlers ---

func handleGetFirmwareConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp, err := serial.SendCommand(`{"get":"config"}`, false, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, resp)
}

func handleSetFirmwareConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	var js json.RawMessage
	if json.Unmarshal(body, &js) != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}
	command := fmt.Sprintf(`{"sc":%s}`, string(body))
	logger.Debug("Sending to device: %s", command)
	resp, err := serial.SendCommand(command, true, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command to device: %v", err), http.StatusServiceUnavailable)
		return
	}

	// Trigger a switch map sync in case standard switches were enabled/disabled
	go serial.SyncFirmwareConfig()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, resp)
}

func handleGetPowerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serial.Status.RLock()
	defer serial.Status.RUnlock()
	if serial.Status.Data == nil {
		http.Error(w, "Status cache is not yet populated", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(serial.Status.Data)
}

func handleSetAllPower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var payload struct {
		State bool `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	stateInt := 0
	if payload.State {
		stateInt = 1
	}
	command := fmt.Sprintf(`{"set":{"all":%d}}`, stateInt)
	responseJSON, err := serial.SendCommand(command, true, 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command to device: %v", err), http.StatusServiceUnavailable)
		return
	}
	var statusData map[string]map[string]interface{}
	if json.Unmarshal([]byte(responseJSON), &statusData) == nil {
		if statusData["status"] == nil {
			logger.Warn("handleSetAllPower: device response did not contain 'status' key, skipping cache update")
		} else {
			serial.Status.Lock()
			serial.Status.Data = statusData["status"]
			serial.Status.Unlock()
		}
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetLiveStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serial.Conditions.RLock()
	defer serial.Conditions.RUnlock()
	if serial.Conditions.Data == nil {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(serial.Conditions.Data)
}

func handleDeviceCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// We need to check the command type to see if we should wait for a response.
	var commandPayload struct {
		Command string `json:"command"`
	}
	// We ignore the error here because the body might be a different type of command JSON
	// and we want to handle those generically.
	json.Unmarshal(body, &commandPayload)

	commandJSON := string(body)

	// Fire-and-forget commands
	if commandPayload.Command == "reboot" || commandPayload.Command == "factory_reset" {
		logger.Info("Received command '%s' from web UI. Sending to device.", commandJSON)
		serial.SendCommand(commandJSON, true, 0) // Don't wait for response
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"Command sent successfully"}`) // Return valid JSON
		return
	}

	// For all other commands, send and wait for a response.
	if commandPayload.Command == "dry_sensor" {
		logger.Info("Received command '%s' from web UI. Sending to device.", commandJSON)
	} else {
		logger.Debug("Received generic command from web UI: %s", commandJSON)
	}

	// Use a timeout that's appropriate for commands that might take a moment.
	resp, err := serial.SendCommand(commandJSON, true, 5*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command to device: %v", err), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// The device response is expected to be JSON, so we can just pass it through.
	fmt.Fprint(w, resp)
}

func handleDownloadLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	logPath := logger.GetLogFilePath()
	if logPath == "" {
		http.Error(w, "Log file path not available", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=\"proxy.log\"")
	http.ServeFile(w, r, logPath)
}

func handleGetFirmwareVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	response := struct {
		Version string `json:"version"`
	}{
		Version: serial.GetFirmwareVersion(),
	}
	json.NewEncoder(w).Encode(response)
}

func handleGetProxyVersion(appVersion string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := struct {
			Version string `json:"version"`
		}{
			Version: appVersion,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	logger.Info("Creating combined configuration backup...")
	snapshot, err := backup.BuildSnapshot()
	if err != nil {
		http.Error(w, "Failed to get firmware configuration", http.StatusInternalServerError)
		return
	}
	backupJSON, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		http.Error(w, "Failed to create backup file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="sv241_backup.json"`)
	w.Write(backupJSON)
	logger.Info("Successfully created and sent configuration backup.")
}

func handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	logger.Info("Restoring combined configuration from uploaded backup...")
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var backupData config.CombinedConfig
	if err := json.Unmarshal(body, &backupData); err != nil {
		http.Error(w, "Invalid backup file format", http.StatusBadRequest)
		return
	}
	if backupData.ProxyConfig == nil || backupData.FirmwareConfig == nil {
		http.Error(w, "Incomplete backup file", http.StatusBadRequest)
		return
	}
	force := r.URL.Query().Get("force") == "true"
	applyBackupRestore(w, backupData, force)
}

// handleListAutoBackups lists the automatic backups written by internal/backup into
// <config dir>/backups/, newest first, so the frontend can offer a "restore from automatic
// backup" list instead of a file-upload dialog (browsers can't be told which folder that dialog
// should start in - there's no API for it).
func handleListAutoBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	dir := filepath.Join(config.GetConfigDir(), "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// No backups directory yet (e.g. never connected to a box) is not an error - just nothing
		// to list.
		json.NewEncoder(w).Encode([]autoBackupInfo{})
		return
	}

	list := make([]autoBackupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), backup.FilePrefix) || !strings.HasSuffix(e.Name(), backup.FileSuffix) {
			continue
		}
		info := autoBackupInfo{Filename: e.Name()}
		timestampPart := strings.TrimSuffix(strings.TrimPrefix(e.Name(), backup.FilePrefix), backup.FileSuffix)
		if t, err := time.ParseInLocation(backup.TimestampForm, timestampPart, time.Local); err == nil {
			info.Timestamp = t.Format(time.RFC3339)
		}
		// Best-effort: peek into the file for which device it came from, to label the entry.
		if data, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
			var partial struct {
				FirmwareConfigSerial string `json:"firmwareConfigSerial"`
			}
			if json.Unmarshal(data, &partial) == nil {
				info.DeviceSerial = partial.FirmwareConfigSerial
				info.RigName = config.GetRigNameForSerial(partial.FirmwareConfigSerial)
			}
		}
		list = append(list, info)
	}
	// Filenames sort lexically in the same order as their embedded timestamp - newest first.
	sort.Slice(list, func(i, j int) bool { return list[i].Filename > list[j].Filename })
	json.NewEncoder(w).Encode(list)
}

// autoBackupInfo is one entry in the GET /api/v1/backup/list response.
type autoBackupInfo struct {
	Filename     string `json:"filename"`
	Timestamp    string `json:"timestamp"` // RFC3339, parsed from the filename; "" if unparseable
	DeviceSerial string `json:"deviceSerial"`
	RigName      string `json:"rigName"`
}

// handleRestoreAutoBackup restores one of the automatic backups listed by handleListAutoBackups,
// identified by filename, read directly off disk (never sent through the browser).
func handleRestoreAutoBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "Missing 'file' parameter", http.StatusBadRequest)
		return
	}

	dir := filepath.Join(config.GetConfigDir(), "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "Could not list automatic backups", http.StatusInternalServerError)
		return
	}
	// Only ever read a file whose name exactly matches one actually present in the backups
	// directory - filename comes from a query parameter, so this is the path-traversal guard
	// (a real directory entry's Name() never contains "/", "\", or "..").
	found := false
	for _, e := range entries {
		if !e.IsDir() && e.Name() == filename {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Backup file not found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		http.Error(w, "Failed to read backup file", http.StatusInternalServerError)
		return
	}
	var backupData config.CombinedConfig
	if err := json.Unmarshal(data, &backupData); err != nil {
		http.Error(w, "Invalid backup file format", http.StatusInternalServerError)
		return
	}
	if backupData.ProxyConfig == nil || backupData.FirmwareConfig == nil {
		http.Error(w, "Incomplete backup file", http.StatusInternalServerError)
		return
	}

	logger.Info("Restoring combined configuration from automatic backup %q...", filename)
	force := r.URL.Query().Get("force") == "true"
	applyBackupRestore(w, backupData, force)
}

// applyBackupRestore contains the actual restore logic (device-mismatch check, sending the
// firmware config to the device, restoring proxy settings, reconnecting) - shared by
// handleRestoreBackup (uploaded file) and handleRestoreAutoBackup (one of our own automatic
// backups), so the two entry points can never drift apart in behavior.
func applyBackupRestore(w http.ResponseWriter, backupData config.CombinedConfig, force bool) {
	// Guard against silently pushing one box's on-device settings (calibration offsets, heater
	// config, power startup states, etc.) onto a different, currently-connected box - confirmed
	// live against real hardware that this otherwise happens with no warning at all. A backup
	// from before FirmwareConfigSerial existed ("") can't be verified either way, so it's treated
	// the same as a genuine mismatch rather than assumed safe. ?force=true (sent only after the
	// user confirms a specific "different box detected" dialog, e.g. because they're deliberately
	// replacing one box with another) skips this check.
	currentSerial := config.GetActiveDeviceSerial()
	if !force && (backupData.FirmwareConfigSerial == "" || backupData.FirmwareConfigSerial != currentSerial) {
		logger.Warn("Backup restore blocked: backup's device (serial=%q) doesn't match the currently connected device (serial=%q). Retry with ?force=true to override.", backupData.FirmwareConfigSerial, currentSerial)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error":               "device_mismatch",
			"backupDeviceSerial":  backupData.FirmwareConfigSerial,
			"backupRigName":       config.GetRigNameForSerial(backupData.FirmwareConfigSerial),
			"currentDeviceSerial": currentSerial,
			"currentRigName":      config.GetActiveRigName(),
		})
		return
	}
	if force && (backupData.FirmwareConfigSerial == "" || backupData.FirmwareConfigSerial != currentSerial) {
		logger.Warn("Restoring a backup from a different/unknown device (serial=%q) onto the currently connected device (serial=%q) - overridden by user.", backupData.FirmwareConfigSerial, currentSerial)
	}

	// Restore Firmware Config
	compactFirmwareConfig, _ := json.Marshal(backupData.FirmwareConfig)
	firmwareCommand := fmt.Sprintf(`{"sc":%s}`, string(compactFirmwareConfig))
	if _, err := serial.SendCommand(firmwareCommand, true, 10*time.Second); err != nil {
		http.Error(w, fmt.Sprintf("Failed to send firmware config to device: %v", err), http.StatusServiceUnavailable)
		return
	}
	logger.Info("Firmware configuration restored successfully.")

	// Restore Proxy Config
	conf := config.Get()
	conf.NetworkPort = backupData.ProxyConfig.NetworkPort
	conf.ListenAddress = backupData.ProxyConfig.ListenAddress
	conf.LogLevel = backupData.ProxyConfig.LogLevel
	// See ProxyConfigMutex's doc comment (internal/config/config.go) - these are maps read
	// concurrently elsewhere, direct assignment here would race those readers.
	config.SetProxyMaps(backupData.ProxyConfig.SwitchNames, backupData.ProxyConfig.HeaterAutoEnableLeader, backupData.ProxyConfig.WeatherSourcePriority)
	// Restore every known device's profile wholesale (not just the currently active one) - this is
	// what makes a backup taken on one computer carry every box's names to another correctly.
	config.SetDeviceProfiles(backupData.ProxyConfig.DeviceProfiles)
	conf.HistoryRetentionNights = backupData.ProxyConfig.HistoryRetentionNights
	conf.TelemetryInterval = backupData.ProxyConfig.TelemetryInterval
	conf.EnableAlpacaVoltageControl = backupData.ProxyConfig.EnableAlpacaVoltageControl
	conf.EnableMasterPower = backupData.ProxyConfig.EnableMasterPower
	conf.AutoDetectPort = backupData.ProxyConfig.AutoDetectPort
	conf.EnableAutoBackup = backupData.ProxyConfig.EnableAutoBackup
	conf.AutoBackupRetentionCount = backupData.ProxyConfig.AutoBackupRetentionCount
	conf.SerialPortName = "" // Clear port to trigger auto-detection
	logger.Info("Serial port name cleared to trigger auto-detection.")
	logger.SetLevelFromString(conf.LogLevel)

	if err := config.Save(); err != nil {
		http.Error(w, "Failed to save proxy configuration", http.StatusInternalServerError)
		return
	}
	logger.Info("Proxy configuration restored successfully.")

	// Synchronously attempt to reconnect so the user comes back to a connected system
	logger.Info("Restore: Disconnecting current session...")
	serial.Reconnect("") // Ensure we are disconnected first to free the port

	// Give the OS a moment to release the serial port handle
	logger.Info("Restore: Waiting for port to release...")
	time.Sleep(1 * time.Second)

	logger.Info("Restore: attempting immediate auto-detection...")
	// FindAndConnect() finds and connects under the same lock the 5s watchdog uses for its own
	// auto-detect, instead of calling the unlocked FindPort() directly - avoids racing the
	// watchdog for the same USB port right after the disconnect above (harmless but noisy
	// "port busy" contention otherwise). It also adopts the already-open, already boot-settled
	// handle it finds instead of closing it and reopening from scratch, avoiding a redundant
	// reset pulse to the device.
	foundPort, err := serial.FindAndConnect()
	if err == nil {
		logger.Info("Restore: Immediate auto-detection found and connected to port '%s'.", foundPort)
		fmt.Fprintf(w, "Configuration restored successfully. Connected to %s.", foundPort)
	} else {
		logger.Warn("Restore: Immediate auto-detection failed: %v. Background task will retry.", err)
		// Leave it to the background task
		go serial.Reconnect("")
		fmt.Fprint(w, "Configuration restored successfully. Logic will retry connection in background.")
	}
}

// handleSerialRelease closes the serial port to allow external tools (e.g., web flasher) to access it.
func handleSerialRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("API request to release serial port received.")
	err := serial.ReleasePort()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Error("Failed to release serial port: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Serial port released. Auto-reconnect is paused.",
	})
}

// handleSerialResume resumes auto-reconnect after flashing is complete.
func handleSerialResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("API request to resume serial reconnect received.")
	serial.ResumeReconnect()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Auto-reconnect resumed.",
	})
}
