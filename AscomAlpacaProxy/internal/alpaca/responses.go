package alpaca

import (
	"encoding/json"
	"net/http"
	"sv241pro-alpaca-proxy/internal/logger"
	"sync/atomic"
)

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
		ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
		ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
	}
	writeResponse(w, r, resp)
}

func StringListResponse(w http.ResponseWriter, r *http.Request, value []string) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func ErrorResponse(w http.ResponseWriter, r *http.Request, errNum int, errMsg string) {
	httpStatus := http.StatusOK
	// The conformance checker expects HTTP 200 OK for Alpaca errors,
	// but using proper HTTP status codes is better practice for non-Alpaca clients.
	// We will stick to the spec for now.
	// if errNum >= 400 && errNum < 600 {
	// 	httpStatus = errNum
	// }

	resp := Response{
		ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
		ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		ErrorNumber:         errNum,
		ErrorMessage:        errMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	logger.Error("Alpaca request failed with error %d: %s", errNum, errMsg)
	json.NewEncoder(w).Encode(resp)
}

func StringResponse(w http.ResponseWriter, r *http.Request, value string) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func IntResponse(w http.ResponseWriter, r *http.Request, value int) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func FloatResponse(w http.ResponseWriter, r *http.Request, value float64) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}

func BoolResponse(w http.ResponseWriter, r *http.Request, value bool) {
	resp := ValueResponse{
		Response: Response{
			ClientTransactionID: atomic.LoadUint32(&ClientTransactionID),
			ServerTransactionID: atomic.AddUint32(&ServerTransactionID, 1),
		},
		Value: value,
	}
	writeResponse(w, r, resp)
}
