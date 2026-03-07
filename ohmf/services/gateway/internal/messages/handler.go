package messages

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		ConversationID string         `json:"conversation_id"`
		IdempotencyKey string         `json:"idempotency_key"`
		ContentType    string         `json:"content_type"`
		Content        map[string]any `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.IdempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "idempotency_key required", nil)
		return
	}
	msg, err := h.svc.Send(r.Context(), userID, req.ConversationID, req.IdempotencyKey, req.ContentType, req.Content)
	if err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "send_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"message_id":   msg.MessageID,
		"server_order": msg.ServerOrder,
		"status": map[string]string{
			"send":     "STORED",
			"delivery": "PENDING",
			"read":     "UNREAD",
		},
	})
}

func (h *Handler) SendToPhone(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		PhoneE164      string         `json:"phone_e164"`
		IdempotencyKey string         `json:"idempotency_key"`
		ContentType    string         `json:"content_type"`
		Content        map[string]any `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.PhoneE164 == "" || req.IdempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "phone_e164 and idempotency_key required", nil)
		return
	}
	msg, err := h.svc.SendToPhone(r.Context(), userID, req.PhoneE164, req.IdempotencyKey, req.ContentType, req.Content)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "send_phone_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"message_id":      msg.MessageID,
		"conversation_id": msg.ConversationID,
		"server_order":    msg.ServerOrder,
		"transport":       "SMS",
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	items, err := h.svc.List(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		ThroughServerOrder int64 `json:"through_server_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if err := h.svc.MarkRead(r.Context(), userID, id, req.ThroughServerOrder); err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "mark_read_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
