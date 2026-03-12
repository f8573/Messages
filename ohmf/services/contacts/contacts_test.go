package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/f8573/Messages/pkg/observability"
)

func TestDiscoverSuccessAndNotFound(t *testing.T) {
    observability.Init()
    srv := newServer()
    mux := http.NewServeMux()
    mux.Handle("/v1/contacts/discover", http.HandlerFunc(srv.discoverHandler))
    mux.Handle("/internal/seed", http.HandlerFunc(srv.internalSeedHandler))
    handler := observability.RequestIDMiddleware(mux)
    ts := httptest.NewServer(handler)
    defer ts.Close()

    // seed
    seed := map[string]interface{}{
        "pepper": "test-pepper",
        "contacts": []map[string]string{
            {"identifier": "alice@example.com", "user_id": "user-alice", "display_name": "Alice"},
        },
    }
    bs, _ := json.Marshal(seed)
    req, _ := http.NewRequest(http.MethodPost, ts.URL+"/internal/seed", bytes.NewReader(bs))
    req.Header.Set("X-Admin-Token", "test")
    res, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("seed request failed: %v", err)
    }
    if res.StatusCode != http.StatusNoContent {
        t.Fatalf("expected 204, got %d", res.StatusCode)
    }

    // discover existing
    dis := map[string]interface{}{
        "identifiers": []string{"alice@example.com", "bob@example.com"},
    }
    bs, _ = json.Marshal(dis)
    res2, err := http.Post(ts.URL+"/v1/contacts/discover", "application/json", bytes.NewReader(bs))
    if err != nil {
        t.Fatalf("discover request failed: %v", err)
    }
    if res2.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", res2.StatusCode)
    }
    var dr struct {
        Matches []struct {
            Identifier string `json:"identifier"`
            Contact    struct {
                UserID      string `json:"user_id"`
                DisplayName string `json:"display_name"`
            } `json:"contact"`
        } `json:"matches"`
    }
    if err := json.NewDecoder(res2.Body).Decode(&dr); err != nil {
        t.Fatalf("decode failed: %v", err)
    }
    if len(dr.Matches) != 1 {
        t.Fatalf("expected 1 match, got %d", len(dr.Matches))
    }
    if dr.Matches[0].Contact.UserID != "user-alice" {
        t.Fatalf("unexpected user id: %s", dr.Matches[0].Contact.UserID)
    }

    // unauthorized seed
    seed2 := map[string]interface{}{"contacts": []map[string]string{}}
    bs, _ = json.Marshal(seed2)
    req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/internal/seed", bytes.NewReader(bs))
    // missing admin token
    res3, err := http.DefaultClient.Do(req2)
    if err != nil {
        t.Fatalf("unauth seed request failed: %v", err)
    }
    if res3.StatusCode != http.StatusUnauthorized {
        t.Fatalf("expected 401, got %d", res3.StatusCode)
    }
}

func TestMain(m *testing.M) {
    observability.Init()
    os.Exit(m.Run())
}
