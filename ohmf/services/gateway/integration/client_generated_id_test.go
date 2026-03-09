package integration_test

import (
    "fmt"
    "testing"
    "time"
)

func TestClientGeneratedIDRoundtrip(t *testing.T) {
    baseURL := getenv("OHMF_BASE_URL", "http://localhost:18080")
    runID := fmt.Sprintf("%d", time.Now().UnixNano())
    waitForHealth(t, baseURL)

    a := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"51", "A-client")
    b := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"52", "B-client")

    conv := postJSON(t, baseURL+"/v1/conversations", map[string]any{
        "type":         "DM",
        "participants": []string{mustString(t, b["user_id"])},
    }, mustString(t, a["access_token"]))
    convID := mustString(t, conv["conversation_id"])

    clientID := "cgid-" + runID
    postJSON(t, baseURL+"/v1/messages", map[string]any{
        "conversation_id": convID,
        "idempotency_key": "idem-cgid-" + runID,
        "content_type":    "text",
        "content":         map[string]any{"text": "hello with cgid"},
        "client_generated_id": clientID,
    }, mustString(t, a["access_token"]))

    // Poll for the message to appear (async pipeline may delay persistence).
    var list map[string]any
    var items []any
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        list = getJSON(t, baseURL+"/v1/conversations/"+convID+"/messages", mustString(t, a["access_token"]))
        items = mustSlice(t, list["items"])
        if len(items) > 0 {
            break
        }
        time.Sleep(200 * time.Millisecond)
    }
    if len(items) == 0 {
        t.Fatalf("expected at least 1 message")
    }
    first := mustMap(t, items[0])
    if mustString(t, first["client_generated_id"]) != clientID {
        t.Fatalf("expected client_generated_id %s, got %v", clientID, first["client_generated_id"])
    }
}
