package main

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "net/http"
    "os"
    "strings"
    "sync"

    "github.com/f8573/Messages/pkg/observability"
)

type uploadResponse struct {
    UploadID string `json:"upload_id"`
    URL      string `json:"url"`
}

type mediaServer struct {
    mu      sync.Mutex
    uploads map[string]bool
}

func newMediaServer() *mediaServer {
    return &mediaServer{
        uploads: make(map[string]bool),
    }
}

func genID() string {
    b := make([]byte, 12)
    _, err := rand.Read(b)
    if err != nil {
        return "upload-unknown"
    }
    return hex.EncodeToString(b)
}

func (s *mediaServer) createUploadHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    id := genID()
    s.mu.Lock()
    s.uploads[id] = false
    s.mu.Unlock()
    resp := uploadResponse{
        UploadID: id,
        URL:      "https://storage.local/upload/" + id,
    }
    b, _ := json.Marshal(resp)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    w.Write(b)
}

func (s *mediaServer) completeUploadHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    parts := strings.Split(r.URL.Path, "/")
    if len(parts) < 4 {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }
    uploadID := parts[len(parts)-2]
    s.mu.Lock()
    defer s.mu.Unlock()
    _, ok := s.uploads[uploadID]
    if !ok {
        http.Error(w, "not found", http.StatusNotFound)
        return
    }
    s.uploads[uploadID] = true
    w.WriteHeader(http.StatusOK)
}

func main() {
    observability.Init()
    s := newMediaServer()
    mux := http.NewServeMux()
    mux.Handle("/v1/media/uploads", http.HandlerFunc(s.createUploadHandler))
    // pattern: /v1/media/uploads/{upload_id}/complete
    mux.Handle("/v1/media/uploads/", http.HandlerFunc(s.completeUploadHandler))

    mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    }))

    handler := observability.RequestIDMiddleware(mux)
    port := "18087"
    observability.Logger.Printf("media service listening on :%s", port)
    if err := http.ListenAndServe(":"+port, handler); err != nil {
        observability.Logger.Printf("listen error: %v", err)
        os.Exit(1)
    }
}
