package pagination

import (
    "testing"
)

func TestEncodeDecode(t *testing.T) {
    src := map[string]any{"updated_at": "2026-03-09T12:00:00Z", "id": "abc"}
    cur := EncodeCursor(src)
    got, err := DecodeCursor(cur)
    if err != nil {
        t.Fatalf("decode failed: %v", err)
    }
    if got["id"] != src["id"] || got["updated_at"] != src["updated_at"] {
        t.Fatalf("roundtrip mismatch: got=%v src=%v", got, src)
    }
}

func TestDecodeInvalid(t *testing.T) {
    if _, err := DecodeCursor("!!notbase64!!"); err == nil {
        t.Fatalf("expected error for invalid cursor")
    }
}
