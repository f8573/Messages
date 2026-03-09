package media

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

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    var req struct{
        AttachmentID string `json:"attachment_id"`
        MessageID string `json:"message_id"`
        ObjectKey string `json:"object_key"`
        MimeType string `json:"mime_type"`
        Size int64 `json:"size_bytes"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if req.AttachmentID == "" || req.MessageID == "" || req.ObjectKey == "" || req.MimeType == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing fields", nil)
        return
    }
    if err := h.svc.RegisterAttachment(r.Context(), req.AttachmentID, req.MessageID, req.ObjectKey, req.MimeType, req.Size); err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
        return
    }
    w.WriteHeader(http.StatusCreated)
}

func (h *Handler) Purge(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    id := chi.URLParam(r, "id")
    if id == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "attachment id required", nil)
        return
    }
    key, err := h.svc.PurgeAttachment(r.Context(), id)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "purge_failed", err.Error(), nil)
        return
    }
    h.svc.notifyObjectDeletion(key)
    w.WriteHeader(http.StatusNoContent)
}

// CreateUploadToken returns a short-lived upload token and an upload URL.
func (h *Handler) CreateUploadToken(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    var req struct{
        AttachmentID string `json:"attachment_id"`
        MimeType string `json:"mime_type"`
        Size int64 `json:"size_bytes"`
        TTLSeconds int `json:"ttl_seconds"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if req.AttachmentID == "" || req.MimeType == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing fields", nil)
        return
    }
    ttl := time.Duration(15) * time.Minute
    if req.TTLSeconds > 0 {
        ttl = time.Duration(req.TTLSeconds) * time.Second
    }
    token, url, err := h.svc.CreateUploadToken(r.Context(), req.AttachmentID, req.MimeType, req.Size, ttl)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "token_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    _ = json.NewEncoder(w).Encode(map[string]string{"token": token, "upload_url": url})
}

// CompleteUpload marks an upload token as completed. Clients or upload workers
// call this once the object is durably stored.
func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
    _, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    token := chi.URLParam(r, "token")
    if token == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "token required", nil)
        return
    }
    var req struct{ ObjectKey string `json:"object_key"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    _, err := h.svc.CompleteUpload(r.Context(), token, req.ObjectKey)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "complete_failed", err.Error(), nil)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
