package telemetry

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/database"
	"sv241pro-alpaca-proxy/internal/logger"
)

type DataPoint struct {
	Timestamp int64   `json:"t"`
	Voltage   float64 `json:"v"`
	Current   float64 `json:"c"`
	Power     float64 `json:"p"`
	TempAmb   float64 `json:"temp"`
	HumAmb    float64 `json:"hum"`
	DewPoint  float64 `json:"dew"`
	TempLens  float64 `json:"lens"`
	PWM1      int     `json:"pwm1"`
	PWM2      int     `json:"pwm2"`
	DC1       int     `json:"dc1"`
	DC2       int     `json:"dc2"`
	DC3       int     `json:"dc3"`
	DC4       int     `json:"dc4"`
	DC5       int     `json:"dc5"`
	USBC12    int     `json:"usbc12"`
	USB345    int     `json:"usb345"`
	AdjConv   float64 `json:"adj_conv"`
}

// HandleGetHistory reads from the DB and returns JSON data.
func HandleGetHistory(w http.ResponseWriter, r *http.Request) {
	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")
	dateParam := r.URL.Query().Get("date")
	durationParam := r.URL.Query().Get("duration")

	var start, end int64
	end = time.Now().Unix()

	if startParam != "" && endParam != "" {
		// Custom range (unix timestamps)
		s, err1 := strconv.ParseInt(startParam, 10, 64)
		e, err2 := strconv.ParseInt(endParam, 10, 64)
		if err1 == nil && err2 == nil {
			start = s
			end = e
		} else {
			http.Error(w, "Invalid timestamp", http.StatusBadRequest)
			return
		}
	} else if dateParam != "" {
		// Specific date (full 24h of represented day)
		t, err := time.Parse("2006-01-02", dateParam)
		if err != nil {
			http.Error(w, "Invalid date format", http.StatusBadRequest)
			return
		}
		start = t.Unix()
		end = t.Add(24 * time.Hour).Unix()
	} else if durationParam != "" {
		// Parse duration like "12h"
		d, err := time.ParseDuration(durationParam)
		if err == nil {
			start = time.Now().Add(-d).Unix()
		} else {
			start = time.Now().Add(-12 * time.Hour).Unix() // Fallback
		}
	} else {
		start = time.Now().Add(-12 * time.Hour).Unix()
	}

	records, err := database.GetHistory(start, end)
	if err != nil {
		logger.Error("DB Query failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Downsampling if too many points
	// If more than 2000 points, take every Nth
	var result []DataPoint
	count := len(records)
	step := 1
	if count > 2000 {
		step = count / 2000
	}

	for i := 0; i < count; i += step {
		r := records[i]
		// Map DB record to API DataPoint
		// NOTE: API DataPoint struct is fixed, but frontend will only graph what it needs.
		// We could optimize by only filling requested fields, but for JSON it handles omitempty if we wanted.
		// For now send full object, it's not huge.
		result = append(result, DataPoint{
			Timestamp: r.Timestamp,
			Voltage:   r.Voltage,
			Current:   r.Current,
			Power:     r.Power,
			TempAmb:   r.TempAmb,
			HumAmb:    r.HumAmb,
			DewPoint:  r.DewPoint,
			TempLens:  r.TempLens,
			PWM1:      r.PWM1,
			PWM2:      r.PWM2,
			DC1:       r.DC1,
			DC2:       r.DC2,
			DC3:       r.DC3,
			DC4:       r.DC4,
			DC5:       r.DC5,
			USBC12:    r.USBC12,
			USB345:    r.USB345,
			AdjConv:   r.AdjConv,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleGetLogDates returns available dates from DB.
func HandleGetLogDates(w http.ResponseWriter, r *http.Request) {
	dates, err := database.GetDistinctDates()
	if err != nil {
		// Return empty list on error
		dates = []string{}
		logger.Error("Failed to get dates: %v", err)
	}
	if dates == nil {
		dates = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dates)
}

// HandleDownloadCSV generates a CSV from DB for the request date or range.
func HandleDownloadCSV(w http.ResponseWriter, r *http.Request) {
	startParam := r.URL.Query().Get("start")
	endParam := r.URL.Query().Get("end")
	dateParam := r.URL.Query().Get("date")
	colsParam := r.URL.Query().Get("cols") // comma-separated keys

	var start, end int64
	filename := "telemetry_export.csv"

	if startParam != "" && endParam != "" {
		s, err1 := strconv.ParseInt(startParam, 10, 64)
		e, err2 := strconv.ParseInt(endParam, 10, 64)
		if err1 == nil && err2 == nil {
			start = s
			end = e
			filename = fmt.Sprintf("telemetry_%d_%d.csv", start, end)
		} else {
			http.Error(w, "Invalid timestamp", http.StatusBadRequest)
			return
		}
	} else if dateParam != "" {
		t, err := time.Parse("2006-01-02", dateParam)
		if err != nil {
			http.Error(w, "Invalid date", http.StatusBadRequest)
			return
		}
		start = t.Unix()
		end = t.Add(24 * time.Hour).Unix()
		filename = fmt.Sprintf("telemetry_%s.csv", dateParam)
	} else {
		http.Error(w, "Missing range parameters", http.StatusBadRequest)
		return
	}

	records, err := database.GetHistory(start, end)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Parse columns
	// Available map: key -> label (or just key checking)
	// We want to write 'timestamp' always + selected columns.
	// If cols is empty, write all (legacy behavior).

	validCols := map[string]bool{
		"voltage": true, "current": true, "power": true,
		"t_amb": true, "h_amb": true, "dew_point": true, "t_lens": true,
		"pwm1": true, "pwm2": true,
		"dc1": true, "dc2": true, "dc3": true, "dc4": true, "dc5": true,
		"usbc12": true, "usb345": true, "adj_conv": true,
	}

	var selectedCols []string
	if colsParam != "" {
		// "voltage,raw_current,..."
		// Need to match csv header keys used in legacy
		// Legacy keys: timestamp,voltage,current,power,t_amb,h_amb,dew_point,t_lens,pwm1,pwm2,dc1...
		// Let's split colsParam
		// Let's split colsParam
		parts := strings.Split(colsParam, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if validCols[p] {
				selectedCols = append(selectedCols, p)
			}
		}
	}

	// If no valid cols selected, default to all
	if len(selectedCols) == 0 {
		selectedCols = []string{
			"voltage", "current", "power", "t_amb", "h_amb", "dew_point", "t_lens", "pwm1", "pwm2",
			"dc1", "dc2", "dc3", "dc4", "dc5", "usbc12", "usb345", "adj_conv",
		}
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	writer := csv.NewWriter(w)

	// Write Header with custom names
	// Format: key (customName) if custom name exists and differs from key
	proxyConf := config.Get()
	header := []string{"timestamp"}
	for _, col := range selectedCols {
		colHeader := col
		if proxyConf.SwitchNames != nil {
			if customName, exists := proxyConf.SwitchNames[col]; exists && customName != "" && customName != col {
				colHeader = fmt.Sprintf("%s (%s)", col, customName)
			}
		}
		header = append(header, colHeader)
	}
	writer.Write(header)

	for _, r := range records {
		ts := time.Unix(r.Timestamp, 0).Format(time.RFC3339)

		var row []string
		row = append(row, ts) // Timestamp first

		for _, col := range selectedCols {
			var val string
			switch col {
			case "voltage":
				val = fmt.Sprintf("%v", r.Voltage)
			case "current":
				val = fmt.Sprintf("%v", r.Current)
			case "power":
				val = fmt.Sprintf("%v", r.Power)
			case "t_amb":
				val = fmt.Sprintf("%v", r.TempAmb)
			case "h_amb":
				val = fmt.Sprintf("%v", r.HumAmb)
			case "dew_point":
				val = fmt.Sprintf("%v", r.DewPoint)
			case "t_lens":
				val = fmt.Sprintf("%v", r.TempLens)
			case "pwm1":
				val = fmt.Sprintf("%d", r.PWM1)
			case "pwm2":
				val = fmt.Sprintf("%d", r.PWM2)
			case "dc1":
				val = fmt.Sprintf("%d", r.DC1)
			case "dc2":
				val = fmt.Sprintf("%d", r.DC2)
			case "dc3":
				val = fmt.Sprintf("%d", r.DC3)
			case "dc4":
				val = fmt.Sprintf("%d", r.DC4)
			case "dc5":
				val = fmt.Sprintf("%d", r.DC5)
			case "usbc12":
				val = fmt.Sprintf("%d", r.USBC12)
			case "usb345":
				val = fmt.Sprintf("%d", r.USB345)
			case "adj_conv":
				val = fmt.Sprintf("%.1f", r.AdjConv)
			}
			row = append(row, val)
		}
		writer.Write(row)
	}
	writer.Flush()
}
