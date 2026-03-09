package messages

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
	"strconv"
	"ohmf/services/gateway/internal/observability"

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
		ClientGeneratedID string      `json:"client_generated_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.IdempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "idempotency_key required", nil)
		return
	}

	// Validate required fields per OpenAPI: conversation_id, content_type, content
	if req.ConversationID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "conversation_id required", nil)
		return
	}
	if req.ContentType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "content_type required", nil)
		return
	}
	if req.Content == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "content required", nil)
		return
	}
	traceID := buildTraceID(chimiddleware.GetReqID(r.Context()))
	result, err := h.svc.Send(r.Context(), userID, req.ConversationID, req.IdempotencyKey, req.ContentType, req.Content, req.ClientGeneratedID, traceID, ipOnly(r.RemoteAddr))
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
	httpStatus := http.StatusCreated
	if result.Queued {
		httpStatus = http.StatusAccepted
	}
	// Normalize transport and status to match OpenAPI contract.
	transport := result.Message.Transport
	switch transport {
	case "OTT":
		transport = "OHMF"
	case "":
		transport = "OHMF"
	}

	messageStatus := result.Message.Status
	if messageStatus == "" {
		messageStatus = "SENT"
	}

	response := map[string]any{
		"message_id":   result.Message.MessageID,
		"server_order": result.Message.ServerOrder,
		"transport":    transport,
		"status":       messageStatus,
		"queued":       result.Queued,
	}
	if result.Queued {
		response["ack_timeout_ms"] = result.AckTimeoutMS
	}
	httpx.WriteJSON(w, httpStatus, response)
	// Emit structured event for observability
	observability.EmitEvent("message.created", map[string]any{
		"message_id": result.Message.MessageID,
		"conversation_id": req.ConversationID,
		"sender_user_id": userID,
		"transport": result.Message.Transport,
		"server_order": result.Message.ServerOrder,
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
		ClientGeneratedID string      `json:"client_generated_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.PhoneE164 == "" || req.IdempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "phone_e164 and idempotency_key required", nil)
		return
	}

	// Validate required fields per OpenAPI: content_type, content
	if req.ContentType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "content_type required", nil)
		return
	}
	if req.Content == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "content required", nil)
		return
	}
	traceID := buildTraceID(chimiddleware.GetReqID(r.Context()))
	result, err := h.svc.SendToPhone(r.Context(), userID, req.PhoneE164, req.IdempotencyKey, req.ContentType, req.Content, req.ClientGeneratedID, traceID, ipOnly(r.RemoteAddr))
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
	httpStatus := http.StatusCreated
	if result.Queued {
		httpStatus = http.StatusAccepted
	}
	transport := result.Message.Transport
	if transport == "" {
		transport = "SMS"
	}
	// Map any internal transport names to API-facing enums
	if transport == "OTT" {
		transport = "OHMF"
	}

	messageStatus := result.Message.Status
	if messageStatus == "" {
		messageStatus = "SENT"
	}

	response := map[string]any{
		"message_id":      result.Message.MessageID,
		"conversation_id": result.Message.ConversationID,
		"server_order":    result.Message.ServerOrder,
		"transport":       transport,
		"status":          messageStatus,
		"queued":          result.Queued,
	}
	if result.Queued {
		response["ack_timeout_ms"] = result.AckTimeoutMS
	}
	httpx.WriteJSON(w, httpStatus, response)
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

func (h *Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	// optional limit param
	q := r.URL.Query().Get("limit")
	limit := 100
	if q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}
	items, err := h.svc.ListUnified(r.Context(), userID, id, limit)
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

func (h *Handler) RecordDelivery(w http.ResponseWriter, r *http.Request) {
	// Intentionally allow authenticated services to post delivery updates
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	var body struct {
		RecipientUserID   string `json:"recipient_user_id,omitempty"`
		RecipientDeviceID string `json:"recipient_device_id,omitempty"`
		RecipientPhone    string `json:"recipient_phone_e164,omitempty"`
		Transport         string `json:"transport"`
		State             string `json:"state"`
		Provider          string `json:"provider,omitempty"`
		SubmittedAt       string `json:"submitted_at,omitempty"`
		FailureCode       string `json:"failure_code,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	dr := DeliveryRecord{
		MessageID:         id,
		RecipientUserID:   body.RecipientUserID,
		RecipientDeviceID: body.RecipientDeviceID,
		RecipientPhone:    body.RecipientPhone,
		Transport:         body.Transport,
		State:             body.State,
		Provider:          body.Provider,
		SubmittedAt:       body.SubmittedAt,
		FailureCode:       body.FailureCode,
	}
	if err := h.svc.RecordDelivery(r.Context(), id, dr); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "record_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	// ensure membership by reusing existing List authorization check
	// reuse List to validate access: List will return error if not a member
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	// Find the conversation id for the message to enforce membership
	var convID string
	if err := h.svc.db.QueryRow(r.Context(), `SELECT conversation_id::text FROM messages WHERE id = $1`, id).Scan(&convID); err != nil {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "message not found", nil)
		return
	}
	if ok, err := h.svc.hasMembership(r.Context(), h.svc.db, userID, convID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "check_failed", err.Error(), nil)
		return
	} else if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
		return
	}
	items, err := h.svc.ListDeliveries(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
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

func (h *Handler) Redact(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	if err := h.svc.Redact(r.Context(), userID, id); err != nil {
		if err.Error() == "forbidden" {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not allowed to redact", nil)
			return
		}
		if err.Error() == "message_not_found" {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "message not found", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "redact_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	if err := h.svc.DeleteMessage(r.Context(), userID, id); err != nil {
		if err.Error() == "forbidden" {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not allowed to delete", nil)
			return
		}
		if err.Error() == "message_not_found" {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "message not found", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddReaction(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	var body struct{
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if body.Emoji == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "emoji required", nil)
		return
	}
	if err := h.svc.AddReaction(r.Context(), userID, id, body.Emoji); err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "add_reaction_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveReaction(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "message id required", nil)
		return
	}
	var body struct{
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if body.Emoji == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "emoji required", nil)
		return
	}
	if err := h.svc.RemoveReaction(r.Context(), userID, id, body.Emoji); err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "remove_reaction_failed", err.Error(), nil)
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
