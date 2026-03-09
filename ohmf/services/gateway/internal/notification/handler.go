package notification

import (
    "encoding/json"
    "net/http"
)

type Handler struct{
    svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

type sendRequest struct{
    UserID string `json:"user_id"`
    Title  string `json:"title"`
    Body   string `json:"body"`
    Data   map[string]any `json:"data,omitempty"`
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
    var req sendRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid json", http.StatusBadRequest)
        return
    }
    if req.UserID == "" || req.Title == "" {
        http.Error(w, "missing fields", http.StatusBadRequest)
        return
    }
    payload := NotificationPayload{Title: req.Title, Body: req.Body, Data: req.Data}
    if err := h.svc.Send(r.Context(), req.UserID, payload); err != nil {
        http.Error(w, "failed to enqueue", http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusAccepted)
}
