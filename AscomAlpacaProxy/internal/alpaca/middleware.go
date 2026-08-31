package alpaca

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
)

// ServerTransactionID is a genuinely global, ever-incrementing counter (per the Alpaca spec, it
// just needs to be unique across the server's lifetime) - safe as shared state, unlike
// ClientTransactionID below.
var ServerTransactionID uint32

// clientTransactionIDKey is the context key ClientTransactionID is stored under - see Handler and
// clientTransactionIDFromContext (responses.go). A request-scoped context value, not the package
// -level atomic variable this used to be: that let one request's ClientTransactionID leak into a
// concurrently-running request's response, since both would read whatever the shared variable
// happened to hold at that moment rather than what they were each sent with.
type clientTransactionIDKeyType struct{}

var clientTransactionIDKey = clientTransactionIDKeyType{}

// Handler is a middleware that wraps HTTP handlers to provide Alpaca-specific functionality.
// It parses ClientTransactionID and ClientID from the request form.
func Handler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("HTTP Request: %s %s", r.Method, r.URL.Path)
		if err := r.ParseForm(); err != nil {
			logger.Warn("Error parsing form for request %s %s: %v", r.Method, r.URL.Path, err)
		}

		var txID uint64
		if txIDStr, ok := GetFormValueIgnoreCase(r, "ClientTransactionID"); ok {
			txID, _ = strconv.ParseUint(txIDStr, 10, 32)
		}
		ctx := context.WithValue(r.Context(), clientTransactionIDKey, uint32(txID))
		r = r.WithContext(ctx)

		// We don't use ClientID, but we acknowledge its presence.
		if _, ok := GetFormValueIgnoreCase(r, "ClientID"); ok {
			// Acknowledged.
		}

		fn(w, r)
	}
}

// GetFormValueIgnoreCase retrieves the first value for a given key from the request form, case-insensitively.
// The Alpaca specification requires parameter names to be case-insensitive.
func GetFormValueIgnoreCase(r *http.Request, key string) (string, bool) {
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
	if _, ok := config.GetSwitchIDMapEntry(id); !ok {
		ErrorResponse(w, r, http.StatusOK, 0x400, "Invalid switch ID")
		return 0, false
	}

	return id, true
}
