package miniapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RegistryClient struct {
	baseURL string
	http    *http.Client
}

func NewRegistryClient(baseURL string) *RegistryClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &RegistryClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *RegistryClient) doJSON(ctx context.Context, method, path, userID string, body any) (map[string]any, int, error) {
	if c == nil {
		return nil, 0, fmt.Errorf("registry client not configured")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	var payload map[string]any
	if res.StatusCode != http.StatusNoContent {
		_ = json.NewDecoder(res.Body).Decode(&payload)
	}
	if res.StatusCode >= 400 {
		if errorPayload, ok := payload["error"].(map[string]any); ok {
			return payload, res.StatusCode, fmt.Errorf("%v", errorPayload["message"])
		}
		return payload, res.StatusCode, fmt.Errorf("registry request failed with %d", res.StatusCode)
	}
	return payload, res.StatusCode, nil
}
