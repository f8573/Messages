package integration_test

import (
	"io"
	"net/http"
	"testing"
)

func TestOpenAPIServe(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	waitForHealth(t, baseURL)

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/openapi.yaml", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get openapi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("expected non-empty openapi body")
	}
}
