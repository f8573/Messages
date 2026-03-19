package abuse

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

// removed: trivial constructor wrapper
func (h *Handler) Record(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())
	var req struct {
		TargetID  string         `json:"target_id"`
		EventType string         `json:"event_type"`
		IP        string         `json:"ip"`
		Details   map[string]any `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.EventType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "event_type required", nil)
		return
	}
	if err := h.svc.RecordEvent(r.Context(), userID, req.TargetID, req.EventType, req.IP, req.Details); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "record_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) GetScore(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "user id required", nil)
		return
	}
	score, err := h.svc.GetScore(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "score_error", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"score": score})
}

func (h *Handler) GetDestinationScore(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "phone query required", nil)
		return
	}
	score, err := h.svc.GetDestinationScore(r.Context(), phone)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "score_error", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"score": score})
}

func (h *Handler) CheckOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	allowed, err := h.svc.CheckOTPThrottle(r.Context(), req.Key, 1*time.Hour, 5)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "throttle_error", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"allowed": allowed})
}
