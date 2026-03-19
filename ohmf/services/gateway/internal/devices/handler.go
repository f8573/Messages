package devices

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var d Device
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	id, err := h.svc.RegisterDevice(r.Context(), userID, d)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"device_id": id})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	deviceID := chi.URLParam(r, "id")
	if deviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing device id", nil)
		return
	}
	var d Device
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	device, err := h.svc.UpdateDevice(r.Context(), userID, deviceID, d)
	if err != nil {
		status := http.StatusInternalServerError
		code := "update_failed"
		if err == ErrDeviceNotFound {
			status = http.StatusNotFound
			code = "device_not_found"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, device)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	ds, err := h.svc.ListDevices(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": ds})
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing device id", nil)
		return
	}
	if err := h.svc.RevokeDevice(r.Context(), userID, id); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "revoke_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
