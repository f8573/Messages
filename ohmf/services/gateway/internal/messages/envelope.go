package messages

import (
    "encoding/json"
    "time"
    "ohmf/services/gateway/internal/version"
)

// Envelope mirrors the canonical envelope in packages/protocol/proto/envelope.proto
type Envelope struct {
    SpecVersion    string `json:"spec_version"`
    EventID        string `json:"event_id"`
    EventType      string `json:"event_type"`
    IssuedAt       string `json:"issued_at"`
    ConversationID string `json:"conversation_id"`
    Transport      string `json:"transport"`
    IdempotencyKey string `json:"idempotency_key"`
    Payload        []byte `json:"payload"`
    Actor          *Actor `json:"actor,omitempty"`
    Trace          *Trace `json:"trace,omitempty"`
}

// Actor describes the initiator of an event
type Actor struct {
    UserID   string `json:"user_id"`
    DeviceID string `json:"device_id,omitempty"`
}

// Trace contains optional tracing identifiers
type Trace struct {
    RequestID     string `json:"request_id,omitempty"`
    CorrelationID string `json:"correlation_id,omitempty"`
}

// MessageRecord is the canonical message payload format used inside Envelope.payload.
type MessageRecord struct {
    MessageID         string `json:"message_id"`
    ConversationID    string `json:"conversation_id"`
    ServerOrder       int64  `json:"server_order"`
    SenderUserID      string `json:"sender_user_id"`
    SenderDeviceID    string `json:"sender_device_id"`
    Transport         string `json:"transport"`
    ContentType       string `json:"content_type"`
    Content           []byte `json:"content"`
    CreatedAtUnixMS   int64  `json:"created_at_unix_ms"`
    EditedAtUnixMS    int64  `json:"edited_at_unix_ms"`
    DeletedAtUnixMS   int64  `json:"deleted_at_unix_ms"`
    RedactedAtUnixMS  int64  `json:"redacted_at_unix_ms"`
    VisibilityState   string `json:"visibility_state"`
}

// BuildEnvelopeFromIngressEvent constructs an Envelope containing a MessageRecord
// The payload is JSON-marshaled bytes of MessageRecord. The Envelope.Payload
// will be JSON bytes (base64 when the envelope itself is JSON-marshaled by the bus).
func BuildEnvelopeFromIngressEvent(evt IngressEvent) (Envelope, error) {
    // marshal content map into bytes
    contentBytes, err := json.Marshal(evt.Content)
    if err != nil {
        return Envelope{}, err
    }

    mr := MessageRecord{
        MessageID:       evt.MessageID,
        ConversationID:  evt.ConversationID,
        ServerOrder:     0,
        SenderUserID:    evt.SenderUserID,
        SenderDeviceID:  "",
        Transport:       evt.TransportIntent,
        ContentType:     evt.ContentType,
        Content:         contentBytes,
        CreatedAtUnixMS: evt.SentAtMS,
        VisibilityState: "VISIBLE",
    }

    payload, err := json.Marshal(mr)
    if err != nil {
        return Envelope{}, err
    }

    env := Envelope{
        SpecVersion:    version.SpecVersion,
        EventID:        evt.EventID,
        EventType:      "message.create",
        IssuedAt:       time.Now().UTC().Format(time.RFC3339Nano),
        ConversationID: evt.ConversationID,
        Transport:      evt.TransportIntent,
        IdempotencyKey: evt.IdempotencyKey,
        Payload:        payload,
        Actor:          &Actor{UserID: evt.SenderUserID},
        Trace:          &Trace{RequestID: evt.TraceID},
    }

    // attach actor and trace as optional fields by embedding in the json payload when marshaled by producer
    // We can't add dynamic fields to the struct without changing producer; instead, include actor/trace by
    // marshaling an envelope wrapper when necessary. For now, enrich the envelope by adding these as json
    // fields via temporary map when published by the bus.

    return env, nil
}
