package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/f8573/Messages/pkg/observability"
)

type Contact struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type discoverRequest struct {
	Identifiers []string `json:"identifiers"`
}

type discoverResult struct {
	Identifier string  `json:"identifier"`
	Contact    Contact `json:"contact"`
}

type discoverResponse struct {
	Matches []discoverResult `json:"matches"`
}

type seedContact struct {
	Identifier  string `json:"identifier"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type seedRequest struct {
	Pepper   string        `json:"pepper"`
	Contacts []seedContact `json:"contacts"`
}

type server struct {
	mu     sync.RWMutex
	index  map[string]Contact
	pepper string
}

func newServer() *server {
	return &server{
		index:  make(map[string]Contact),
		pepper: "pepper-01",
	}
}

func (s *server) hashIdentifier(identifier string) string {
	h := sha256.Sum256([]byte(identifier + s.pepper))
	return hex.EncodeToString(h[:])
}

func (s *server) discoverHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req discoverRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var resp discoverResponse
	for _, id := range req.Identifiers {
		hash := s.hashIdentifier(id)
		s.mu.RLock()
		contact, ok := s.index[hash]
		s.mu.RUnlock()
		if ok {
			resp.Matches = append(resp.Matches, discoverResult{
				Identifier: id,
				Contact:    contact,
			})
		}
	}
	b, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func (s *server) internalSeedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Admin-Token") != "test" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req seedRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Pepper != "" {
		s.mu.Lock()
		s.pepper = req.Pepper
		s.mu.Unlock()
	}
	for _, c := range req.Contacts {
		hash := s.hashIdentifier(c.Identifier)
		s.mu.Lock()
		s.index[hash] = Contact{
			UserID:      c.UserID,
			DisplayName: c.DisplayName,
		}
		s.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	observability.Init()
	srv := newServer()
	mux := http.NewServeMux()
	mux.Handle("/v1/contacts/discover", http.HandlerFunc(srv.discoverHandler))
	mux.Handle("/internal/seed", http.HandlerFunc(srv.internalSeedHandler))
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	handler := observability.RequestIDMiddleware(mux)

	port := "18085"
	observability.Logger.Printf("contacts service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		observability.Logger.Printf("listen error: %v", err)
		os.Exit(1)
	}
}
