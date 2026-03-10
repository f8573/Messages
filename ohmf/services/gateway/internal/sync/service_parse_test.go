package sync

import (
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"
)

func TestParseCursor_RFC3339(t *testing.T) {
    ts := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
    s := ts.Format(time.RFC3339Nano)
    got := parseCursor(s)
    if !got.Equal(ts) {
        t.Fatalf("expected %v, got %v", ts, got)
    }
}

func TestParseCursor_OpaqueBase64JSON(t *testing.T) {
    ts := time.Date(2026, 3, 9, 12, 30, 0, 0, time.UTC)
    ms := ts.UnixNano() / int64(time.Millisecond)
    obj := map[string]any{"cursor_version": 1, "timestamp_ms": ms}
    b, _ := json.Marshal(obj)
    enc := base64.StdEncoding.EncodeToString(b)
    got := parseCursor(enc)
    if !got.Equal(ts) {
        t.Fatalf("expected %v, got %v", ts, got)
    }
}

func TestParseCursor_InvalidFallsBackZero(t *testing.T) {
    got := parseCursor("not-a-timestamp")
    if !got.IsZero() {
        t.Fatalf("expected zero time for invalid cursor, got %v", got)
    }
}
