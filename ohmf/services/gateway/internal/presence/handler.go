package presence

import (
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

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	userID := chi.URLParam(r, "userID")
	item, err := h.svc.GetUserPresence(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "presence_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	actorUserID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	items, err := h.svc.GetConversationPresence(r.Context(), actorUserID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, ErrConversationAccess) {
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "not a member", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "presence_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
