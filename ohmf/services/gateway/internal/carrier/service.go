package carrier

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type CarrierMessage struct {
    ID               string          `json:"id"`
    DeviceID         string          `json:"device_id"`
    ThreadKey        string          `json:"thread_key"`
    CarrierMessageID string          `json:"carrier_message_id"`
    Direction        string          `json:"direction"`
    Transport        string          `json:"transport"`
    Text             string          `json:"text"`
    MediaJSON        json.RawMessage `json:"media_json"`
    CreatedAt        time.Time       `json:"created_at"`
    DeviceAuthoritative bool         `json:"device_authoritative"`
    ServerMessageID  *string         `json:"server_message_id,omitempty"`
    RawPayload       json.RawMessage `json:"raw_payload"`
}

// CarrierMessageLinkAudit represents a single audit entry linking a carrier
// message to a server message.
type CarrierMessageLinkAudit struct {
    ID               string    `json:"id"`
    CarrierMessageID string    `json:"carrier_message_id"`
    ServerMessageID  *string   `json:"server_message_id,omitempty"`
    SetAt            time.Time `json:"set_at"`
    Actor            string    `json:"actor"`
}

type Service struct{
    db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// ServiceAPI defines the subset of Service methods required by HTTP handlers.
type ServiceAPI interface {
    ImportCarrierMessage(ctx context.Context, deviceID string, m CarrierMessage) (CarrierMessage, error)
    ListCarrierMessagesForDevice(ctx context.Context, deviceID string, since time.Time, limit int) ([]CarrierMessage, error)
    SetServerMessageLink(ctx context.Context, carrierMessageID string, serverMessageID string, actor string) (CarrierMessage, error)
    ListCarrierMessageLinks(ctx context.Context, carrierMessageID string, limit int) ([]CarrierMessageLinkAudit, error)
    ListCarrierMessageLinksByActor(ctx context.Context, actor string, limit int) ([]CarrierMessageLinkAudit, error)
}

// ImportCarrierMessage stores a mirror of a carrier SMS/MMS message and marks
// it device_authoritative=true. serverMessageID may be nil.
func (s *Service) ImportCarrierMessage(ctx context.Context, deviceID string, m CarrierMessage) (CarrierMessage, error) {
    // ensure media_json and raw_payload are valid jsonb (already raw)
    var media json.RawMessage = nil
    if len(m.MediaJSON) > 0 {
        media = m.MediaJSON
    }
    var raw json.RawMessage = nil
    if len(m.RawPayload) > 0 {
        raw = m.RawPayload
    }

    var outID string
    var serverMsgID *string
    err := s.db.QueryRow(ctx, `
        INSERT INTO carrier_messages (id, device_id, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id, raw_payload)
        VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, true, $9::uuid, $10::jsonb)
        RETURNING id::text, server_message_id::text
    `, deviceID, m.ThreadKey, m.CarrierMessageID, m.Direction, m.Transport, m.Text, media, m.CreatedAt, m.ServerMessageID, raw).Scan(&outID, &serverMsgID)
    if err != nil {
        return CarrierMessage{}, err
    }
    m.ID = outID
    m.DeviceAuthoritative = true
    if serverMsgID != nil && *serverMsgID != "" {
        m.ServerMessageID = serverMsgID
    } else {
        m.ServerMessageID = nil
    }
    return m, nil
}

// ListCarrierMessagesForDevice returns carrier mirror messages for a device since a time.
func (s *Service) ListCarrierMessagesForDevice(ctx context.Context, deviceID string, since time.Time, limit int) ([]CarrierMessage, error) {
    rows, err := s.db.Query(ctx, `SELECT id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload FROM carrier_messages WHERE device_id = $1::uuid AND created_at > $2 ORDER BY created_at ASC LIMIT $3`, deviceID, since, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []CarrierMessage
    for rows.Next() {
        var cm CarrierMessage
        var media json.RawMessage
        var raw json.RawMessage
        var serverMsgID *string
        if err := rows.Scan(&cm.ID, &cm.DeviceID, &cm.ThreadKey, &cm.CarrierMessageID, &cm.Direction, &cm.Transport, &cm.Text, &media, &cm.CreatedAt, &cm.DeviceAuthoritative, &serverMsgID, &raw); err != nil {
            return nil, err
        }
        cm.MediaJSON = media
        cm.RawPayload = raw
        if serverMsgID != nil && *serverMsgID != "" {
            cm.ServerMessageID = serverMsgID
        }
        out = append(out, cm)
    }
    return out, nil
}

// PurgeCarrierMirror deletes carrier mirror rows by id list.
func (s *Service) PurgeCarrierMirror(ctx context.Context, messageIDs []string) error {
    if len(messageIDs) == 0 {
        return nil
    }
    _, err := s.db.Exec(ctx, `DELETE FROM carrier_messages WHERE id = ANY($1::uuid[])`, messageIDs)
    return err
}

// SetServerMessageLink sets the server_message_id for a carrier mirror row while
// preserving device_authoritative semantics. This will only set the link when
// the existing mirror row is device-authoritative (true). The update is
// idempotent and will not modify carrier-origin fields.
func (s *Service) SetServerMessageLink(ctx context.Context, carrierMessageID string, serverMessageID string, actor string) (CarrierMessage, error) {
    // Guard: verify the carrier mirror exists and is device-authoritative.
    var deviceAuth bool
    var existingServerID *string
    if err := s.db.QueryRow(ctx, `SELECT device_authoritative, server_message_id::text FROM carrier_messages WHERE id = $1::uuid`, carrierMessageID).Scan(&deviceAuth, &existingServerID); err != nil {
        return CarrierMessage{}, err
    }
    if !deviceAuth {
        // Reconciliation required to modify non-device-authoritative carrier rows.
        return CarrierMessage{}, fmt.Errorf("carrier: carrier_message %s is not device-authoritative; reconciliation required", carrierMessageID)
    }

    // If the existing server_message_id is already equal to the requested value,
    // return the current row (idempotent) and do not insert a duplicate audit entry.
    if existingServerID != nil && *existingServerID == serverMessageID {
        var cm CarrierMessage
        var media json.RawMessage
        var raw json.RawMessage
        var serverMsgID *string
        if err := s.db.QueryRow(ctx, `
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

    // Proceed to set the server_message_id; this UPDATE only modifies the link
    // column and leaves carrier-origin fields untouched (enforced by SQL).
    var cm CarrierMessage
    var media json.RawMessage
    var raw json.RawMessage
    var serverMsgID *string
    if err := s.db.QueryRow(ctx, `
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

    // Record an audit entry for the link only when the link changed.
    if _, err := s.db.Exec(ctx, `
        INSERT INTO carrier_message_links_audit (carrier_message_id, server_message_id, actor)
        VALUES ($1::uuid, $2::uuid, $3)
    `, cm.ID, serverMessageID, actor); err != nil {
        // Audit insert failed — log but don't fail the link operation.
        log.Printf("carrier: failed to insert carrier_message_links_audit for carrier_message_id=%s actor=%s: %v", cm.ID, actor, err)
    }

    return cm, nil
}

// ListCarrierMessageLinks returns audit entries for a carrier message.
func (s *Service) ListCarrierMessageLinks(ctx context.Context, carrierMessageID string, limit int) ([]CarrierMessageLinkAudit, error) {
    if limit <= 0 || limit > 1000 {
        limit = 100
    }
    rows, err := s.db.Query(ctx, `
        SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor
        FROM carrier_message_links_audit
        WHERE carrier_message_id = $1::uuid
        ORDER BY set_at ASC
        LIMIT $2
    `, carrierMessageID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []CarrierMessageLinkAudit
    for rows.Next() {
        var a CarrierMessageLinkAudit
        var serverID *string
        if err := rows.Scan(&a.ID, &a.CarrierMessageID, &serverID, &a.SetAt, &a.Actor); err != nil {
            return nil, err
        }
        if serverID != nil && *serverID != "" {
            a.ServerMessageID = serverID
        }
        out = append(out, a)
    }
    return out, nil
}

// ListCarrierMessageLinksByActor returns audit entries filtered by actor.
func (s *Service) ListCarrierMessageLinksByActor(ctx context.Context, actor string, limit int) ([]CarrierMessageLinkAudit, error) {
    if limit <= 0 || limit > 1000 {
        limit = 100
    }
    rows, err := s.db.Query(ctx, `
        SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor
        FROM carrier_message_links_audit
        WHERE actor = $1
        ORDER BY set_at DESC
        LIMIT $2
    `, actor, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []CarrierMessageLinkAudit
    for rows.Next() {
        var a CarrierMessageLinkAudit
        var serverID *string
        if err := rows.Scan(&a.ID, &a.CarrierMessageID, &serverID, &a.SetAt, &a.Actor); err != nil {
            return nil, err
        }
        if serverID != nil && *serverID != "" {
            a.ServerMessageID = serverID
        }
        out = append(out, a)
    }
    return out, nil
}
