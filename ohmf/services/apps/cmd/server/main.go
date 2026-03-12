package main

import (
    "encoding/json"
    "log"
    "net/http"
)

func handleRegister(w http.ResponseWriter, r *http.Request) {
    // Placeholder: validate manifest and signature
    var payload map[string]interface{}
    _ = json.NewDecoder(r.Body).Decode(&payload)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/apps/register", handleRegister)
    addr := ":18086"
    log.Printf("apps service listening %s", addr)
    log.Fatal(http.ListenAndServe(addr, mux))
}
