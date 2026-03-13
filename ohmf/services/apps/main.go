package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/f8573/Messages/pkg/observability"
)

type Manifest struct {
	AppID       string   `json:"app_id"`
	Entrypoint  string   `json:"entrypoint"`
	Permissions []string `json:"permissions"`
	Signature   string   `json:"signature"`
}

type appsServer struct {
	mu       sync.Mutex
	dataFile string
}

func newAppsServer(dataFile string) *appsServer {
	return &appsServer{
		dataFile: dataFile,
	}
}

func (s *appsServer) loadAll() ([]Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []Manifest
	f, err := os.Open(s.dataFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return list, nil
		}
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *appsServer) saveAll(list []Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.dataFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(s.dataFile)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}

func validateManifest(m Manifest) error {
	if m.AppID == "" {
		return errors.New("app_id required")
	}
	if m.Entrypoint == "" {
		return errors.New("entrypoint required")
	}
	if len(m.Permissions) == 0 {
		return errors.New("permissions required")
	}
	if m.Signature == "" {
		return errors.New("signature required")
	}
	return nil
}

func (s *appsServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := validateManifest(m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	list, err := s.loadAll()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// naive dedupe by app_id
	for _, ex := range list {
		if ex.AppID == m.AppID {
			http.Error(w, "app already registered", http.StatusConflict)
			return
		}
	}
	list = append(list, m)
	if err := s.saveAll(list); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func main() {
	observability.Init()
	dataFile := os.Getenv("DATA_FILE")
	if dataFile == "" {
		dataFile = "ohmf/services/apps/data/registered_apps.json"
	}
	s := newAppsServer(dataFile)
	mux := http.NewServeMux()
	mux.Handle("/v1/apps/register", http.HandlerFunc(s.registerHandler))
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	handler := observability.RequestIDMiddleware(mux)
	port := "18086"
	observability.Logger.Printf("apps service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		observability.Logger.Printf("listen error: %v", err)
		os.Exit(1)
	}
}
