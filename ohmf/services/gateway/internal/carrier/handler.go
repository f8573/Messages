package carrier

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"

	"fmt"
	"log"
)

type Handler struct {
	db DB
}

// removed: trivial constructor wrapper
// POST /v1/carrier/messages/import
func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		DeviceID         string `json:"device_id"`
		ThreadKey        string `json:"thread_key"`
		CarrierMessageID string `json:"carrier_message_id"`
		Direction        string `json:"direction"`
		Transport        string `json:"transport"`
		Text             string `json:"text"`
		Media            any    `json:"media"`
		RawPayload       any    `json:"raw_payload"`
		CreatedAt  string `json:"created_at"`
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
		DeviceID:         req.DeviceID,
		ThreadKey:        req.ThreadKey,
		CarrierMessageID: req.CarrierMessageID,
		Direction:        req.Direction,
		Transport:        req.Transport,
		Text:             req.Text,
		MediaJSON:        mediaJSON,
		RawPayload:       rawJSON,
		CreatedAt:        createdAt,
	}
	var media json.RawMessage
	if len(cm.MediaJSON) > 0 {
		media = cm.MediaJSON
	}
	var raw json.RawMessage
	if len(cm.RawPayload) > 0 {
		raw = cm.RawPayload
	}

	var outID string
	var serverMsgID *string
	err := h.db.QueryRow(r.Context(), `
        INSERT INTO carrier_messages (id, device_id, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id, raw_payload)
        VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, true, $9::uuid, $10::jsonb)
        RETURNING id::text, server_message_id::text
    `, req.DeviceID, cm.ThreadKey, cm.CarrierMessageID, cm.Direction, cm.Transport, cm.Text, media, cm.CreatedAt, cm.ServerMessageID, raw).Scan(&outID, &serverMsgID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "import_failed", err.Error(), nil)
		return
	}
	cm.ID = outID
	m.DeviceAuthoritative = true
	if serverMsgID != nil && *serverMsgID != "" {
		cm.ServerMessageID = serverMsgID
	} else {
		cm.ServerMessageID = nil
	}
	out := cm
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
		since = time.Unix(0, 0)
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
	rows, err := h.db.Query(r.Context(), `SELECT id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload FROM carrier_messages WHERE device_id = $1::uuid AND created_at > $2 ORDER BY created_at ASC LIMIT $3`, deviceID, since, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
	}
	defer rows.Close()
	var out []CarrierMessage
	for rows.Next() {
		var cm CarrierMessage
		var media json.RawMessage
		var raw json.RawMessage
		var serverMsgID *string
		if err := rows.Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		cm.MediaJSON = media
		cm.RawPayload = raw
		if serverMsgID != nil && *serverMsgID != "" {
			cm.ServerMessageID = serverMsgID
		}
		out = append(out, cm)
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

	var body struct {
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
	var deviceAuth bool
	var existingServerID *string
	if err := h.db.QueryRow(r.Context(), `SELECT device_authoritative, server_message_id::text FROM carrier_messages WHERE id = $1::uuid`, carrierID).Scan(&deviceAuth, &existingServerID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "link_failed", err.Error(), nil)
		return
	}
	if !deviceAuth {
		httpx.WriteError(w, r, http.StatusConflict, "reconciliation_required", fmt.Sprintf("carrier: carrier_message %s is not device-authoritative; reconciliation required", carrierID)
	}

	if existingServerID != nil && *existingServerID == body.ServerMessageID {
		var cm CarrierMessage
		var media json.RawMessage
		var raw json.RawMessage
		var serverMsgID *string
		if err := h.db.QueryRow(r.Context(), `
            SELECT id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload
            FROM carrier_messages
            WHERE id = $1::uuid
        `, carrierID).Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "link_failed", err.Error(), nil)
		return
		}
		cm.MediaJSON = media
		cm.RawPayload = raw
		if serverMsgID != nil && *serverMsgID != "" {
			cm.ServerMessageID = serverMsgID
		}
		out := cm
	}

	var cm CarrierMessage
	var media json.RawMessage
	var raw json.RawMessage
	var serverMsgID *string
	if err := h.db.QueryRow(r.Context(), `
        UPDATE carrier_messages
        SET server_message_id = $2::uuid
        WHERE id = $1::uuid AND device_authoritative = true
        RETURNING id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload
    `, carrierID, body.ServerMessageID).Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "link_failed", err.Error(), nil)
		return
	}
	cm.MediaJSON = media
	cm.RawPayload = raw
	if serverMsgID != nil && *serverMsgID != "" {
		cm.ServerMessageID = serverMsgID
	}

	if _, err := h.db.Exec(r.Context(), `
        INSERT INTO carrier_message_links_audit (carrier_message_id, server_message_id, userID)
        VALUES ($1::uuid, $2::uuid, $3)
    `, cm.ID, body.ServerMessageID, userID); err != nil {
		log.Printf("carrier: failed to insert carrier_message_links_audit for carrier_message_id=%s userID=%s: %v", cm.ID, userID, err)
	}

	out := cm
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

	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := h.db.Query(r.Context(), `
        SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor
        FROM carrier_message_links_audit
        WHERE carrier_message_id = $1::uuid
        ORDER BY set_at ASC
        LIMIT $2
    `, carrierID, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
	}
	defer rows.Close()
	var out []CarrierMessageLinkAudit
	for rows.Next() {
		var a CarrierMessageLinkAudit
		var serverID *string
		if err := rows.Scan(&a.ID, &a.CarrierMessageID, &serverID, &a.SetAt, &a.Actor); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		if serverID != nil && *serverID != "" {
			a.ServerMessageID = serverID
		}
		out = append(out, a)
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

	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := h.db.Query(r.Context(), `
        SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor
        FROM carrier_message_links_audit
        WHERE actor = $1
        ORDER BY set_at DESC
        LIMIT $2
    `, actor, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
	}
	defer rows.Close()
	var out []CarrierMessageLinkAudit
	for rows.Next() {
		var a CarrierMessageLinkAudit
		var serverID *string
		if err := rows.Scan(&a.ID, &a.CarrierMessageID, &serverID, &a.SetAt, &a.Actor); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		if serverID != nil && *serverID != "" {
			a.ServerMessageID = serverID
		}
		out = append(out, a)
	}
	
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"links": out})
}


type CarrierMessage struct {
	ID                  string          `json:"id"`
	DeviceID            string          `json:"device_id"`
	ThreadKey           string          `json:"thread_key"`
	CarrierMessageID    string          `json:"carrier_message_id"`
	Direction           string          `json:"direction"`
	Transport           string          `json:"transport"`
	Text                string          `json:"text"`
	MediaJSON           json.RawMessage `json:"media_json"`
	CreatedAt           time.Time       `json:"created_at"`
	DeviceAuthoritative bool            `json:"device_authoritative"`
	ServerMessageID     *string         `json:"server_message_id,omitempty"`
	RawPayload          json.RawMessage `json:"raw_payload"`
}

type CarrierMessageLinkAudit struct {
	ID               string    `json:"id"`
	CarrierMessageID string    `json:"carrier_message_id"`
	ServerMessageID  *string   `json:"server_message_id,omitempty"`
	SetAt            time.Time `json:"set_at"`
	Actor            string    `json:"actor"`
}

type RowScanner interface {
	Scan(dest ...any) error
}

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
}

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) RowScanner
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (any, error)
}

func (h *Handler) PurgeCarrierMirror(ctx context.Context, messageIDs []string) error {
	var deviceAuth bool
	var existingServerID *string
	if err := h.db.QueryRow(ctx, `SELECT device_authoritative, server_message_id::text FROM carrier_messages WHERE id = $1::uuid`, carrierMessageID).Scan(&deviceAuth, &existingServerID); err != nil {
		return CarrierMessage{}, err
	}
	if !deviceAuth {
		return CarrierMessage{}, fmt.Errorf("carrier: carrier_message %s is not device-authoritative; reconciliation required", carrierMessageID)
	}

	if existingServerID != nil && *existingServerID == serverMessageID {
		var cm CarrierMessage
		var media json.RawMessage
		var raw json.RawMessage
		var serverMsgID *string
		if err := h.db.QueryRow(ctx, `
            SELECT id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload
            FROM carrier_messages
            WHERE id = $1::uuid
        `, carrierMessageID).Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
			return CarrierMessage{}, err
		}
		cm.MediaJSON = media
		cm.RawPayload = raw
		if serverMsgID != nil && *serverMsgID != "" {
			cm.ServerMessageID = serverMsgID
		}
		return cm, nil
	}

	var cm CarrierMessage
	var media json.RawMessage
	var raw json.RawMessage
	var serverMsgID *string
	if err := h.db.QueryRow(ctx, `
        UPDATE carrier_messages
        SET server_message_id = $2::uuid
        WHERE id = $1::uuid AND device_authoritative = true
        RETURNING id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload
    `, carrierMessageID, serverMessageID).Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
		return CarrierMessage{}, err
	}
	cm.MediaJSON = media
	cm.RawPayload = raw
	if serverMsgID != nil && *serverMsgID != "" {
		cm.ServerMessageID = serverMsgID
	}

	if _, err := h.db.Exec(ctx, `
        INSERT INTO carrier_message_links_audit (carrier_message_id, server_message_id, actor)
        VALUES ($1::uuid, $2::uuid, $3)
    `, cm.ID, serverMessageID, actor); err != nil {
		log.Printf("carrier: failed to insert carrier_message_links_audit for carrier_message_id=%s actor=%s: %v", cm.ID, actor, err)
	}

	return cm, nil
}
