package handlers

import (
	"encoding/json"
	"net/http"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
)

// AlpacaIDsResponse is the body of GET /api/v1/alpaca/ids and POST /api/v1/alpaca/regenerate-ids.
type AlpacaIDsResponse struct {
	IDs config.AlpacaDeviceIDs `json:"ids"`
	// Legacy is true while this install still advertises the IDs shared by every SV241 install
	// from before UniqueIDs were configurable - the Proxy tab uses it to suggest regenerating
	// when more than one box is on the network.
	Legacy bool `json:"legacy"`
}

func writeAlpacaIDs(w http.ResponseWriter, ids config.AlpacaDeviceIDs) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AlpacaIDsResponse{IDs: ids, Legacy: ids.IsLegacy()})
}

// HandleGetAlpacaIDs returns the ASCOM UniqueIDs this install advertises.
func HandleGetAlpacaIDs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeAlpacaIDs(w, config.GetAlpacaUniqueIDs())
}

// HandleRegenerateAlpacaIDs replaces all ASCOM UniqueIDs with fresh GUIDs and saves them.
func HandleRegenerateAlpacaIDs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ids, err := config.RegenerateAlpacaUniqueIDs()
	if err != nil {
		logger.Error("Failed to regenerate ASCOM Alpaca UniqueIDs: %v", err)
		http.Error(w, "Failed to save new ASCOM IDs", http.StatusInternalServerError)
		return
	}
	writeAlpacaIDs(w, ids)
}
