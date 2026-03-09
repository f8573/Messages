package sync

import (
    "context"
    "encoding/json"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{
    db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// SyncResponse is a minimal response payload for incremental sync.
type SyncResponse struct{
    Events     []map[string]any `json:"events"`
    NextCursor string           `json:"next_cursor,omitempty"`
}

// IncrementalSync returns events since the cursor. This is a simplified
// implementation: it returns recent messages across conversations as
// event objects and encodes a simple next_cursor based on timestamp.
func (s *Service) IncrementalSync(ctx context.Context, cursor string, limit int) (SyncResponse, error) {
    // For a lightweight implementation, interpret cursor as an RFC3339 timestamp
    var since time.Time
    if cursor != "" {
        if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
            since = t
        }
    }
    if since.IsZero() {
        since = time.Now().Add(-5 * time.Minute)
    }
    rows, err := s.db.Query(ctx, `SELECT id::text, conversation_id::text, sender_user_id::text, content_type, content, server_order, created_at FROM messages WHERE created_at > $1 ORDER BY created_at ASC LIMIT $2`, since, limit)
    if err != nil {
        return SyncResponse{}, err
    }
    defer rows.Close()
    events := []map[string]any{}
    var last time.Time
    for rows.Next() {
        var id, conv, sender, contentType string
        var contentB []byte
        var serverOrder int64
        var createdAt time.Time
        if err := rows.Scan(&id, &conv, &sender, &contentType, &contentB, &serverOrder, &createdAt); err != nil {
            return SyncResponse{}, err
        }
        var content any
        _ = json.Unmarshal(contentB, &content)
        events = append(events, map[string]any{
            "type": "message.created",
            "payload": map[string]any{
                "message_id": id,
                "conversation_id": conv,
                "sender_user_id": sender,
                "content_type": contentType,
                "content": content,
                "server_order": serverOrder,
                "created_at": createdAt,
            },
        })
        last = createdAt
    }
    nextCursor := ""
    if !last.IsZero() {
        nextCursor = last.Format(time.RFC3339Nano)
    }
    return SyncResponse{Events: events, NextCursor: nextCursor}, nil
}
