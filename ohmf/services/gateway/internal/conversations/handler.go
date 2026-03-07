package conversations

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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		Type         string   `json:"type"`
		Participants []string `json:"participants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	c, err := h.svc.CreateDM(r.Context(), actor, req.Participants, req.Type)
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
	items, err := h.svc.List(r.Context(), actor)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
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
