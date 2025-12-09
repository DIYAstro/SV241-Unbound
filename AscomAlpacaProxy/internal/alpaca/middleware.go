package alpaca

import (
	"net/http"
	"strconv"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync/atomic"
)

var (
	ClientTransactionID uint32
	ServerTransactionID uint32
)

// Handler is a middleware that wraps HTTP handlers to provide Alpaca-specific functionality.
// It parses ClientTransactionID and ClientID from the request form.
func Handler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("HTTP Request: %s %s", r.Method, r.URL.Path)
		if err := r.ParseForm(); err != nil {
			logger.Warn("Error parsing form for request %s %s: %v", r.Method, r.URL.Path, err)
		}

		if txIDStr, ok := GetFormValueIgnoreCase(r, "ClientTransactionID"); ok {
			txID, _ := strconv.ParseUint(txIDStr, 10, 32)
			atomic.StoreUint32(&ClientTransactionID, uint32(txID))
		} else {
			atomic.StoreUint32(&ClientTransactionID, 0)
		}

		// We don't use ClientID, but we acknowledge its presence.
		if _, ok := GetFormValueIgnoreCase(r, "ClientID"); ok {
			// Acknowledged.
		}

		fn(w, r)
	}
}

// GetFormValueIgnoreCase retrieves the first value for a given key from the request form, case-insensitively.
// The ASCOM conformance checker requires case-sensitivity for PUT parameters, so we handle that.
func GetFormValueIgnoreCase(r *http.Request, key string) (string, bool) {
	if r.Method == "PUT" {
		if values, ok := r.Form[key]; ok {
			if len(values) > 0 {
				return values[0], true
			}
			return "", true // Key exists, but has no value.
		}
		return "", false // Key not found with correct case.
	}

	// For GET and other methods, be case-insensitive.
	for k, values := range r.Form {
		if strings.EqualFold(k, key) {
			if len(values) > 0 {
				return values[0], true
			}
			return "", true // Key exists but has no value.
		}
	}
	return "", false
}

// ParseSwitchID extracts and validates the 'Id' parameter from the request.
// It returns the integer ID and a boolean indicating success.
// If it returns false, it has already written an Alpaca error response.
func ParseSwitchID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr, ok := GetFormValueIgnoreCase(r, "Id")
	if !ok || idStr == "" {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Invalid or missing switch ID")
		return 0, false
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Invalid or missing switch ID")
		return 0, false
	}
	if _, ok := config.SwitchIDMap[id]; !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Invalid switch ID")
		return 0, false
	}

	// If ID is 10 (Master Power) and it's disabled, verify it's inaccessible
	if id == 10 && !config.Get().EnableMasterPower {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Invalid switch ID (Disabled)")
		return 0, false
	}

	return id, true
}
