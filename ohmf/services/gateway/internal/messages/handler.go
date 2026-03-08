package messages

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/limit"
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
	traceID := buildTraceID(chimiddleware.GetReqID(r.Context()))
	result, err := h.svc.Send(r.Context(), userID, req.ConversationID, req.IdempotencyKey, req.ContentType, req.Content, traceID, ipOnly(r.RemoteAddr))
	if err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
			return
		}
		var rlErr RateLimitError
		if errors.As(err, &rlErr) {
			decision := limit.Decision{Allowed: false, RetryAfter: time.Duration(retryAfterOrDefault(rlErr.RetryAfter)) * time.Millisecond}
			limit.SetHeaders(w, scopeLimit(rlErr.Scope), decision)
			httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", err.Error(), map[string]any{
				"scope":          rlErr.Scope,
				"retry_after_ms": retryAfterOrDefault(rlErr.RetryAfter),
			})
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "send_failed", err.Error(), nil)
		return
	}
	status := http.StatusCreated
	if result.Queued {
		status = http.StatusAccepted
	}
	response := map[string]any{
		"message_id":   result.Message.MessageID,
		"server_order": result.Message.ServerOrder,
		"transport":    "OHMF",
		"status":       "SENT",
		"queued":       result.Queued,
	}
	if result.Queued {
		response["ack_timeout_ms"] = result.AckTimeoutMS
	}
	httpx.WriteJSON(w, status, response)
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
	traceID := buildTraceID(chimiddleware.GetReqID(r.Context()))
	result, err := h.svc.SendToPhone(r.Context(), userID, req.PhoneE164, req.IdempotencyKey, req.ContentType, req.Content, traceID, ipOnly(r.RemoteAddr))
	if err != nil {
		var rlErr RateLimitError
		if errors.As(err, &rlErr) {
			decision := limit.Decision{Allowed: false, RetryAfter: time.Duration(retryAfterOrDefault(rlErr.RetryAfter)) * time.Millisecond}
			limit.SetHeaders(w, scopeLimit(rlErr.Scope), decision)
			httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", err.Error(), map[string]any{
				"scope":          rlErr.Scope,
				"retry_after_ms": retryAfterOrDefault(rlErr.RetryAfter),
			})
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "send_phone_failed", err.Error(), nil)
		return
	}
	status := http.StatusCreated
	if result.Queued {
		status = http.StatusAccepted
	}
	response := map[string]any{
		"message_id":      result.Message.MessageID,
		"conversation_id": result.Message.ConversationID,
		"server_order":    result.Message.ServerOrder,
		"transport":       "SMS",
		"status":          "SENT",
		"queued":          result.Queued,
	}
	if result.Queued {
		response["ack_timeout_ms"] = result.AckTimeoutMS
	}
	httpx.WriteJSON(w, status, response)
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

func retryAfterOrDefault(v time.Duration) int64 {
	if v <= 0 {
		return 1000
	}
	return v.Milliseconds()
}

func scopeLimit(scope string) int64 {
	switch scope {
	case "user":
		return 60
	case "conversation":
		return 500
	case "ip":
		return 240
	default:
		return 0
	}
}

func ipOnly(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
