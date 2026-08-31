package alpaca

import (
	"encoding/json"
	"math"
	"net/http"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync/atomic"
)

// clientTransactionIDFromContext reads the ClientTransactionID the middleware (Handler,
// middleware.go) stashed in this specific request's context - request-scoped, so concurrent
// requests each get back the value they were sent with, not whatever a different in-flight
// request happened to store last (see clientTransactionIDKey's doc comment).
func clientTransactionIDFromContext(r *http.Request) uint32 {
	if v, ok := r.Context().Value(clientTransactionIDKey).(uint32); ok {
		return v
	}
	return 0
}

// --- Response Structs ---

type Response struct {
	ClientTransactionID uint32 `json:"ClientTransactionID"`
	ServerTransactionID uint32 `json:"ServerTransactionID"`
	ErrorNumber         int    `json:"ErrorNumber"`
	ErrorMessage        string `json:"ErrorMessage"`
}

type ValueResponse struct {
	Response
	Value interface{} `json:"Value"`
}

// --- Management API Response ---

// ManagementValueResponse is for management endpoints that don't use the standard handler.
func ManagementValueResponse(w http.ResponseWriter, r *http.Request, value interface{}) {
	response := struct {
		Value               interface{} `json:"Value"`
		ClientTransactionID uint32      `json:"ClientTransactionID"`
		ServerTransactionID uint32      `json:"ServerTransactionID"`
		ErrorNumber         int         `json:"ErrorNumber"`
		ErrorMessage        string      `json:"ErrorMessage"`
	}{
		Value:               value,
		ClientTransactionID: 0, // Not available in this context
		ServerTransactionID: 0, // Not stateful for this endpoint
		ErrorNumber:         0,
		ErrorMessage:        "",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- Standard Alpaca Responses ---

func writeResponse(w http.ResponseWriter, r *http.Request, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func EmptyResponse(w http.ResponseWriter, r *http.Request) {
	resp := Response{
		ClientTransactionID: clientTransactionIDFromContext(r),
		ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
	}
	writeResponse(w, r, resp)
}

func StringListResponse(w http.ResponseWriter, r *http.Request, value []string) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func ErrorResponse(w http.ResponseWriter, r *http.Request, httpStatus int, errNum int, errMsg string) {
	resp := Response{
		ClientTransactionID: clientTransactionIDFromContext(r),
		ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		ErrorNumber:         errNum,
		ErrorMessage:        errMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	// Filter "Not Implemented" errors (ASCOM 0x400 = 1024, 0x40C = 1036)
	// These are normal during device discovery and should not be logged as ERROR.
	if errNum == 0x400 || errNum == 0x40C {
		logger.Debug("Alpaca request 'Not Implemented' (normal behavior) - Status %d, Error %d: %s", httpStatus, errNum, errMsg)
	} else {
		logger.Error("Alpaca request failed with HTTP status %d, error %d: %s", httpStatus, errNum, errMsg)
	}

	json.NewEncoder(w).Encode(resp)
}

func StringResponse(w http.ResponseWriter, r *http.Request, value string) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func IntResponse(w http.ResponseWriter, r *http.Request, value int) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func FloatResponse(w http.ResponseWriter, r *http.Request, value float64) {
	// Round to 2 decimal places to avoid IEEE 754 floating-point precision issues
	// (e.g., 3.4 showing as 3.4000000000000004)
	// IMPORTANT: Use RawFloatResponse for high-precision dates!
	value = math.Round(value*100) / 100

	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func RawFloatResponse(w http.ResponseWriter, r *http.Request, value float64) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func InvalidValueResponse(w http.ResponseWriter, r *http.Request, errNum int, errMsg string) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
			ErrorNumber:         errNum,
			ErrorMessage:        errMsg,
		},
		Value: nil, // Use nil for the value in an invalid value response
	}
	writeResponse(w, r, resp)
}

func BoolResponse(w http.ResponseWriter, r *http.Request, value bool) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: clientTransactionIDFromContext(r),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}
