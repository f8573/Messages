package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// parseCursor accepts either an opaque base64-encoded JSON cursor containing
// `timestamp_ms` (unix ms) or an RFC3339 timestamp string. Returns zero Time
// on failure.
// opaqueCursor represents the spec-recommended cursor shape used by
// clients: base64(JSON({cursor_version, last_server_order, timestamp_ms})).
type opaqueCursor struct {
	CursorVersion   int64            `json:"cursor_version"`
	LastServerOrder map[string]int64 `json:"last_server_order,omitempty"`
	TimestampMS     int64            `json:"timestamp_ms"`
}

// parseCursor accepts either an opaque base64-encoded JSON cursor containing
// `timestamp_ms` (unix ms) or an RFC3339 timestamp string. Returns zero Time
// on failure. It preserves backward compatibility while allowing the server to
// emit/consume the spec-recommended opaque cursor format.
func parseCursor(cursor string) time.Time {
	if cursor == "" {
		return time.Time{}
	}
	// Try base64 JSON opaque cursor first
	if b, err := base64.StdEncoding.DecodeString(cursor); err == nil {
		var obj opaqueCursor
		if err := json.Unmarshal(b, &obj); err == nil {
			if obj.TimestampMS > 0 {
				return time.UnixMilli(obj.TimestampMS)
			}
		}
	}
	// Fallback to RFC3339 parse
	if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
		return t
	}
	return time.Time{}
}

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// SyncResponse is a minimal response payload for incremental sync.
type SyncResponse struct {
	Events     []map[string]any `json:"events"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// IncrementalSync returns events since the cursor. This is a simplified
// implementation: it returns recent messages across conversations as
// event objects and encodes a simple next_cursor based on timestamp.
func (s *Service) IncrementalSync(ctx context.Context, cursor string, limit int) (SyncResponse, error) {
	// Parse cursor into a time. Helper supports opaque base64 JSON with
	// `timestamp_ms` (unix ms) or RFC3339 timestamp string.
	since := parseCursor(cursor)
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
	// Track per-conversation max server_order for next_cursor construction.
	lastServerOrder := map[string]int64{}
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
				"message_id":      id,
				"conversation_id": conv,
				"sender_user_id":  sender,
				"content_type":    contentType,
				"content":         content,
				"server_order":    serverOrder,
				"created_at":      createdAt,
			},
		})
		last = createdAt
		if cur, ok := lastServerOrder[conv]; !ok || serverOrder > cur {
			lastServerOrder[conv] = serverOrder
		}
	}
	nextCursor := ""
	if !last.IsZero() {
		// Prefer emitting the spec-recommended opaque cursor (base64 JSON)
		// containing cursor_version, timestamp_ms and per-conversation
		// last_server_order. This allows clients to resume deterministically
		// when they also maintain per-conversation watermarks.
		oc := opaqueCursor{CursorVersion: 1, TimestampMS: last.UnixMilli(), LastServerOrder: lastServerOrder}
		if b, err := json.Marshal(oc); err == nil {
			nextCursor = base64.StdEncoding.EncodeToString(b)
		} else {
			// Fallback to RFC3339 if marshaling fails for some reason.
			nextCursor = last.Format(time.RFC3339Nano)
		}
	}
	return SyncResponse{Events: events, NextCursor: nextCursor}, nil
}
