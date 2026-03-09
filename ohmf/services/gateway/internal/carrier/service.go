package carrier

import (
    "context"
    "encoding/json"
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

type Service struct{
    db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

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
