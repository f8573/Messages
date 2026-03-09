package types

// SyncCursor is a canonical in-memory representation of a sync cursor.
// It maps to the `sync_cursor.schema.json` in packages/protocol/schemas.
type SyncCursor struct {
	CursorVersion   int64            `json:"cursor_version"`
	LastServerOrder map[string]int64 `json:"last_server_order,omitempty"`
	TimestampMS     int64            `json:"timestamp_ms"`
}
