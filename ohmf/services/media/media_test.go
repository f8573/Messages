package main

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"

    "github.com/f8573/Messages/pkg/observability"
)

func TestCreateAndCompleteUpload(t *testing.T) {
    observability.Init()
    s := newMediaServer()
    mux := http.NewServeMux()
    mux.Handle("/v1/media/uploads", http.HandlerFunc(s.createUploadHandler))
    mux.Handle("/v1/media/uploads/", http.HandlerFunc(s.completeUploadHandler))
    handler := observability.RequestIDMiddleware(mux)
    ts := httptest.NewServer(handler)
    defer ts.Close()

    // create upload
    res, err := http.Post(ts.URL+"/v1/media/uploads", "application/json", nil)
    if err != nil {
        t.Fatalf("create request failed: %v", err)
    }
    if res.StatusCode != http.StatusCreated {
        t.Fatalf("expected 201, got %d", res.StatusCode)
    }
    var ur uploadResponse
    if err := json.NewDecoder(res.Body).Decode(&ur); err != nil {
        t.Fatalf("decode failed: %v", err)
    }
    if ur.UploadID == "" || !strings.Contains(ur.URL, ur.UploadID) {
        t.Fatalf("unexpected response: %+v", ur)
    }

    // complete upload
    completeURL := ts.URL + "/v1/media/uploads/" + ur.UploadID + "/complete"
    req, _ := http.NewRequest(http.MethodPost, completeURL, bytes.NewReader([]byte{}))
    res2, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("complete request failed: %v", err)
    }
    if res2.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(res2.Body)
        t.Fatalf("expected 200, got %d body=%s", res2.StatusCode, string(body))
    }

    // complete non-existent
    req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/media/uploads/notfound/complete", nil)
    res3, err := http.DefaultClient.Do(req2)
    if err != nil {
        t.Fatalf("complete notfound failed: %v", err)
    }
    if res3.StatusCode != http.StatusNotFound {
        t.Fatalf("expected 404, got %d", res3.StatusCode)
    }
}

func TestMain(m *testing.M) {
    observability.Init()
    os.Exit(m.Run())
}
