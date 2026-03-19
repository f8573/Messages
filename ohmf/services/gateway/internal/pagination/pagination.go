package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

func EncodeCursor(data map[string]any) string {
	b, _ := json.Marshal(data)
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeCursor(s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("invalid cursor json: %w", err)
	}
	return out, nil
}

// removed: redundant doc comments stripped where function names are explicit
