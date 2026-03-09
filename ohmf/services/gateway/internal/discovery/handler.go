package discovery

import (
    "encoding/json"
    "net/http"

    "ohmf/services/gateway/internal/middleware"
)

type requestBody struct {
    Algorithm string    `json:"algorithm"`
    Contacts  []Contact `json:"contacts"`
}

type responseBody struct {
    Matches []Match `json:"matches"`
}

type Handler struct{
    svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
    // require auth to reduce abuse
    if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    var body requestBody
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "invalid_json", http.StatusBadRequest)
        return
    }
    if body.Algorithm != "SHA256_PEPPERED_V1" {
        http.Error(w, "unsupported_algorithm", http.StatusBadRequest)
        return
    }
    matches, err := h.svc.Discover(r.Context(), body.Contacts)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(responseBody{Matches: matches})
}
