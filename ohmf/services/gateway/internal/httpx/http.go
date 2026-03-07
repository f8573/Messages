package httpx

import (
	"encoding/json"
	"net/http"
)

type ErrorEnvelope struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	reqID := r.Header.Get("X-Request-Id")
	WriteJSON(w, status, ErrorEnvelope{
		Code:      code,
		Message:   message,
		RequestID: reqID,
		Details:   details,
	})
}
