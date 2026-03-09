package miniapp

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "ohmf/services/gateway/internal/httpx"
    "ohmf/services/gateway/internal/middleware"
)

type Handler struct{
    svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterManifest(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    var req struct{ Manifest any `json:"manifest"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    id, err := h.svc.RegisterManifest(r.Context(), userID, req.Manifest)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    _ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    var req struct{
        ManifestID string   `json:"manifest_id"`
        ConversationID string `json:"conversation_id"`
        Participants []string `json:"participants"`
        TTLSeconds int `json:"ttl_seconds"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    ttl := time.Duration(30) * time.Minute
    if req.TTLSeconds > 0 {
        ttl = time.Duration(req.TTLSeconds) * time.Second
    }
    id, err := h.svc.CreateSession(r.Context(), req.ManifestID, req.ConversationID, req.Participants, ttl)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "create_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    _ = json.NewEncoder(w).Encode(map[string]string{"session_id": id})
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    id := chi.URLParam(r, "id")
    if id == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
        return
    }
    s, err := h.svc.GetSession(r.Context(), id)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "not_found", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(s)
}

func (h *Handler) EndSession(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    id := chi.URLParam(r, "id")
    if id == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
        return
    }
    if err := h.svc.EndSession(r.Context(), id); err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "end_failed", err.Error(), nil)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AppendEvent(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    id := chi.URLParam(r, "id")
    if id == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
        return
    }
    var req struct{
        EventName string `json:"event_name"`
        Body any `json:"body"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if req.EventName == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "event_name required", nil)
        return
    }
    seq, err := h.svc.AppendEvent(r.Context(), id, userID, req.EventName, req.Body)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "append_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    _ = json.NewEncoder(w).Encode(map[string]any{"event_seq": seq})
}

func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    id := chi.URLParam(r, "id")
    if id == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
        return
    }
    var req struct{ State any `json:"state"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if err := h.svc.SnapshotSession(r.Context(), id, req.State, 0); err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "snapshot_failed", err.Error(), nil)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    items, err := h.svc.ListManifests(r.Context())
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
        return
    }
    httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
