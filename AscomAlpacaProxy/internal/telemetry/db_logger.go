package telemetry

import (
	"os"
	"path/filepath"
	"time"

	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/database"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
)

// Init initializes the database logging system.
func Init() {
	configDir, _ := os.UserConfigDir()
	appDir := filepath.Join(configDir, "SV241AlpacaProxy")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		logger.Error("Failed to create app directory: %v", err)
		return
	}

	dbPath := filepath.Join(appDir, "alpaca_proxy.db")
	if err := database.Init(dbPath); err != nil {
		logger.Error("Failed to initialize database: %v", err)
		return
	}

	// Prune old data based on config
	conf := config.Get()
	if conf.HistoryRetentionNights > 0 {
		if err := database.PruneOldTelemetry(conf.HistoryRetentionNights); err != nil {
			logger.Error("Failed to prune old telemetry: %v", err)
		}
	} else {
		// Default retention of 30 days if not set or 0?
		// Legacy code: PruneOldFiles(logsDir, config.Get().HistoryRetentionNights)
		// If config is 0, maybe it means keep forever?
		// Let's stick to config.
	}

	// Start the logging loop (unless disabled)
	go loggingLoop()
}

func loggingLoop() {
	conf := config.Get()
	interval := time.Duration(conf.TelemetryInterval) * time.Second

	// If interval is 0, telemetry logging is disabled
	if interval <= 0 {
		logger.Info("Database Telemetry Logging disabled (interval=0).")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("Database Telemetry Logging started. Interval: %v", interval)

	for range ticker.C {
		logTelemetry()
	}
}

func logTelemetry() {
	// 1. Get current conditions (thread-safe copy)
	serial.Conditions.RLock()
	data := serial.Conditions.Data
	serial.Conditions.RUnlock()

	if data == nil {
		return // No data yet
	}

	// Helper to safely get float
	getFloat := func(key string) float64 {
		if v, ok := data[key]; ok {
			// It might be float64 or int or string depending on JSON unmarshal, usually float64
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			default:
				return 0
			}
		}
		return 0
	}

	getInt := func(key string) int {
		if v, ok := data[key]; ok {
			switch val := v.(type) {
			case float64:
				return int(val)
			case int:
				return val
			default:
				return 0
			}
		}
		return 0
	}

	record := database.TelemetryRecord{
		Timestamp: time.Now().Unix(),
		Voltage:   getFloat("v"),
		Current:   getFloat("i"),
		Power:     getFloat("p"),
		TempAmb:   getFloat("t_amb"),
		HumAmb:    getFloat("h_amb"),
		DewPoint:  getFloat("d"),
		TempLens:  getFloat("t_lens"),
		PWM1:      getInt("pwm1"),
		PWM2:      getInt("pwm2"),
	}

	// Add switch states
	serial.Status.RLock()
	statusData := serial.Status.Data
	serial.Status.RUnlock()

	if statusData != nil {
		// Helper for switches
		getSwitch := func(shortKey string) int {
			if val, ok := statusData[shortKey]; ok {
				switch v := val.(type) {
				case bool:
					if v {
						return 1
					} else {
						return 0
					}
				case float64:
					if v >= 1.0 {
						return 1
					}
					return 0
				default:
					return 0
				}
			}
			return 0
		}

		// Map from config keys. We hardcode mapping here or use the map logic from before?
		// The previous logic used config.ShortSwitchIDMap.
		// Let's assume standard names based on previous implementation.
		// Layout: dc1..dc5, usbc12, usb345, adj_conv

		// We need to resolve long names to short keys again?
		// Actually, `serial.Status.Data` uses short keys (e.g. "S1", "S2").
		// To be robust, we need to look up which switch corresponds to "dc1".
		// For now, let's use the same look up logic as before strictly if we want to be correct.

		config.SwitchMapMutex.RLock()
		// Replicate logic: iterate switch order, find short key
		switchOrder := []string{"dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv"}
		for i, longKey := range switchOrder {
			shortKey := config.ShortSwitchIDMap[longKey]
			val := 0

			// Check existence
			isEnabled := false
			for _, k := range config.SwitchIDMap {
				if k == longKey {
					isEnabled = true
					break
				}
			}

			if isEnabled {
				if longKey == "adj_conv" {
					if v, ok := statusData[shortKey]; ok {
						if f, ok := v.(float64); ok {
							record.AdjConv = f
						}
					}
				} else {
					val = getSwitch(shortKey)
					// Assign to correct field
					switch i {
					case 0:
						record.DC1 = val
					case 1:
						record.DC2 = val
					case 2:
						record.DC3 = val
					case 3:
						record.DC4 = val
					case 4:
						record.DC5 = val
					case 5:
						record.USBC12 = val
					case 6:
						record.USB345 = val
					}
				}
			}
		}
		config.SwitchMapMutex.RUnlock()
	}

	if err := database.InsertTelemetry(record); err != nil {
		logger.Error("Failed to insert telemetry: %v", err)
	}
}
