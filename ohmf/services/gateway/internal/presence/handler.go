package presence

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "ohmf/services/gateway/internal/httpx"
    "ohmf/services/gateway/internal/middleware"
    "context"
)

type Handler struct{
    svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    target := chi.URLParam(r, "id")
    if target == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "user id required", nil)
        return
    }
    online, err := h.svc.IsUserOnline(context.Background(), target)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
        return
    }
    status := "offline"
    if online {
        status = "online"
    }
    _ = json.NewEncoder(w).Encode(map[string]any{"user_id": target, "status": status})
}

func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    conv := chi.URLParam(r, "id")
    if conv == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "conversation id required", nil)
        return
    }
    users, err := h.svc.ConversationOnlineUsers(context.Background(), conv)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
        return
    }
    _ = json.NewEncoder(w).Encode(map[string]any{"conversation_id": conv, "online_users": users})
}
