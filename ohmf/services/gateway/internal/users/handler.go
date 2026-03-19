package users

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

// Handler handles HTTP requests for user operations
type Handler struct {
	svc *Service
}

// NewHandler creates a handler for user operations
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ExportAccount exports user account data
func (h *Handler) ExportAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	result, err := h.svc.ExportAccount(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "export_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// DeleteAccount deletes all user account data
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	if err := h.svc.DeleteAccount(r.Context(), userID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// BlockUser creates a block relationship
func (h *Handler) BlockUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if err := h.svc.BlockUser(r.Context(), userID, req.UserID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "block_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnblockUser removes a block relationship
func (h *Handler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing user id", nil)
		return
	}
	if err := h.svc.UnblockUser(r.Context(), userID, targetID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "unblock_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListBlocked retrieves users blocked by the current user
func (h *Handler) ListBlocked(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	blocked, err := h.svc.ListBlockedUsers(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"blocked_users": blocked})
}

// GetMe retrieves current user profile
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	profile, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "user not found", nil)
		} else {
			httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, profile)
}

// UpdateMe updates current user profile
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		AvatarURL   *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	profile, err := h.svc.UpdateProfile(r.Context(), userID, req.DisplayName, req.AvatarURL)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "update_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, profile)
}

// ResolveProfiles batch fetches user profiles
func (h *Handler) ResolveProfiles(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	profiles, err := h.svc.ResolveProfiles(r.Context(), req.UserIDs)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "resolve_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}
