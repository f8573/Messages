package conversations

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/pagination"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		Type              string   `json:"type"`
		Participants      []string `json:"participants"`
		ParticipantPhones []string `json:"participant_phones"`
		Title             string   `json:"title"`
		AvatarURL         string   `json:"avatar_url"`
		EncryptionState   string   `json:"encryption_state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	c, err := h.svc.CreateConversation(r.Context(), actor, CreateRequest{
		Type:              req.Type,
		Participants:      req.Participants,
		ParticipantPhones: req.ParticipantPhones,
		Title:             req.Title,
		AvatarURL:         req.AvatarURL,
		EncryptionState:   req.EncryptionState,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "conversation_create_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	items, nextCursorValue, err := h.svc.List(r.Context(), actor, 100)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	var nextCursor string
	if nextCursorValue != "" {
		nextCursor = pagination.EncodeCursor(map[string]any{"updated_at": nextCursorValue})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) ListProjected(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	items, nextCursorValue, err := h.svc.ListProjected(r.Context(), actor, 100)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	var nextCursor string
	if nextCursorValue != "" {
		nextCursor = pagination.EncodeCursor(map[string]any{"updated_at": nextCursorValue})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	c, err := h.svc.Get(r.Context(), actor, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) CreatePhone(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		PhoneE164 string `json:"phone_e164"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	c, err := h.svc.FindOrCreatePhoneDM(r.Context(), actor, req.PhoneE164)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "conversation_create_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		TransportPolicy string `json:"transport_policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	c, err := h.svc.UpdateTransportPolicy(r.Context(), actor, id, req.TransportPolicy)
	if err != nil {
		if err == ErrNotFound {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		if err.Error() == "invalid_transport_policy" {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid transport_policy", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "update_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) SetThreadKeys(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var body struct {
		ThreadKeys []map[string]string `json:"thread_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if err := h.svc.SetThreadKeys(r.Context(), actor, id, body.ThreadKeys); err != nil {
		if err == ErrNotFound {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "set_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Nickname   *string `json:"nickname"`
		Closed     *bool   `json:"closed"`
		Archived   *bool   `json:"archived"`
		Pinned     *bool   `json:"pinned"`
		MutedUntil *string `json:"muted_until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	updated, err := h.svc.UpdatePreferences(r.Context(), actor, id, req.Nickname, req.Closed, req.Archived, req.Pinned, req.MutedUntil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "update_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) UpdateMetadata(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Title           *string `json:"title"`
		AvatarURL       *string `json:"avatar_url"`
		EncryptionState *string `json:"encryption_state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	updated, err := h.svc.UpdateMetadata(r.Context(), actor, id, req.Title, req.AvatarURL, req.EncryptionState)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		if errors.Is(err, ErrEncryptedConversationNotReady) {
			httpx.WriteError(w, r, http.StatusConflict, "encrypted_conversation_not_ready", "both DM participants must publish E2EE_OTT_V2 signal bundles before enabling encryption", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "update_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) AddMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	updated, err := h.svc.AddMembers(r.Context(), actor, id, req.UserIDs)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "member_update_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	targetUserID := chi.URLParam(r, "userID")
	updated, err := h.svc.RemoveMember(r.Context(), actor, id, targetUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "member_remove_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}
