package sync

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Incremental(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	resp, err := h.svc.IncrementalSync(r.Context(), cursor, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "sync_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) IncrementalV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 200
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	resp, err := h.svc.IncrementalSyncV2(r.Context(), userID, cursor, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "sync_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkDeliveredV2(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	conversationID := chi.URLParam(r, "id")
	var req struct {
		ThroughServerOrder int64  `json:"through_server_order"`
		DeviceID           string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if err := h.svc.MarkDelivered(r.Context(), userID, req.DeviceID, conversationID, req.ThroughServerOrder); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "mark_delivered_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
