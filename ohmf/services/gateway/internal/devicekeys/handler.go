package devicekeys

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

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	deviceID := chi.URLParam(r, "deviceID")
	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	bundle, err := h.svc.PublishBundle(r.Context(), actor, deviceID, req)
	if err != nil {
		if errors.Is(err, ErrDeviceNotOwned) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "device not owned by actor", nil)
			return
		}
		if errors.Is(err, ErrDeviceCapabilityRequired) {
			httpx.WriteError(w, r, http.StatusConflict, "device_capability_required", "device must support E2EE_OTT_V2", nil)
			return
		}
		if errors.Is(err, ErrInvalidBundle) || errors.Is(err, ErrEncryptedPrekeysInsufficient) {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "publish_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bundle)
}

func (h *Handler) AddPrekeys(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	deviceID := chi.URLParam(r, "deviceID")
	var body struct {
		OneTimePrekeys []OneTimePrekey `json:"one_time_prekeys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	bundle, err := h.svc.AddOneTimePrekeys(r.Context(), actor, deviceID, body.OneTimePrekeys)
	if err != nil {
		if errors.Is(err, ErrDeviceNotOwned) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "device not owned by actor", nil)
			return
		}
		if errors.Is(err, ErrDeviceCapabilityRequired) {
			httpx.WriteError(w, r, http.StatusConflict, "device_capability_required", "device must support E2EE_OTT_V2", nil)
			return
		}
		if errors.Is(err, ErrInvalidBundle) || errors.Is(err, ErrEncryptedPrekeysRequired) {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "prekey_add_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bundle)
}

func (h *Handler) ListForUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	userID := chi.URLParam(r, "userID")
	items, err := h.svc.ListBundlesForUser(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) ClaimForUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	userID := chi.URLParam(r, "userID")
	items, err := h.svc.ClaimBundles(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "claim_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
