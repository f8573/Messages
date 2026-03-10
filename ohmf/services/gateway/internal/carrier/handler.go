package carrier

import (
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/go-chi/chi/v5"
    "ohmf/services/gateway/internal/httpx"
    "ohmf/services/gateway/internal/middleware"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/google/uuid"
)

type Handler struct{
    svc ServiceAPI
    db  *pgxpool.Pool
}

func NewHandler(svc ServiceAPI, db *pgxpool.Pool) *Handler { return &Handler{svc: svc, db: db} }

// POST /v1/carrier/messages/import
func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    var req struct{
        DeviceID string `json:"device_id"`
        ThreadKey string `json:"thread_key"`
        CarrierMessageID string `json:"carrier_message_id"`
        Direction string `json:"direction"`
        Transport string `json:"transport"`
        Text string `json:"text"`
        Media any `json:"media"
        `
        RawPayload any `json:"raw_payload"`
        CreatedAt string `json:"created_at"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if req.DeviceID == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "device_id required", nil)
        return
    }
    // verify device belongs to user
    var exists bool
    if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1::uuid AND user_id = $2::uuid)`, req.DeviceID, userID).Scan(&exists); err != nil || !exists {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "device not found", nil)
        return
    }

    var mediaJSON []byte
    if req.Media != nil {
        if b, err := json.Marshal(req.Media); err == nil {
            mediaJSON = b
        }
    }
    var rawJSON []byte
    if req.RawPayload != nil {
        if b, err := json.Marshal(req.RawPayload); err == nil {
            rawJSON = b
        }
    }
    createdAt := time.Now()
    if req.CreatedAt != "" {
        if t, err := time.Parse(time.RFC3339, req.CreatedAt); err == nil {
            createdAt = t
        }
    }

    cm := CarrierMessage{
        DeviceID: req.DeviceID,
        ThreadKey: req.ThreadKey,
        CarrierMessageID: req.CarrierMessageID,
        Direction: req.Direction,
        Transport: req.Transport,
        Text: req.Text,
        MediaJSON: mediaJSON,
        RawPayload: rawJSON,
        CreatedAt: createdAt,
    }
    out, err := h.svc.ImportCarrierMessage(r.Context(), req.DeviceID, cm)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "import_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    _ = json.NewEncoder(w).Encode(out)
}

// GET /v1/carrier/messages?since=...&limit=...
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    deviceID := r.URL.Query().Get("device_id")
    if deviceID == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "device_id required", nil)
        return
    }
    // verify device ownership
    var exists bool
    if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1::uuid AND user_id = $2::uuid)`, deviceID, userID).Scan(&exists); err != nil || !exists {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "device not found", nil)
        return
    }

    sinceStr := r.URL.Query().Get("since")
    var since time.Time
    if sinceStr == "" {
        since = time.Unix(0,0)
    } else {
        var err error
        since, err = time.Parse(time.RFC3339, sinceStr)
        if err != nil {
            httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid since", nil)
            return
        }
    }
    limit := 100
    if l := r.URL.Query().Get("limit"); l != "" {
        if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
            limit = v
        }
    }
    out, err := h.svc.ListCarrierMessagesForDevice(r.Context(), deviceID, since, limit)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{"messages": out})
}

// POST /v1/carrier/messages/{id}/link
func (h *Handler) Link(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }

    carrierID := chi.URLParam(r, "id")
    if carrierID == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "carrier message id required", nil)
        return
    }

    var body struct{
        ServerMessageID string `json:"server_message_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
        return
    }
    if body.ServerMessageID == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "server_message_id required", nil)
        return
    }

    // Validate server_message_id is a UUID.
    if _, err := uuid.Parse(body.ServerMessageID); err != nil {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "server_message_id invalid", nil)
        return
    }

    // Verify caller has the right to link this carrier message: require the
    // authenticated user to be the owner of the device associated with the
    // carrier message. If `h.db` is nil (tests), skip the ownership check.
    if h.db != nil {
        var exists bool
        if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM carrier_messages cm JOIN devices d ON cm.device_id = d.id WHERE cm.id = $1::uuid AND d.user_id = $2::uuid)`, carrierID, userID).Scan(&exists); err != nil || !exists {
            httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "not authorized to link this carrier message", nil)
            return
        }
    }

    // Pass the authenticated user id as the actor for audit purposes.
    out, err := h.svc.SetServerMessageLink(r.Context(), carrierID, body.ServerMessageID, userID)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "link_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(out)
}

// GET /v1/carrier/messages/{id}/links
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
        return
    }
    carrierID := chi.URLParam(r, "id")
    if carrierID == "" {
        httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "carrier message id required", nil)
        return
    }

    // Verify caller owns the device for this carrier message. Skip when h.db
    // is nil (used by unit tests).
    if h.db != nil {
        var exists bool
        if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM carrier_messages cm JOIN devices d ON cm.device_id = d.id WHERE cm.id = $1::uuid AND d.user_id = $2::uuid)`, carrierID, userID).Scan(&exists); err != nil || !exists {
            httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "not authorized to view links for this carrier message", nil)
            return
        }
    }

    limit := 100
    if l := r.URL.Query().Get("limit"); l != "" {
        if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
            limit = v
        }
    }

    out, err := h.svc.ListCarrierMessageLinks(r.Context(), carrierID, limit)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{"links": out})
}

// GET /v1/admin/carrier_message_links?actor=&limit=
func (h *Handler) AdminListLinks(w http.ResponseWriter, r *http.Request) {
    // Require ADMIN profile
    if !middleware.HasProfile(r.Context(), "ADMIN") {
        httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "admin profile required", nil)
        return
    }

    actor := r.URL.Query().Get("actor")
    limit := 100
    if l := r.URL.Query().Get("limit"); l != "" {
        if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
            limit = v
        }
    }

    out, err := h.svc.ListCarrierMessageLinksByActor(r.Context(), actor, limit)
    if err != nil {
        httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{"links": out})
}
